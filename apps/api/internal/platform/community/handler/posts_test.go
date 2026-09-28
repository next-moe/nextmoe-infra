package handler

import (
	"context"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
)

func resolvePosts(t *testing.T, s *Server, ctx context.Context, ids []int64) []dto.AuthorPostView {
	t.Helper()
	out, err := s.resolvePosts(ctx, &resolvePostsInput{Body: dto.PostsResolveRequest{IDs: ids}})
	if err != nil {
		t.Fatalf("resolvePosts: %v", err)
	}
	return out.Body.Data.Posts
}

func TestPostsResolve_Hydration(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	posts := replyN(t, s, ctx, th, 500, 4)
	visibleA, hidden, deleted, visibleB := posts[0], posts[1], posts[2], posts[3]

	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, hidden).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: deleted, AuthorID: 500}); err != nil {
		t.Fatalf("self-delete: %v", err)
	}

	ctxOther := clientCtx("siteB")
	thB := resolve(t, s, ctxOther, model.AnchorKindSiteGame, "g1")
	otherPost := replyN(t, s, ctxOther, thB, 500, 1)[0]

	const unknown int64 = 9_999_999

	req := []int64{visibleB, hidden, visibleA, deleted, otherPost, unknown, visibleB, visibleA}
	got := resolvePosts(t, s, ctx, req)

	want := []int64{visibleB, visibleA}
	if len(got) != len(want) {
		t.Fatalf("hydration must return %d visible posts, got %d (%+v)", len(want), len(got), got)
	}
	for i, id := range want {
		if got[i].Post.ID != id {
			t.Fatalf("request-order[%d]: want post %d, got %d", i, id, got[i].Post.ID)
		}
		if got[i].Post.Status != model.PostStatusVisible {
			t.Fatalf("resolved post %d must be visible, status=%d", got[i].Post.ID, got[i].Post.Status)
		}
	}

	dup := resolvePosts(t, s, ctx, []int64{visibleA, visibleA, visibleA})
	if len(dup) != 1 || dup[0].Post.ID != visibleA {
		t.Fatalf("duplicate ids must resolve once, got %+v", dup)
	}
}

func TestPostsResolve_CapEmptyProjection(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g7")
	pid := replyN(t, s, ctx, th, 500, 1)[0]
	got := resolvePosts(t, s, ctx, []int64{pid})
	if len(got) != 1 {
		t.Fatalf("one visible id must resolve to one view, got %d", len(got))
	}
	v := got[0]
	if v.Post.ID != pid || v.Thread.ThreadID != th ||
		v.Thread.AnchorKind != model.AnchorKindSiteGame || v.Thread.AnchorID != "g7" {
		t.Fatalf("thread context wrong: %+v", v.Thread)
	}
	if v.Thread.Title != nil {
		t.Fatalf("a comments thread has no title, got %q", *v.Thread.Title)
	}

	boardID := testBoard(t, "letmoe", "b1")
	topicOut, err := s.openTopic(ctx, &openTopicInput{Body: dto.OpenTopicRequest{AuthorID: 500, BoardID: boardID, Title: "hello", Body: "x"}})
	if err != nil {
		t.Fatalf("openTopic: %v", err)
	}
	openingID := topicOut.Body.Data.Post.ID
	tv := resolvePosts(t, s, ctx, []int64{openingID})
	if len(tv) != 1 || tv[0].Thread.Title == nil || *tv[0].Thread.Title != "hello" ||
		tv[0].Thread.AnchorKind != model.AnchorKindBoard || tv[0].Thread.AnchorID != model.BoardAnchorID(boardID) ||
		tv[0].Thread.BoardID == nil || *tv[0].Thread.BoardID != boardID {
		t.Fatalf("board topic context wrong: %+v", tv)
	}

	ids101 := make([]int64, 101)
	for i := range ids101 {
		ids101[i] = int64(1000 + i)
	}
	_, e := s.resolvePosts(ctx, &resolvePostsInput{Body: dto.PostsResolveRequest{IDs: ids101}})
	wantStatus(t, e, 422)

	ids100 := make([]int64, 100)
	for i := range ids100 {
		ids100[i] = int64(2000 + i)
	}
	if got := resolvePosts(t, s, ctx, ids100); len(got) != 0 {
		t.Fatalf("100 unknown ids must resolve to empty, got %d", len(got))
	}

	if got := resolvePosts(t, s, ctx, nil); len(got) != 0 {
		t.Fatalf("nil ids must resolve to empty, got %d", len(got))
	}
	if got := resolvePosts(t, s, ctx, []int64{}); len(got) != 0 {
		t.Fatalf("empty ids must resolve to empty, got %d", len(got))
	}
}

