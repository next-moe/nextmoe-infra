package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
	siteModel "api/internal/platform/site/model"

	"github.com/gofiber/fiber/v3"
)

// Every id-addressed handler must stamp the caller's site onto the context; a
// handler that forgets leaves its guard reading an empty site and waves every
// tenant through.
func TestEngagementHandlers_StampTheCallerSite(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads:    service.NewThreadService(testDB, sink),
		posts:      service.NewPostService(testDB, sink),
		reactions:  service.NewReactionService(testDB),
		trust:      service.NewTrustService(testDB),
		engagement: service.NewEngagementService(testDB),
	}
	mine, theirs := clientCtx("letmoe"), clientCtx("kungal")

	topic, err := s.openTopic(mine, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "t", Body: "opening",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	threadID := topic.Body.Data.Thread.ID

	read, err := s.markThreadRead(mine, &threadReadInput{ID: threadID, Body: dto.ThreadReadRequest{UserID: 300, LastReadPostNumber: 1}})
	if err != nil {
		t.Fatalf("markThreadRead: %v", err)
	}
	if read.Body.Data.LastReadPostNumber != 1 || read.Body.Data.UnreadCount != 0 {
		t.Fatalf("unexpected state: %+v", read.Body.Data)
	}
	if _, err := s.markThreadRead(theirs, &threadReadInput{ID: threadID, Body: dto.ThreadReadRequest{UserID: 300, LastReadPostNumber: 1}}); err == nil {
		t.Fatal("another tenant must not mark this thread read")
	}
	if _, err := s.setThreadNotification(theirs, &threadNotificationInput{ID: threadID, Body: dto.ThreadNotificationRequest{UserID: 300, Level: 3}}); err == nil {
		t.Fatal("another tenant must not set this thread's notification level")
	}

	if _, err := s.setThreadNotification(mine, &threadNotificationInput{ID: threadID, Body: dto.ThreadNotificationRequest{UserID: 300, Level: 9}}); err == nil {
		t.Fatal("an out-of-range level must be refused")
	}

	states, err := s.threadStates(mine, &threadStatesInput{Body: dto.ThreadStatesRequest{UserID: 300, ThreadIDs: []int64{threadID}}})
	if err != nil {
		t.Fatalf("threadStates: %v", err)
	}
	if len(states.Body.Data.States) != 1 {
		t.Fatalf("expected one state row, got %+v", states.Body.Data.States)
	}
	foreign, err := s.threadStates(theirs, &threadStatesInput{Body: dto.ThreadStatesRequest{UserID: 300, ThreadIDs: []int64{threadID}}})
	if err != nil {
		t.Fatalf("threadStates (other tenant): %v", err)
	}
	if len(foreign.Body.Data.States) != 0 {
		t.Fatal("another tenant must not read this thread's state")
	}

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (200, 1, 0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	if _, err := s.reply(mine, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: 200, Body: "r"}}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	unread, err := s.listUnread(mine, &unreadListInput{ID: 300, Kind: -1})
	if err != nil {
		t.Fatalf("listUnread: %v", err)
	}
	if unread.Body.Data.Total != 1 || len(unread.Body.Data.Threads) != 1 {
		t.Fatalf("the reader should have exactly one unread thread: %+v", unread.Body.Data)
	}
	if unread.Body.Data.Threads[0].State.UserID != 300 {
		t.Fatalf("the listed state must name the reader it was asked about: %+v", unread.Body.Data.Threads[0].State)
	}
	if unread.Body.Data.Threads[0].State.UnreadCount != 1 {
		t.Fatalf("unread count should be 1: %+v", unread.Body.Data.Threads[0].State)
	}
	empty, err := s.listUnread(theirs, &unreadListInput{ID: 300, Kind: -1})
	if err != nil {
		t.Fatalf("listUnread (other tenant): %v", err)
	}
	if empty.Body.Data.Total != 0 || len(empty.Body.Data.Threads) != 0 {
		t.Fatalf("another tenant must see none of it: %+v", empty.Body.Data)
	}
}

// Calling listUnread directly leaves `kind` at Go's zero, which is "topics
// only"; only the router applies the declared default of -1. A caller that
// sends neither filter must keep counting every kind, so this goes through it.
func TestUnreadFilters_ThroughRouter(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	threads, posts := service.NewThreadService(testDB, sink), service.NewPostService(testDB, sink)
	engagement := service.NewEngagementService(testDB)
	app := fiber.New()
	app.Use("/api/v1/community", func(c fiber.Ctx) error {
		c.Locals(localClient, &siteModel.OAuthClient{ID: "letmoe", CatalogSite: "letmoe"})
		return c.Next()
	})
	Setup(app, Services{Threads: threads, Posts: posts, Engagement: engagement})

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining)
		 VALUES (100, 1, 0), (200, 1, 0), (300, 1, 0)`).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	ctx := service.WithCallerSite(clientCtx("letmoe"), "letmoe")
	const reader int64 = 300
	opened, _, err := threads.OpenTopic(ctx, service.OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"), Title: "t", BodyRaw: "only opened",
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	posted, _, err := threads.OpenTopic(ctx, service.OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b2"), Title: "t", BodyRaw: "posted in",
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	wall, _, err := posts.Comment(ctx, service.CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1", AuthorID: 100, BodyRaw: "wall",
	})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	for _, id := range []int64{opened.ID, wall.ID} {
		if _, err := engagement.MarkRead(ctx, id, reader, 1); err != nil {
			t.Fatalf("mark read: %v", err)
		}
	}
	if _, err := posts.Reply(ctx, service.ReplyParams{ThreadID: posted.ID, AuthorID: reader, BodyRaw: "mine"}); err != nil {
		t.Fatalf("reader reply: %v", err)
	}
	for _, id := range []int64{opened.ID, posted.ID, wall.ID} {
		if _, err := posts.Reply(ctx, service.ReplyParams{ThreadID: id, AuthorID: 200, BodyRaw: "r"}); err != nil {
			t.Fatalf("reply: %v", err)
		}
	}

	cases := []struct {
		query string
		want  int64
	}{
		{"", 3},
		{"?kind=-1&min_level=1", 3},
		{"?kind=0", 2},
		{"?kind=1", 1},
		{"?min_level=2", 1},
		{"?kind=0&min_level=2", 1},
		{"?kind=1&min_level=2", 0},
		{"?not_a_parameter=1", 3},
	}
	for _, c := range cases {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/v1/community/users/300/unread"+c.query, nil))
		if err != nil {
			t.Fatalf("GET unread%s: %v", c.query, err)
		}
		var out Envelope[dto.UnreadListResponse]
		derr := json.NewDecoder(resp.Body).Decode(&out)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK || derr != nil {
			t.Fatalf("GET unread%s: status %d, decode %v", c.query, resp.StatusCode, derr)
		}
		if out.Data.Total != c.want || int64(len(out.Data.Threads)) != c.want {
			t.Errorf("GET unread%s: total %d with %d threads, want %d of each",
				c.query, out.Data.Total, len(out.Data.Threads), c.want)
		}
	}
}
