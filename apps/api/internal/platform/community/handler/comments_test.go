package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func commentsServer() *Server {
	sink := service.NoopSink{}
	return &Server{
		threads:   service.NewThreadService(testDB, sink),
		posts:     service.NewPostService(testDB, sink),
		reactions: service.NewReactionService(testDB),
		trust:     service.NewTrustService(testDB),
	}
}

func countCommentThreads(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityThread{}).
		Where("kind = ?", model.ThreadKindComments).Count(&n).Error; err != nil {
		t.Fatalf("count comments threads: %v", err)
	}
	return n
}

func TestComments_ReadDoesNotMintAThread(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("letmoe")

	out, err := s.getComments(ctx, &commentsPageInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1"})
	if err != nil {
		t.Fatalf("getComments: %v", err)
	}
	if out.Body.Data.Thread != nil {
		t.Fatalf("an anchor nobody commented on carries no thread: %+v", out.Body.Data.Thread)
	}
	if out.Body.Data.Posts == nil || len(out.Body.Data.Posts) != 0 {
		t.Fatalf("posts should be an empty array, got %+v", out.Body.Data.Posts)
	}
	if n := countCommentThreads(t); n != 0 {
		t.Fatalf("the read face must write nothing, got %d threads", n)
	}
}

func TestComments_WriteThenRead(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("letmoe")

	wrote, err := s.comment(ctx, &commentInput{Body: dto.CommentRequest{
		AnchorKind: model.AnchorKindSiteResource, AnchorID: "website:12", ContentRating: model.ContentRatingAll,
		AuthorID: 100, Body: "hello **world**",
	}})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	if wrote.Body.Data.Post == nil || wrote.Body.Data.Post.PostNumber != 1 {
		t.Fatalf("the first comment is post 1: %+v", wrote.Body.Data.Post)
	}
	threadID := wrote.Body.Data.Thread.ID
	if threadID == 0 || wrote.Body.Data.Thread.PostsCount != 1 {
		t.Fatalf("the response should carry the thread as it now stands: %+v", wrote.Body.Data.Thread)
	}

	read, err := s.getComments(ctx, &commentsPageInput{AnchorKind: model.AnchorKindSiteResource, AnchorID: "website:12"})
	if err != nil {
		t.Fatalf("getComments: %v", err)
	}
	if read.Body.Data.Thread == nil || read.Body.Data.Thread.ID != threadID {
		t.Fatalf("the read face should find the thread the comment created: %+v", read.Body.Data.Thread)
	}
	if len(read.Body.Data.Posts) != 1 || read.Body.Data.Posts[0].ContentHTML == "" {
		t.Fatalf("the comment should come back cooked: %+v", read.Body.Data.Posts)
	}
	if n := countCommentThreads(t); n != 1 {
		t.Fatalf("one anchor, one thread, got %d", n)
	}
}

func TestComments_AnchorIsValidated(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctx := clientCtx("letmoe")

	bad := []struct {
		name       string
		anchorKind int16
		anchorID   string
	}{
		{"board", model.AnchorKindBoard, "b1"},
		{"unknown kind", 9, "x"},
		{"no anchor", model.AnchorKindSiteGame, ""},
	}
	for _, c := range bad {
		if _, err := s.getComments(ctx, &commentsPageInput{AnchorKind: c.anchorKind, AnchorID: c.anchorID}); err == nil {
			t.Errorf("getComments(%s) should be refused", c.name)
		}
		if _, err := s.comment(ctx, &commentInput{Body: dto.CommentRequest{
			AnchorKind: c.anchorKind, AnchorID: c.anchorID, AuthorID: 1, Body: "x",
		}}); err == nil {
			t.Errorf("comment(%s) should be refused", c.name)
		}
	}
	if n := countCommentThreads(t); n != 0 {
		t.Fatalf("a refused comment must write nothing, got %d threads", n)
	}
}

func TestComments_SiteLocalAnchorStaysInItsTenant(t *testing.T) {
	cleanTables(t)
	s := commentsServer()
	ctxA, ctxB := clientCtx("letmoe"), clientCtx("kungal")

	if _, err := s.comment(ctxA, &commentInput{Body: dto.CommentRequest{
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "42", AuthorID: 100, Body: "site A",
	}}); err != nil {
		t.Fatalf("site A comment: %v", err)
	}
	readB, err := s.getComments(ctxB, &commentsPageInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "42"})
	if err != nil {
		t.Fatalf("site B read: %v", err)
	}
	if readB.Body.Data.Thread != nil {
		t.Fatalf("site B must not see site A's game 42: %+v", readB.Body.Data.Thread)
	}

	wroteB, err := s.comment(ctxB, &commentInput{Body: dto.CommentRequest{
		AnchorKind: model.AnchorKindSiteGame, AnchorID: "42", AuthorID: 200, Body: "site B",
	}})
	if err != nil {
		t.Fatalf("site B comment: %v", err)
	}
	if wroteB.Body.Data.Thread.PostsCount != 1 {
		t.Fatalf("site B opens its own conversation: %+v", wroteB.Body.Data.Thread)
	}
	if n := countCommentThreads(t); n != 2 {
		t.Fatalf("each tenant keeps its own thread for local anchor 42, got %d", n)
	}
}