func TestPostsResolve_CrossTenant(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctxA := clientCtx("siteA")
	ctxB := clientCtx("siteB")
	seedTL1(t, 500)

	tA := resolve(t, s, ctxA, model.AnchorKindSiteGame, "g1")
	aPosts := replyN(t, s, ctxA, tA, 500, 3)

	if got := resolvePosts(t, s, ctxB, aPosts); len(got) != 0 {
		t.Fatalf("site B must not resolve site A's posts, got %d", len(got))
	}
	got := resolvePosts(t, s, ctxA, aPosts)
	if len(got) != len(aPosts) {
		t.Fatalf("site A must resolve its own %d posts, got %d", len(aPosts), len(got))
	}
	for i, id := range aPosts {
		if got[i].Post.ID != id {
			t.Fatalf("own resolve order[%d]: want %d, got %d", i, id, got[i].Post.ID)
		}
	}
}

func TestModerationResolvePosts_EveryStatusOwnSiteOnly(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	posts := replyN(t, s, ctx, th, 500, 3)
	visible, hidden, deleted := posts[0], posts[1], posts[2]
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, hidden).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: deleted, AuthorID: 500}); err != nil {
		t.Fatalf("self-delete: %v", err)
	}

	ctxOther := clientCtx("siteB")
	thB := resolve(t, s, ctxOther, model.AnchorKindSiteGame, "g1")
	otherPost := replyN(t, s, ctxOther, thB, 500, 1)[0]

	out, err := s.moderationResolvePosts(ctx, &moderationResolvePostsInput{Body: dto.ModerationPostsResolveRequest{
		IDs: []int64{deleted, otherPost, hidden, 9_999_999, visible, hidden},
	}})
	if err != nil {
		t.Fatalf("moderationResolvePosts: %v", err)
	}
	got := out.Body.Data.Posts
	want := []struct {
		id     int64
		status int16
	}{
		{deleted, model.PostStatusDeleted},
		{hidden, model.PostStatusHidden},
		{visible, model.PostStatusVisible},
	}
	if len(got) != len(want) {
		t.Fatalf("want %d posts (own site, every status, deduped), got %d (%+v)", len(want), len(got), got)
	}
	for i, w := range want {
		if got[i].Post.ID != w.id || got[i].Post.Status != w.status {
			t.Fatalf("[%d] = post %d status %d, want post %d status %d", i, got[i].Post.ID, got[i].Post.Status, w.id, w.status)
		}
		if got[i].Post.AuthorID != 500 || got[i].Thread.ThreadID != th || got[i].Thread.AnchorID != "g1" {
			t.Fatalf("[%d] author/thread context wrong: %+v", i, got[i])
		}
	}
	if got[1].Post.ContentRaw == "" {
		t.Fatal("a hidden post must come back with its content; that is what the moderator is judging")
	}

	if render := resolvePosts(t, s, ctx, []int64{hidden, deleted}); len(render) != 0 {
		t.Fatalf("the render face must still drop hidden and deleted posts, got %d", len(render))
	}

	ids101 := make([]int64, 101)
	for i := range ids101 {
		ids101[i] = int64(1000 + i)
	}
	_, e := s.moderationResolvePosts(ctx, &moderationResolvePostsInput{Body: dto.ModerationPostsResolveRequest{IDs: ids101}})
	wantStatus(t, e, 422)
}
