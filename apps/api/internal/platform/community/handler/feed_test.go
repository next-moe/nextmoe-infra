package handler

import (
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/service"
)

func feedServer() *Server {
	sink := service.NoopSink{}
	return &Server{
		threads:   service.NewThreadService(testDB, sink),
		posts:     service.NewPostService(testDB, sink),
		reactions: service.NewReactionService(testDB),
		trust:     service.NewTrustService(testDB),
	}
}

func TestListThreads_SortValidation(t *testing.T) {
	cleanTables(t)
	s := feedServer()
	ctx := clientCtx("letmoe")

	for range 3 {
		if _, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
			AuthorID: 100, AnchorID: "1", Title: "t", Body: "x",
		}}); err != nil {
			t.Fatalf("openTopic: %v", err)
		}
	}

	if _, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Sort: "hottest"}); err == nil {
		t.Fatal("an unknown sort must be refused")
	}

	page, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Sort: "activity", Limit: 2})
	if err != nil {
		t.Fatalf("activity page: %v", err)
	}
	cursor := page.Body.Data.NextCursor
	if cursor == "" {
		t.Fatal("a full page must hand back a cursor")
	}
	if _, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Sort: "created", Cursor: cursor, Limit: 2}); err == nil {
		t.Fatal("a cursor minted under one sort must not be replayed under another")
	}
	if _, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Sort: "activity", Cursor: cursor, Limit: 2}); err != nil {
		t.Fatalf("the cursor must still work under its own sort: %v", err)
	}

	legacy, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Limit: 2})
	if err != nil {
		t.Fatalf("default sort: %v", err)
	}
	if _, err := s.listThreads(ctx, &listThreadsInput{Kind: 0, Cursor: legacy.Body.Data.NextCursor, Limit: 2}); err != nil {
		t.Fatalf("a cursor from the sortless call must keep working: %v", err)
	}
}

func TestListSitePosts_FeedAndFilters(t *testing.T) {
	cleanTables(t)
	s := feedServer()
	ctx := clientCtx("letmoe")

	topicOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{
		AuthorID: 100, AnchorID: "1", Title: "t", Body: "opening",
	}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (200, 1, 0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
	for range 3 {
		if _, err := s.reply(ctx, &replyInput{ID: topicOut.Body.Data.Thread.ID, Body: dto.ReplyRequest{AuthorID: 200, Body: "r"}}); err != nil {
			t.Fatalf("reply: %v", err)
		}
	}

	all, err := s.listSitePosts(ctx, &sitePostsInput{Kind: -1, AnchorKind: -1})
	if err != nil {
		t.Fatalf("site posts: %v", err)
	}
	if len(all.Body.Data.Posts) != 4 {
		t.Fatalf("feed should carry the opening post and 3 replies, got %d", len(all.Body.Data.Posts))
	}
	if all.Body.Data.Posts[0].Thread.ThreadID != topicOut.Body.Data.Thread.ID {
		t.Fatal("feed rows must carry thread context")
	}

	replies, err := s.listSitePosts(ctx, &sitePostsInput{Kind: -1, AnchorKind: -1, RepliesOnly: true})
	if err != nil {
		t.Fatalf("replies only: %v", err)
	}
	if len(replies.Body.Data.Posts) != 3 {
		t.Fatalf("replies_only should drop the opening post, got %d", len(replies.Body.Data.Posts))
	}

	seen := map[int64]bool{}
	cursor := ""
	for {
		page, err := s.listSitePosts(ctx, &sitePostsInput{Kind: -1, AnchorKind: -1, Cursor: cursor, Limit: 2})
		if err != nil {
			t.Fatalf("feed page: %v", err)
		}
		for _, p := range page.Body.Data.Posts {
			if seen[p.Post.ID] {
				t.Fatalf("feed keyset returned post %d twice", p.Post.ID)
			}
			seen[p.Post.ID] = true
		}
		cursor = page.Body.Data.NextCursor
		if cursor == "" {
			break
		}
	}
	if len(seen) != 4 {
		t.Fatalf("feed keyset covered %d of 4 posts", len(seen))
	}

	if _, err := s.listSitePosts(ctx, &sitePostsInput{Kind: -1, AnchorKind: -1, Cursor: "not-base64!"}); err == nil {
		t.Fatal("a malformed cursor must be refused")
	}
}
