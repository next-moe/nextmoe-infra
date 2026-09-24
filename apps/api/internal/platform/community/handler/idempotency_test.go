package handler

import (
	"net/http"
	"sync"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
)

func postsInThread(t *testing.T, threadID int64) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityPost{}).Where("thread_id = ?", threadID).Count(&n).Error; err != nil {
		t.Fatalf("count posts: %v", err)
	}
	return n
}

// moyu's BFF times out at 8 s and the user retries; before the key, every
// retry that raced a slow first write landed a second copy of the comment.
func TestCommentRetryWithKeyWritesOnce(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)
	body := dto.CommentRequest{AnchorKind: model.AnchorKindSiteGame, AnchorID: "42", AuthorID: 700, Body: "hello"}

	first, err := s.comment(ctx, &commentInput{IdempotencyKey: "k-1", Body: body})
	if err != nil {
		t.Fatalf("first comment: %v", err)
	}
	if first.Replayed != "" {
		t.Fatalf("a first write must not claim to be a replay, got %q", first.Replayed)
	}
	again, err := s.comment(ctx, &commentInput{IdempotencyKey: "k-1", Body: body})
	if err != nil {
		t.Fatalf("retried comment: %v", err)
	}
	if again.Replayed != "true" || again.Body.Data.Post.ID != first.Body.Data.Post.ID {
		t.Fatalf("retry must replay post %d, got post %d replayed=%q",
			first.Body.Data.Post.ID, again.Body.Data.Post.ID, again.Replayed)
	}
	threadID := first.Body.Data.Thread.ID
	if n := postsInThread(t, threadID); n != 1 {
		t.Fatalf("want 1 post after a retried comment, got %d", n)
	}

	changed := body
	changed.Body = "hello, edited before retry"
	_, err = s.comment(ctx, &commentInput{IdempotencyKey: "k-1", Body: changed})
	wantStatus(t, err, http.StatusConflict)

	if _, err := s.comment(ctx, &commentInput{Body: body}); err != nil {
		t.Fatalf("keyless comment: %v", err)
	}
	if n := postsInThread(t, threadID); n != 2 {
		t.Fatalf("a call without a key must still write, want 2 posts, got %d", n)
	}

	other := clientCtx("letmoe")
	theirs, err := s.comment(other, &commentInput{IdempotencyKey: "k-1", Body: body})
	if err != nil {
		t.Fatalf("other tenant, same key: %v", err)
	}
	if theirs.Replayed != "" || theirs.Body.Data.Thread.ID == threadID {
		t.Fatalf("a key is per site: another tenant's k-1 must write its own comment, got replayed=%q thread=%d",
			theirs.Replayed, theirs.Body.Data.Thread.ID)
	}
}

func TestReplyRetryWithKeyWritesOnce(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)
	thread := resolve(t, s, ctx, model.AnchorKindSiteGame, "43")
	body := dto.ReplyRequest{AuthorID: 700, Body: "a reply"}

	first, err := s.reply(ctx, &replyInput{ID: thread, IdempotencyKey: "r-1", Body: body})
	if err != nil {
		t.Fatalf("first reply: %v", err)
	}
	again, err := s.reply(ctx, &replyInput{ID: thread, IdempotencyKey: "r-1", Body: body})
	if err != nil {
		t.Fatalf("retried reply: %v", err)
	}
	if again.Replayed != "true" || again.Body.Data.Post.ID != first.Body.Data.Post.ID {
		t.Fatalf("retry must replay post %d, got %d replayed=%q", first.Body.Data.Post.ID, again.Body.Data.Post.ID, again.Replayed)
	}
	if n := postsInThread(t, thread); n != 1 {
		t.Fatalf("want 1 post after a retried reply, got %d", n)
	}

	elsewhere := resolve(t, s, ctx, model.AnchorKindSiteGame, "44")
	_, err = s.reply(ctx, &replyInput{ID: elsewhere, IdempotencyKey: "r-1", Body: body})
	wantStatus(t, err, http.StatusConflict)
}

