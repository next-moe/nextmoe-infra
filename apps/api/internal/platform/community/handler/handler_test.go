package handler

import (
	"context"
	"os"
	"strings"
	"testing"

	suitelock "api/internal/platform/community/dbtest"
	"api/internal/platform/community/dto"
	"api/internal/platform/community/migrate"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("community/handler")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("community/handler", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("community/handler", "community migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"community_purge_archive", "community_notification", "community_event",
		"community_review_item", "community_flag", "community_reaction",
		"community_anchor_user", "community_thread_user", "community_board", "community_trust",
		"community_post", "community_thread",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func TestSpecExport(t *testing.T) {
	api := Setup(fiber.New(), Services{})
	b, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	spec := string(b)
	for _, want := range []string{
		"/api/v1/community/comments/resolve",
		"/api/v1/community/threads",
		"operationId: getComments",
		"operationId: comment\n",
		"/api/v1/community/topics",
		"/api/v1/community/trust/activity",
		"/api/v1/community/review",
		"operationId: reply",
		"operationId: editPost",
		"operationId: deletePost",
		"operationId: submitFlag",
		"operationId: recordActivity",
		"operationId: approveReview",
		"operationId: topAuthors",
		"operationId: listBoards",
		"operationId: getBoardBySlug",
		"operationId: createBoard",
		"operationId: updateBoard",
		"operationId: deleteBoard",
		"operationId: reorderBoards",
		"operationId: moveTopic",
		"operationId: pinTopic",
		"operationId: closeThread",
		"operationId: markAnswer",
		"operationId: setAnchorNotification",
		"operationId: anchorStates",
		"operationId: listAnchorSubscriptions",
		"operationId: listNotifications",
		"operationId: markNotificationsRead",
		"operationId: notificationFeed",
		"mention_user_ids",
		"name: board_id",
		"name: pinned",
		"name: anchor_id",
		"name: viewer_id",
		"reaction_count",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec missing %q", want)
		}
	}
}

func clientCtx(site string) context.Context {
	return context.WithValue(context.Background(), ctxKeyClient, &siteModel.OAuthClient{ID: site, CatalogSite: site})
}

func TestHandlerFlow(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:   service.NewThreadService(testDB, sink),
		posts:     service.NewPostService(testDB, sink),
		reactions: service.NewReactionService(testDB),
		feedback:  service.NewFeedbackService(testDB, sink),
		flags:     service.NewFlagService(testDB, sink),
		trust:     service.NewTrustService(testDB),
		review:    service.NewReviewService(testDB, sink),
	}
	ctx := clientCtx("letmoe")

	topicOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "hello", ContentRating: 0, Body: "opening **post**",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	if topicOut.Body.Code != 0 || topicOut.Body.Data.Thread.ID == 0 || topicOut.Body.Data.Post == nil || topicOut.Body.Data.Post.PostNumber != 1 {
		t.Fatalf("unexpected openTopic envelope: %+v", topicOut.Body)
	}
	threadID := topicOut.Body.Data.Thread.ID

	if _, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "a reply"}}); err != nil {
		t.Fatalf("reply: %v", err)
	}
	detail, err := s.getThread(ctx, &threadPostsInput{ID: threadID})
	if err != nil {
		t.Fatalf("getThread: %v", err)
	}
	if detail.Body.Data.Thread.PostsCount != 2 || len(detail.Body.Data.Posts) != 2 {
		t.Fatalf("thread should have 2 posts: count=%d len=%d", detail.Body.Data.Thread.PostsCount, len(detail.Body.Data.Posts))
	}
	if !strings.Contains(detail.Body.Data.Posts[0].ContentHTML, "<strong>post</strong>") {
		t.Fatalf("opening post should be cooked: %s", detail.Body.Data.Posts[0].ContentHTML)
	}

	c1, err := s.resolveComments(ctx, &resolveCommentsInput{Body: dto.CommentsResolveRequest{AnchorKind: 1, AnchorID: "g9"}})
	if err != nil {
		t.Fatalf("resolveComments: %v", err)
	}
	c2, _ := s.resolveComments(ctx, &resolveCommentsInput{Body: dto.CommentsResolveRequest{AnchorKind: 1, AnchorID: "g9"}})
	if c1.Body.Data.Thread.ID != c2.Body.Data.Thread.ID {
		t.Fatalf("comments resolve not idempotent: %d vs %d", c1.Body.Data.Thread.ID, c2.Body.Data.Thread.ID)
	}

	list, err := s.listThreads(ctx, &listThreadsInput{Kind: 0})
	if err != nil {
		t.Fatalf("listThreads: %v", err)
	}
	if len(list.Body.Data.Threads) != 1 || list.Body.Data.Threads[0].ID != threadID {
		t.Fatalf("expected the topic in the site list, got %+v", list.Body.Data.Threads)
	}

	if _, err := s.openTopic(context.Background(), &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 1, AnchorID: "1", Body: "x"}}); err == nil {
		t.Fatal("unbound client should be refused")
	}
}

