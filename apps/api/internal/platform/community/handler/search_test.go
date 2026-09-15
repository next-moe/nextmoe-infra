package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func TestSearch_HandlerFaces(t *testing.T) {
	cleanTables(t)
	sink := service.NoopSink{}
	s := &Server{
		threads: service.NewThreadService(testDB, sink),
		posts:   service.NewPostService(testDB, sink),
		trust:   service.NewTrustService(testDB),
		search:  service.NewSearchService(testDB),
	}
	ctx := clientCtx("letmoe")

	topic, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, AnchorID: "1", Title: "夏日回忆 攻略", Body: "推荐这条路线",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}

	posts, err := s.searchPosts(ctx, &searchInput{Q: "路线", Kind: -1})
	if err != nil {
		t.Fatalf("searchPosts: %v", err)
	}
	if len(posts.Body.Data.Posts) != 1 || posts.Body.Data.Posts[0].Thread.ThreadID != topic.Body.Data.Thread.ID {
		t.Fatalf("expected the post with its thread context: %+v", posts.Body.Data.Posts)
	}

	threads, err := s.searchThreads(ctx, &searchInput{Q: "夏日", Kind: -1})
	if err != nil {
		t.Fatalf("searchThreads: %v", err)
	}
	if len(threads.Body.Data.Threads) != 1 {
		t.Fatalf("expected one title hit, got %d", len(threads.Body.Data.Threads))
	}
	hit := threads.Body.Data.Threads[0]
	if hit.OpeningStatus == nil || hit.OpeningAuthorID == nil {
		t.Fatal("a title hit must carry its opening-post status, or a held opening post leaks its title here")
	}

	if _, err := s.searchPosts(ctx, &searchInput{Q: "路", Kind: -1}); err == nil {
		t.Fatal("a one-character query must be refused")
	}
	if _, err := s.searchThreads(ctx, &searchInput{Q: "夏", Kind: -1}); err == nil {
		t.Fatal("a one-character thread query must be refused")
	}

	if _, err := s.searchThreads(ctx, &searchInput{Q: "夏日", Kind: -1, Cursor: "bogus~~"}); err == nil {
		t.Fatal("a malformed cursor must be refused")
	}
	activity, err := s.listThreads(ctx, &listThreadsInput{Kind: model.ThreadKindTopic, Limit: 1})
	if err != nil {
		t.Fatalf("listThreads: %v", err)
	}
	if cur := activity.Body.Data.NextCursor; cur != "" {
		if _, err := s.searchThreads(ctx, &searchInput{Q: "夏日", Kind: -1, Cursor: cur}); err == nil {
			t.Fatal("an activity cursor must not be replayed against search, which orders by creation")
		}
	}
}