func TestConcurrentRetriesWithOneKeyWriteOnce(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)
	body := dto.CommentRequest{AnchorKind: model.AnchorKindSiteGame, AnchorID: "45", AuthorID: 700, Body: "racing"}

	const n = 8
	ids := make([]int64, n)
	replayed := make([]bool, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			out, err := s.comment(ctx, &commentInput{IdempotencyKey: "race", Body: body})
			errs[i] = err
			if err == nil {
				ids[i], replayed[i] = out.Body.Data.Post.ID, out.Replayed == "true"
			}
		})
	}
	wg.Wait()

	fresh := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("call %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatalf("every call must answer the same post: %v", ids)
		}
		if !replayed[i] {
			fresh++
		}
	}
	if fresh != 1 {
		t.Fatalf("exactly one call writes and the rest replay, got %d fresh writes", fresh)
	}
	var threads []int64
	if err := testDB.Model(&model.CommunityThread{}).Where("anchor_id = ?", "45").Pluck("id", &threads).Error; err != nil || len(threads) != 1 {
		t.Fatalf("want one comments thread, got %v (err %v)", threads, err)
	}
	if got := postsInThread(t, threads[0]); got != 1 {
		t.Fatalf("want 1 post after %d concurrent retries, got %d", n, got)
	}
}

func likesReceived(t *testing.T, userID int64) int32 {
	t.Helper()
	var v []*int32
	if err := testDB.Model(&model.CommunityTrust{}).Where("user_id = ?", userID).Pluck("likes_received", &v).Error; err != nil {
		t.Fatalf("likes_received: %v", err)
	}
	if len(v) == 0 || v[0] == nil {
		return 0
	}
	return *v[0]
}

// A retried toggle undoes the like it retries, so moyu's retry turned every
// slow like into no like.
func TestSetAndUnsetReactionAreIdempotent(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctx := clientCtx("moyu")
	seedTL1(t, 700)
	thread := resolve(t, s, ctx, model.AnchorKindSiteGame, "46")
	post := replyN(t, s, ctx, thread, 700, 1)[0]
	like := dto.ReactionToggleRequest{UserID: 801, Kind: model.ReactionKindLike}

	for i, wantChanged := range []bool{true, false} {
		out, err := s.setReaction(ctx, &toggleReactionInput{ID: post, Body: like})
		if err != nil {
			t.Fatalf("PUT %d: %v", i, err)
		}
		d := out.Body.Data
		if !d.Added || d.Changed != wantChanged || d.ReactionCount != 1 {
			t.Fatalf("PUT %d: want added=true changed=%v count=1, got %+v", i, wantChanged, d)
		}
	}
	if got := likesReceived(t, 700); got != 1 {
		t.Fatalf("a repeated PUT must credit the author once, likes_received=%d", got)
	}

	for i, wantChanged := range []bool{true, false} {
		out, err := s.unsetReaction(ctx, &unsetReactionInput{ID: post, UserID: 801, Kind: model.ReactionKindLike})
		if err != nil {
			t.Fatalf("DELETE %d: %v", i, err)
		}
		d := out.Body.Data
		if d.Added || d.Changed != wantChanged || d.ReactionCount != 0 {
			t.Fatalf("DELETE %d: want added=false changed=%v count=0, got %+v", i, wantChanged, d)
		}
	}
	if got := likesReceived(t, 700); got != 0 {
		t.Fatalf("a repeated DELETE must debit the author once, likes_received=%d", got)
	}

	out, err := s.toggleReaction(ctx, &toggleReactionInput{ID: post, Body: like})
	if err != nil || !out.Body.Data.Added || !out.Body.Data.Changed {
		t.Fatalf("the toggle keeps working for its other callers: %+v err=%v", out, err)
	}

	_, err = s.setReaction(clientCtx("letmoe"), &toggleReactionInput{ID: post, Body: like})
	wantStatus(t, err, http.StatusNotFound)
}
