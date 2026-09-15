package service

import (
	"context"
	"sync"
	"testing"

	"api/internal/platform/community/model"
)

func countThreads(t *testing.T, anchorKind int16, anchorID string) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityThread{}).
		Where("anchor_kind = ? AND anchor_id = ?", anchorKind, anchorID).Count(&n).Error; err != nil {
		t.Fatalf("count threads: %v", err)
	}
	return n
}

func TestComment_ReadingAnAnchorMintsNothing(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})

	for range 3 {
		th, err := ts.FindCommentsThread("letmoe", model.AnchorKindSiteGame, "g1")
		if err != nil {
			t.Fatalf("find: %v", err)
		}
		if th != nil {
			t.Fatalf("an anchor nobody commented on has no thread, got %d", th.ID)
		}
	}
	if n := countThreads(t, model.AnchorKindSiteGame, "g1"); n != 0 {
		t.Fatalf("reads must not mint a thread, got %d rows", n)
	}
}

func TestComment_FirstCommentCreatesTheThread(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()

	thread, post, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1",
		ContentRating: model.ContentRatingR18, AuthorID: 100, BodyRaw: "first **comment**",
	})
	if err != nil {
		t.Fatalf("first comment: %v", err)
	}
	if post.PostNumber != 1 || post.ThreadID != thread.ID {
		t.Fatalf("first comment should be post 1 of the new thread: %+v", post)
	}
	if thread.Kind != model.ThreadKindComments || thread.CreatedBy != 100 {
		t.Fatalf("thread should be a comments thread created by its first commenter: %+v", thread)
	}
	if thread.ContentRating != model.ContentRatingR18 {
		t.Fatalf("thread should inherit the anchor's rating: %d", thread.ContentRating)
	}
	if thread.PostsCount != 1 || thread.ParticipantsCount != 1 || thread.HighestPostNumber != 1 || thread.LastPostedAt == nil {
		t.Fatalf("counters after the first comment: %+v", thread)
	}
	if post.ContentRating != model.ContentRatingR18 {
		t.Fatalf("post should carry the thread's rating: %d", post.ContentRating)
	}

	second, sp, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1",
		ContentRating: model.ContentRatingAll, AuthorID: 200, BodyRaw: "second",
	})
	if err != nil {
		t.Fatalf("second comment: %v", err)
	}
	if second.ID != thread.ID {
		t.Fatalf("the anchor must keep one thread: %d vs %d", second.ID, thread.ID)
	}
	if sp.PostNumber != 2 || second.PostsCount != 2 || second.ParticipantsCount != 2 {
		t.Fatalf("counters after the second comment: post=%d %+v", sp.PostNumber, second)
	}
	if second.ContentRating != model.ContentRatingR18 {
		t.Fatalf("a later comment must not restate the thread's rating: %d", second.ContentRating)
	}
	if n := countThreads(t, model.AnchorKindSiteGame, "g1"); n != 1 {
		t.Fatalf("exactly one thread should exist, got %d", n)
	}

	found, err := ts.FindCommentsThread("letmoe", model.AnchorKindSiteGame, "g1")
	if err != nil || found == nil || found.ID != thread.ID {
		t.Fatalf("the read face should now find the thread: %+v (%v)", found, err)
	}
}

func TestComment_TenancyMirrorsTheGuard(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()

	a, _, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "7", AuthorID: 1, BodyRaw: "a",
	})
	if err != nil {
		t.Fatalf("site A comment: %v", err)
	}
	b, _, err := ps.Comment(ctx, CommentParams{
		Site: "kungal", AnchorKind: model.AnchorKindSiteGame, AnchorID: "7", AuthorID: 2, BodyRaw: "b",
	})
	if err != nil {
		t.Fatalf("site B comment: %v", err)
	}
	if a.ID == b.ID {
		t.Fatalf("a site-local anchor id belongs to one tenant only, both got %d", a.ID)
	}

	wa, _, err := ps.Comment(ctx, CommentParams{
		Site: "letmoe", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w7", AuthorID: 1, BodyRaw: "a",
	})
	if err != nil {
		t.Fatalf("site A catalog comment: %v", err)
	}
	wb, _, err := ps.Comment(ctx, CommentParams{
		Site: "kungal", AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w7", AuthorID: 2, BodyRaw: "b",
	})
	if err != nil {
		t.Fatalf("site B catalog comment: %v", err)
	}
	if wa.ID != wb.ID {
		t.Fatalf("a catalog anchor shares one conversation network-wide: %d vs %d", wa.ID, wb.ID)
	}
	if wb.PostsCount != 2 {
		t.Fatalf("both sites' comments land in the same thread: %+v", wb)
	}
}

func TestConcurrent_FirstCommentsCreateOneThread(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	const n = 8

	errs := make([]error, n)
	ids := make([]int64, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			th, _, err := ps.Comment(context.Background(), CommentParams{
				Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "race",
				AuthorID: int64(500 + i), BodyRaw: "c",
			})
			if err != nil {
				errs[i] = err
				return
			}
			ids[i] = th.ID
		}(i)
	}
	wg.Wait()

	for i := range n {
		if errs[i] != nil {
			t.Fatalf("comment %d: %v", i, errs[i])
		}
		if ids[i] == 0 || ids[i] != ids[0] {
			t.Fatalf("every first comment must land in the same thread: %v", ids)
		}
	}
	if rows := countThreads(t, model.AnchorKindSiteGame, "race"); rows != 1 {
		t.Fatalf("exactly one comments thread should exist, got %d", rows)
	}
	th := getThread(t, ids[0])
	if th.PostsCount != n || th.HighestPostNumber != n || th.ParticipantsCount != n {
		t.Fatalf("counters after %d concurrent first comments: %+v", n, th)
	}
}