func TestHandlerGapfill(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:   service.NewThreadService(testDB, sink),
		posts:     service.NewPostService(testDB, sink),
		reactions: service.NewReactionService(testDB),
		feedback:  service.NewFeedbackService(testDB, sink),
		trust:     service.NewTrustService(testDB),
	}
	ctx := clientCtx("letmoe")

	topicOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "t", Body: "opening"}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	threadID := topicOut.Body.Data.Thread.ID
	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (200, 1, 0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	replyOut, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "first"}})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	postID := replyOut.Body.Data.Post.ID

	editOut, err := s.editPost(ctx, &editPostInput{ID: postID, Body: dto.EditPostRequest{AuthorID: 200, Body: "edited **body**"}})
	if err != nil {
		t.Fatalf("editPost: %v", err)
	}
	if editOut.Body.Data.Post.EditedAt == nil || !strings.Contains(editOut.Body.Data.Post.ContentHTML, "<strong>body</strong>") {
		t.Fatalf("edit not applied: %+v", editOut.Body.Data.Post)
	}

	if _, err := s.editPost(ctx, &editPostInput{ID: postID, Body: dto.EditPostRequest{AuthorID: 999, Body: "hijack"}}); err == nil {
		t.Fatal("a non-author edit should be refused")
	}

	if _, err := s.deletePost(ctx, &deletePostInput{ID: postID, AuthorID: 200}); err != nil {
		t.Fatalf("deletePost: %v", err)
	}
	detail, _ := s.getThread(ctx, &threadPostsInput{ID: threadID})
	if detail.Body.Data.Thread.PostsCount != 2 {
		t.Fatalf("posts_count must not decrement on self-delete: %d", detail.Body.Data.Thread.PostsCount)
	}

	if _, err := s.openFeedback(ctx, &openFeedbackInput{Body: dto.OpenFeedbackRequest{AuthorID: 100, AnchorKind: 2, AnchorID: "r1", Title: "fb1", Body: "x"}}); err != nil {
		t.Fatalf("openFeedback r1: %v", err)
	}
	if _, err := s.openFeedback(ctx, &openFeedbackInput{Body: dto.OpenFeedbackRequest{AuthorID: 100, AnchorKind: 2, AnchorID: "r2", Title: "fb2", Body: "y"}}); err != nil {
		t.Fatalf("openFeedback r2: %v", err)
	}
	hit, err := s.listThreads(ctx, &listThreadsInput{Kind: 2, AnchorKind: 2, AnchorID: "r1"})
	if err != nil {
		t.Fatalf("listThreads anchor: %v", err)
	}
	if len(hit.Body.Data.Threads) != 1 || hit.Body.Data.Threads[0].AnchorID != "r1" {
		t.Fatalf("anchor filter should return only r1, got %+v", hit.Body.Data.Threads)
	}
	miss, _ := s.listThreads(ctx, &listThreadsInput{Kind: 2, AnchorKind: 2, AnchorID: "r404"})
	if len(miss.Body.Data.Threads) != 0 {
		t.Fatalf("a non-existent anchor should be empty, got %d", len(miss.Body.Data.Threads))
	}
}

func TestListThreads_OpeningStatusProjected(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:   service.NewThreadService(testDB, sink),
		posts:     service.NewPostService(testDB, sink),
		reactions: service.NewReactionService(testDB),
		trust:     service.NewTrustService(testDB),
	}
	ctx := clientCtx("letmoe")

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (100,1,0),(300,1,0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}

	visOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "vis", Body: "x"}})
	if err != nil {
		t.Fatalf("openTopic vis: %v", err)
	}
	visID := visOut.Body.Data.Thread.ID

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (700, 0, 2)
		 ON CONFLICT (user_id) DO UPDATE SET level = 0, first_posts_held_remaining = 2`,
	).Error; err != nil {
		t.Fatalf("seed held author: %v", err)
	}
	heldOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 700, BoardID: testBoard(t, "letmoe", "b1"), Title: "held", Body: "y"}})
	if err != nil {
		t.Fatalf("openTopic held: %v", err)
	}
	heldID := heldOut.Body.Data.Thread.ID
	if heldOut.Body.Data.Post.Status != model.PostStatusHidden {
		t.Fatalf("newcomer opening should be held: %d", heldOut.Body.Data.Post.Status)
	}

	delOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 300, BoardID: testBoard(t, "letmoe", "b1"), Title: "del", Body: "z"}})
	if err != nil {
		t.Fatalf("openTopic del: %v", err)
	}
	delID := delOut.Body.Data.Thread.ID
	if _, err := s.deletePost(ctx, &deletePostInput{ID: delOut.Body.Data.Post.ID, AuthorID: 300}); err != nil {
		t.Fatalf("deletePost: %v", err)
	}

	list, err := s.listThreads(ctx, &listThreadsInput{Kind: 0})
	if err != nil {
		t.Fatalf("listThreads: %v", err)
	}
	got := map[int64]dto.ThreadView{}
	for _, th := range list.Body.Data.Threads {
		got[th.ID] = th
	}
	assertOpening := func(id int64, status int16, author int64) {
		th, ok := got[id]
		if !ok {
			t.Fatalf("thread %d missing from the list", id)
		}
		if th.OpeningStatus == nil || *th.OpeningStatus != status {
			t.Fatalf("thread %d opening_status: want %d, got %v", id, status, th.OpeningStatus)
		}
		if th.OpeningAuthorID == nil || *th.OpeningAuthorID != author {
			t.Fatalf("thread %d opening_author_id: want %d, got %v", id, author, th.OpeningAuthorID)
		}
	}
	assertOpening(visID, model.PostStatusVisible, 100)
	assertOpening(heldID, model.PostStatusHidden, 700)
	assertOpening(delID, model.PostStatusDeleted, 300)
}

func testBoard(t *testing.T, site, slug string) int64 {
	t.Helper()
	id, err := suitelock.Board(testDB, site, slug)
	if err != nil {
		t.Fatalf("board %s/%s: %v", site, slug, err)
	}
	return id
}
