package handler

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
)

func replyN(t *testing.T, s *Server, ctx context.Context, threadID, author int64, n int) []int64 {
	t.Helper()
	ids := make([]int64, 0, n)
	for i := range n {
		out, err := s.reply(ctx, &replyInput{ID: threadID, Body: dto.ReplyRequest{AuthorID: author, Body: fmt.Sprintf("post %d", i)}})
		if err != nil {
			t.Fatalf("reply %d: %v", i, err)
		}
		ids = append(ids, out.Body.Data.Post.ID)
	}
	return ids
}

func pageAuthorPosts(t *testing.T, s *Server, ctx context.Context, authorID int64, anchorKind int16, limit int) []dto.AuthorPostView {
	t.Helper()
	var all []dto.AuthorPostView
	var after int64
	for {
		out, err := s.listAuthorPosts(ctx, &authorPostsInput{ID: authorID, After: after, AnchorKind: anchorKind, Limit: limit})
		if err != nil {
			t.Fatalf("listAuthorPosts: %v", err)
		}
		page := out.Body.Data.Posts
		if len(page) == 0 {
			break
		}
		all = append(all, page...)
		if len(page) < limit {
			break
		}
		after = page[len(page)-1].Post.ID
	}
	return all
}

func addReaction(t *testing.T, postID, userID int64) {
	t.Helper()
	if err := testDB.Create(&model.CommunityReaction{PostID: postID, UserID: userID, Kind: model.ReactionKindLike}).Error; err != nil {
		t.Fatalf("add reaction (post %d, user %d): %v", postID, userID, err)
	}
}

func countReactionsByUser(t *testing.T, userID int64) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityReaction{}).Where("user_id = ?", userID).Count(&n).Error; err != nil {
		t.Fatalf("count reactions of %d: %v", userID, err)
	}
	return n
}

func TestAuthorPosts_KeysetAndEmpty(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	want := replyN(t, s, ctx, th, 500, 7)
	wantSet := map[int64]bool{}
	for _, id := range want {
		wantSet[id] = true
	}

	got := pageAuthorPosts(t, s, ctx, 500, -1, 3)
	if len(got) != len(want) {
		t.Fatalf("keyset should return all %d posts, got %d", len(want), len(got))
	}
	seen := map[int64]bool{}
	var prev int64
	for i, v := range got {
		id := v.Post.ID
		if seen[id] {
			t.Fatalf("keyset returned duplicate id %d", id)
		}
		seen[id] = true
		if !wantSet[id] {
			t.Fatalf("keyset returned unexpected id %d", id)
		}
		if i > 0 && id >= prev {
			t.Fatalf("keyset must be strictly descending by id: %d then %d", prev, id)
		}
		prev = id
		if v.Post.ThreadID != th || v.Thread.ThreadID != th || v.Thread.AnchorKind != model.AnchorKindSiteGame || v.Thread.AnchorID != "g1" {
			t.Fatalf("thread context wrong: %+v", v.Thread)
		}
	}
	if len(seen) != len(want) {
		t.Fatalf("keyset dropped posts: covered %d of %d", len(seen), len(want))
	}

	if got := pageAuthorPosts(t, s, ctx, 999, -1, 20); len(got) != 0 {
		t.Fatalf("unknown author must be empty, got %d", len(got))
	}
}

