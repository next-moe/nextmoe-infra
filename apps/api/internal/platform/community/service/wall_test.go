package service

import (
	"context"
	"testing"

	"api/internal/platform/community/model"
)

func mentionsOf(t *testing.T, postID int64) []int64 {
	t.Helper()
	var p model.CommunityPost
	if err := testDB.First(&p, postID).Error; err != nil {
		t.Fatalf("read post: %v", err)
	}
	ids, err := parseMentionIDs(p.MentionedUserIDs)
	if err != nil {
		t.Fatalf("parse mentions: %v", err)
	}
	return ids
}

func mentionNotifs(t *testing.T, userID int64) int {
	t.Helper()
	n := 0
	for _, r := range notifsOf(t, userID) {
		if r.Kind == model.NotificationKindMentioned {
			n++
		}
	}
	return n
}

func TestEditNotifiesNewMentionsOnce(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, B, C, M, X int64 = 100, 200, 300, 900, 400

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	post := replyTo(t, ps, ctx, th.ID, A, nil, []int64{B}, "hi @b")
	if got := mentionsOf(t, post.ID); len(got) != 1 || got[0] != B {
		t.Fatalf("a new post records whom it mentioned: %v", got)
	}
	processBatch(t)

	edit := func(actor int64, asMod bool, ids ...int64) {
		t.Helper()
		if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: actor, BodyRaw: "edited", AsModerator: asMod, MentionUserIDs: ids}); err != nil {
			t.Fatalf("edit: %v", err)
		}
		processBatch(t)
	}
	edit(A, false, B, C)
	if mentionNotifs(t, B) != 1 || mentionNotifs(t, C) != 1 {
		t.Fatalf("the edit tells C, and B only once: B=%d C=%d", mentionNotifs(t, B), mentionNotifs(t, C))
	}
	edit(A, false)
	edit(A, false, C)
	if mentionNotifs(t, C) != 1 {
		t.Fatalf("taking a mention out and back does not notify again: %d", mentionNotifs(t, C))
	}
	edit(M, true, X)
	if mentionNotifs(t, X) != 0 {
		t.Fatalf("a moderator's edit of someone else's post notifies nobody")
	}
	if got := mentionsOf(t, post.ID); len(got) != 2 {
		t.Fatalf("mentioned set is B and C: %v", got)
	}

	if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: A, BodyRaw: "x", MentionUserIDs: make([]int64, 21)}); err != nil {
		t.Fatalf("ids <= 0 are dropped before the limit applies: %v", err)
	}
	many := make([]int64, 21)
	for i := range many {
		many[i] = int64(1000 + i)
	}
	if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: A, BodyRaw: "x", MentionUserIDs: many}); err == nil {
		t.Fatalf("more than 20 mentions is refused")
	}
}

func TestRestoreFollowsTheRemover(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	rs := NewReviewService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const A, N, M int64 = 100, 500, 900

	th := openTopic(t, ts, "letmoe", A, "b1", "opening")
	seedTrust(t, N, model.TrustLevelNew, 1)
	held, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: N, BodyRaw: "held"})
	if err != nil || held.Status != model.PostStatusHidden {
		t.Fatalf("a held first post: %+v %v", held, err)
	}
	var item model.CommunityReviewItem
	if err := testDB.Where("post_id = ?", held.ID).Take(&item).Error; err != nil {
		t.Fatalf("review item: %v", err)
	}
	if _, _, err := ps.Restore(ctx, held.ID, M); err == nil {
		t.Fatalf("a held post is released by the queue, not restored")
	}
	if err := rs.Reject(ctx, item.ID, M); err != nil {
		t.Fatalf("reject: %v", err)
	}
	var p model.CommunityPost
	testDB.First(&p, held.ID)
	if p.DeleteSource != model.DeleteSourceReview || p.DeletedBy == nil || *p.DeletedBy != M {
		t.Fatalf("a rejection records review and the moderator: %+v", p)
	}
	restored, changed, err := ps.Restore(ctx, held.ID, M)
	if err != nil || !changed || restored.Status != model.PostStatusVisible {
		t.Fatalf("a rejection can be undone: %+v %v", restored, err)
	}

	gone := replyTo(t, ps, ctx, th.ID, N, nil, nil, "will be purged")
	if err := ps.Delete(ctx, gone.ID, M, true); err != nil {
		t.Fatalf("mod delete: %v", err)
	}
	if _, err := ps.PurgeAuthor(context.Background(), "letmoe", N); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if _, _, err := ps.Restore(ctx, gone.ID, M); err == nil {
		t.Fatalf("a purged post is not restored by a moderator")
	}
	if _, err := ps.RestoreAuthor(context.Background(), "letmoe", N); err != nil {
		t.Fatalf("purge restore: %v", err)
	}
	var back model.CommunityPost
	testDB.First(&back, gone.ID)
	if back.Status != model.PostStatusDeleted || back.DeleteSource != model.DeleteSourceModerator || back.ContentRaw == "" {
		t.Fatalf("undoing the purge gives back the moderator's tombstone: %+v", back)
	}
	if _, changed, err := ps.Restore(ctx, gone.ID, M); err != nil || !changed {
		t.Fatalf("which a moderator can then restore: %v", err)
	}
}

func TestReviewLeavesAnAuthorsDeleteAlone(t *testing.T) {
	for _, approve := range []bool{true, false} {
		cleanTables(t)
		ts := NewThreadService(testDB, NoopSink{})
		ps := NewPostService(testDB, NoopSink{})
		rs := NewReviewService(testDB, NoopSink{})
		ctx := letmoeCtx()
		const A, N, M int64 = 100, 500, 900

		th := openTopic(t, ts, "letmoe", A, "b1", "opening")
		seedTrust(t, N, model.TrustLevelNew, 1)
		held, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: N, BodyRaw: "held"})
		if err != nil {
			t.Fatalf("held reply: %v", err)
		}
		if err := ps.Delete(ctx, held.ID, N, false); err != nil {
			t.Fatalf("author delete: %v", err)
		}
		var item model.CommunityReviewItem
		if err := testDB.Where("post_id = ?", held.ID).Take(&item).Error; err != nil {
			t.Fatalf("review item: %v", err)
		}
		decide := rs.Reject
		if approve {
			decide = rs.Approve
		}
		if err := decide(ctx, item.ID, M); err != nil {
			t.Fatalf("decide: %v", err)
		}
		var p model.CommunityPost
		testDB.First(&p, held.ID)
		if p.Status != model.PostStatusDeleted || p.DeleteSource != model.DeleteSourceAuthor {
			t.Fatalf("approve=%v: the author's delete stands: %+v", approve, p)
		}
		var decided model.CommunityReviewItem
		testDB.First(&decided, item.ID)
		if decided.Status == model.ReviewStatusPending {
			t.Fatalf("approve=%v: the item is still decided", approve)
		}
	}
}