func TestAuthorPosts_AnchorKindFilterAndStatus(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)

	gameThread := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	resThread := resolve(t, s, ctx, model.AnchorKindSiteResource, "r1")
	gamePosts := replyN(t, s, ctx, gameThread, 500, 3)
	resPosts := replyN(t, s, ctx, resThread, 500, 2)

	if got := pageAuthorPosts(t, s, ctx, 500, -1, 20); len(got) != 5 {
		t.Fatalf("no filter should return 5, got %d", len(got))
	}
	game := pageAuthorPosts(t, s, ctx, 500, model.AnchorKindSiteGame, 20)
	if len(game) != 3 {
		t.Fatalf("anchor_kind=1 should return 3, got %d", len(game))
	}
	for _, v := range game {
		if v.Thread.AnchorKind != model.AnchorKindSiteGame {
			t.Fatalf("filtered result leaked anchor_kind %d", v.Thread.AnchorKind)
		}
	}
	if got := pageAuthorPosts(t, s, ctx, 500, model.AnchorKindSiteResource, 20); len(got) != 2 {
		t.Fatalf("anchor_kind=2 should return 2, got %d", len(got))
	}

	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, gamePosts[0]).Error; err != nil {
		t.Fatalf("hide post: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: gamePosts[1], AuthorID: 500}); err != nil {
		t.Fatalf("delete post: %v", err)
	}
	visible := pageAuthorPosts(t, s, ctx, 500, model.AnchorKindSiteGame, 20)
	if len(visible) != 1 || visible[0].Post.ID != gamePosts[2] {
		t.Fatalf("only the untouched game post should remain visible, got %+v", visible)
	}
	_ = resPosts
}

func TestAuthorStats(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)
	seedTL1(t, 600)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	p500 := replyN(t, s, ctx, th, 500, 3)
	replyN(t, s, ctx, th, 600, 1)
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, p500[0]).Error; err != nil {
		t.Fatalf("hide post: %v", err)
	}

	out, err := s.authorStats(ctx, &authorStatsInput{IDs: "500,600,700"})
	if err != nil {
		t.Fatalf("authorStats: %v", err)
	}
	got := out.Body.Data.Stats
	want := []dto.AuthorStat{{AuthorID: 500, VisiblePosts: 2}, {AuthorID: 600, VisiblePosts: 1}, {AuthorID: 700, VisiblePosts: 0}}
	if len(got) != len(want) {
		t.Fatalf("stats length: want %d, got %d (%+v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("stats[%d]: want %+v, got %+v", i, want[i], got[i])
		}
	}

	var b strings.Builder
	for i := range 101 {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Itoa(1000 + i))
	}
	_, e := s.authorStats(ctx, &authorStatsInput{IDs: b.String()})
	wantStatus(t, e, 422)

	_, e = s.authorStats(ctx, &authorStatsInput{IDs: "500,abc,600"})
	wantStatus(t, e, 400)

	empty, err := s.authorStats(ctx, &authorStatsInput{IDs: ""})
	if err != nil || len(empty.Body.Data.Stats) != 0 {
		t.Fatalf("empty ids should be empty stats: err=%v got=%+v", err, empty.Body.Data.Stats)
	}
}

func TestAuthorPurge(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("letmoe")
	seedTL1(t, 500)
	seedTL1(t, 600)

	th := resolve(t, s, ctx, model.AnchorKindSiteGame, "g1")
	posts := replyN(t, s, ctx, th, 500, 3)
	other := replyN(t, s, ctx, th, 600, 1)

	visible, hidden, deleted := posts[0], posts[1], posts[2]
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, hidden).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if _, err := s.deletePost(ctx, &deletePostInput{ID: deleted, AuthorID: 500}); err != nil {
		t.Fatalf("self-delete: %v", err)
	}
	nums := map[int64]int32{}
	for _, id := range posts {
		nums[id] = getPostRow(t, id).PostNumber
	}

	addReaction(t, visible, 500)
	addReaction(t, other[0], 500)
	addReaction(t, visible, 700)

	res, err := s.purgeAuthor(ctx, &authorPurgeInput{ID: 500})
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if res.Body.Data.PostsPurged != 3 || res.Body.Data.ReactionsDeleted != 2 {
		t.Fatalf("purge counts: want {3,2}, got {%d,%d}", res.Body.Data.PostsPurged, res.Body.Data.ReactionsDeleted)
	}
	// Posting subscribed 500 to the thread, so the purge has a read-state row to
	// clear; leaving it would keep a record of which threads they opened.
	if res.Body.Data.ReadStatesDeleted != 1 {
		t.Fatalf("purge must clear the author's read state, got %d", res.Body.Data.ReadStatesDeleted)
	}
	if n := countReadStates(t, 500); n != 0 {
		t.Fatalf("author 500 read states must be gone, got %d", n)
	}
	if n := countReadStates(t, 600); n != 1 {
		t.Fatalf("another author's read state must survive, got %d", n)
	}

	for _, id := range posts {
		p := getPostRow(t, id)
		if p.Status != model.PostStatusDeleted || p.ContentRaw != "" || p.ContentHTML != "" {
			t.Fatalf("post %d not purged: status=%d raw=%q html=%q", id, p.Status, p.ContentRaw, p.ContentHTML)
		}
		if p.PostNumber != nums[id] {
			t.Fatalf("post %d number changed: %d -> %d", id, nums[id], p.PostNumber)
		}
	}
	if p := getPostRow(t, other[0]); p.Status != model.PostStatusVisible || p.ContentRaw == "" {
		t.Fatalf("other author's post must be untouched: status=%d raw=%q", p.Status, p.ContentRaw)
	}
	if n := countReactionsByUser(t, 500); n != 0 {
		t.Fatalf("author 500 reactions must be deleted, got %d", n)
	}
	if n := countReactionsByUser(t, 700); n != 1 {
		t.Fatalf("stranger 700 reaction must survive, got %d", n)
	}

	again, err := s.purgeAuthor(ctx, &authorPurgeInput{ID: 500})
	if err != nil {
		t.Fatalf("purge again: %v", err)
	}
	if again.Body.Data.PostsPurged != 0 || again.Body.Data.ReactionsDeleted != 0 || again.Body.Data.ReadStatesDeleted != 0 {
		t.Fatalf("second purge must be 0/0/0, got {%d,%d,%d}",
			again.Body.Data.PostsPurged, again.Body.Data.ReactionsDeleted, again.Body.Data.ReadStatesDeleted)
	}
}

func countReadStates(t *testing.T, userID int64) int64 {
	t.Helper()
	var n int64
	if err := testDB.Raw(
		`SELECT count(*) FROM community_thread_user WHERE user_id = ?`, userID).Scan(&n).Error; err != nil {
		t.Fatalf("count read states: %v", err)
	}
	return n
}

func TestAuthorCrossTenant(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctxA := clientCtx("siteA")
	ctxB := clientCtx("siteB")
	seedTL1(t, 500)

	tA := resolve(t, s, ctxA, model.AnchorKindSiteGame, "g1")
	tB := resolve(t, s, ctxB, model.AnchorKindSiteGame, "g1")
	replyN(t, s, ctxA, tA, 500, 2)
	replyN(t, s, ctxB, tB, 500, 3)

	if got := pageAuthorPosts(t, s, ctxA, 500, -1, 20); len(got) != 2 {
		t.Fatalf("site A must see only its 2 posts, got %d", len(got))
	}
	stat, err := s.authorStats(ctxA, &authorStatsInput{IDs: "500"})
	if err != nil {
		t.Fatalf("stats A: %v", err)
	}
	if stat.Body.Data.Stats[0].VisiblePosts != 2 {
		t.Fatalf("site A stat must be 2, got %d", stat.Body.Data.Stats[0].VisiblePosts)
	}
	res, err := s.purgeAuthor(ctxA, &authorPurgeInput{ID: 500})
	if err != nil {
		t.Fatalf("purge A: %v", err)
	}
	if res.Body.Data.PostsPurged != 2 {
		t.Fatalf("site A purge must affect 2, got %d", res.Body.Data.PostsPurged)
	}
	if got := pageAuthorPosts(t, s, ctxB, 500, -1, 20); len(got) != 3 {
		t.Fatalf("site B posts must survive A's purge, got %d", len(got))
	}
}
