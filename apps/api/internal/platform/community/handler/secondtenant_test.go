package handler

import (
	"context"
	"testing"

	"api/internal/platform/community/dto"
	"api/internal/platform/community/model"
	"api/internal/platform/community/service"
)

func newTenantServer() *Server {
	sink := service.NoopSink{}
	return &Server{
		threads:    service.NewThreadService(testDB, sink),
		posts:      service.NewPostService(testDB, sink),
		reactions:  service.NewReactionService(testDB),
		feedback:   service.NewFeedbackService(testDB, sink),
		flags:      service.NewFlagService(testDB, sink),
		trust:      service.NewTrustService(testDB),
		review:     service.NewReviewService(testDB, sink),
		engagement: service.NewEngagementService(testDB),
		search:     service.NewSearchService(testDB),
		boards:     service.NewBoardService(testDB),
		notify:     service.NewNotificationService(testDB),
	}
}

func wantStatus(t *testing.T, err error, want int) {
	t.Helper()
	if err == nil {
		t.Fatalf("want HTTP %d, got nil error", want)
	}
	se, ok := err.(interface{ GetStatus() int })
	if !ok {
		t.Fatalf("error is not a status error: %v", err)
	}
	if se.GetStatus() != want {
		t.Fatalf("want HTTP %d, got %d (%v)", want, se.GetStatus(), err)
	}
}

func seedTL1(t *testing.T, userID int64) {
	t.Helper()
	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (?, 1, 0)
		 ON CONFLICT (user_id) DO UPDATE SET level = 1, first_posts_held_remaining = 0`,
		userID,
	).Error; err != nil {
		t.Fatalf("seed trust %d: %v", userID, err)
	}
}

func pendingReviewItemID(t *testing.T, site string) int64 {
	t.Helper()
	var id int64
	if err := testDB.Raw(
		`SELECT id FROM community_review_item WHERE site = ? AND status = 0 ORDER BY id DESC LIMIT 1`, site,
	).Scan(&id).Error; err != nil {
		t.Fatalf("query pending review item for %s: %v", site, err)
	}
	if id == 0 {
		t.Fatalf("no pending review item for site %s", site)
	}
	return id
}

func resolve(t *testing.T, s *Server, ctx context.Context, anchorKind int16, anchorID string) int64 {
	t.Helper()
	out, err := s.resolveComments(ctx, &resolveCommentsInput{Body: dto.CommentsResolveRequest{AnchorKind: anchorKind, AnchorID: anchorID}})
	if err != nil {
		t.Fatalf("resolveComments(%s, %d, %q): %v", callerOf(ctx), anchorKind, anchorID, err)
	}
	return out.Body.Data.Thread.ID
}

func callerOf(ctx context.Context) string {
	if c := clientFromCtx(ctx); c != nil {
		return c.CatalogSite
	}
	return "?"
}

func TestSecondTenant_AnchorScoping(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctxA := clientCtx("siteA")
	ctxB := clientCtx("siteB")

	tA := resolve(t, s, ctxA, model.AnchorKindSiteGame, "123")
	tB := resolve(t, s, ctxB, model.AnchorKindSiteGame, "123")
	if tA == tB {
		t.Fatalf("site-local anchor 123 must resolve to distinct threads per site, both got %d", tA)
	}
	if again := resolve(t, s, ctxA, model.AnchorKindSiteGame, "123"); again != tA {
		t.Fatalf("same-site resolve must be idempotent: %d vs %d", again, tA)
	}

	wA := resolve(t, s, ctxA, model.AnchorKindCatalogWork, "w345")
	wB := resolve(t, s, ctxB, model.AnchorKindCatalogWork, "w345")
	if wA != wB {
		t.Fatalf("catalog anchor w345 must be ONE shared thread cross-site: %d vs %d", wA, wB)
	}

	if _, err := s.getThread(ctxB, &threadPostsInput{ID: wB}); err != nil {
		t.Fatalf("siteB must be able to read the shared catalog thread (guard exempt): %v", err)
	}
}

func TestSecondTenant_CrossTenantGuard(t *testing.T) {
	cleanTables(t)
	s := newTenantServer()
	ctxA := clientCtx("siteA")
	ctxB := clientCtx("siteB")
	seedTL1(t, 100)

	tA := resolve(t, s, ctxA, model.AnchorKindSiteGame, "500")
	replyOut, err := s.reply(ctxA, &replyInput{ID: tA, Body: dto.ReplyRequest{AuthorID: 100, Body: "hello from A"}})
	if err != nil {
		t.Fatalf("A reply: %v", err)
	}
	postID := replyOut.Body.Data.Post.ID

	_, e := s.getThread(ctxB, &threadPostsInput{ID: tA})
	wantStatus(t, e, 404)
	_, e = s.listPosts(ctxB, &threadPostsInput{ID: tA})
	wantStatus(t, e, 404)
	_, e = s.reply(ctxB, &replyInput{ID: tA, Body: dto.ReplyRequest{AuthorID: 200, Body: "intruder"}})
	wantStatus(t, e, 404)
	_, e = s.editPost(ctxB, &editPostInput{ID: postID, Body: dto.EditPostRequest{AuthorID: 100, Body: "hijack"}})
	wantStatus(t, e, 404)
	_, e = s.toggleReaction(ctxB, &toggleReactionInput{ID: postID, Body: dto.ReactionToggleRequest{UserID: 200, Kind: model.ReactionKindLike}})
	wantStatus(t, e, 404)
	_, e = s.submitFlag(ctxB, &flagInput{ID: postID, Body: dto.FlagRequest{FlaggerID: 200}})
	wantStatus(t, e, 404)
	_, e = s.deletePost(ctxB, &deletePostInput{ID: postID, AuthorID: 100})
	wantStatus(t, e, 404)

	if p := getPostRow(t, postID); p.Status != model.PostStatusVisible {
		t.Fatalf("post must stay visible after cross-tenant probes, status=%d", p.Status)
	}
	if n := countFlags(t, postID); n != 0 {
		t.Fatalf("cross-tenant flag must not be recorded, got %d flags", n)
	}

	if _, err := s.getThread(ctxA, &threadPostsInput{ID: tA}); err != nil {
		t.Fatalf("A getThread own: %v", err)
	}
	if _, err := s.listPosts(ctxA, &threadPostsInput{ID: tA}); err != nil {
		t.Fatalf("A listPosts own: %v", err)
	}
	if _, err := s.toggleReaction(ctxA, &toggleReactionInput{ID: postID, Body: dto.ReactionToggleRequest{UserID: 200, Kind: model.ReactionKindLike}}); err != nil {
		t.Fatalf("A reaction own: %v", err)
	}
	if _, err := s.editPost(ctxA, &editPostInput{ID: postID, Body: dto.EditPostRequest{AuthorID: 100, Body: "edited by A"}}); err != nil {
		t.Fatalf("A edit own: %v", err)
	}
	if _, err := s.deletePost(ctxA, &deletePostInput{ID: postID, AuthorID: 100}); err != nil {
		t.Fatalf("A delete own: %v", err)
	}

	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (700, 0, 2)
		 ON CONFLICT (user_id) DO UPDATE SET level = 0, first_posts_held_remaining = 2`,
	).Error; err != nil {
		t.Fatalf("seed held author: %v", err)
	}
	if _, err := s.reply(ctxA, &replyInput{ID: tA, Body: dto.ReplyRequest{AuthorID: 700, Body: "held newcomer post"}}); err != nil {
		t.Fatalf("A held reply: %v", err)
	}
	itemID := pendingReviewItemID(t, "siteA")
	_, e = s.approveReview(ctxB, &reviewDecisionInput{ID: itemID, Body: dto.ReviewDecisionRequest{DecidedBy: 900}})
	wantStatus(t, e, 404)
	_, e = s.rejectReview(ctxB, &reviewDecisionInput{ID: itemID, Body: dto.ReviewDecisionRequest{DecidedBy: 900}})
	wantStatus(t, e, 404)
	if _, err := s.approveReview(ctxA, &reviewDecisionInput{ID: itemID, Body: dto.ReviewDecisionRequest{DecidedBy: 900}}); err != nil {
		t.Fatalf("A approve own: %v", err)
	}

	fbOut, err := s.openFeedback(ctxA, &openFeedbackInput{Body: dto.OpenFeedbackRequest{AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "fb1", Title: "bug", Body: "x"}})
	if err != nil {
		t.Fatalf("A openFeedback: %v", err)
	}
	fbID := fbOut.Body.Data.Thread.ID
	if _, err := s.setFeedbackStatus(ctxB, &feedbackStatusInput{ID: fbID, Body: dto.FeedbackStatusRequest{FbStatus: model.FeedbackStatusFixed, ResponderID: 900}}); err == nil {
		t.Fatal("B setting A's feedback status must be rejected")
	}
	if st := feedbackStatus(t, fbID); st == nil || *st != model.FeedbackStatusOpen {
		t.Fatalf("A's feedback status must be unchanged (open) after B's attempt, got %v", st)
	}
	if _, err := s.setFeedbackStatus(ctxA, &feedbackStatusInput{ID: fbID, Body: dto.FeedbackStatusRequest{FbStatus: model.FeedbackStatusFixed, ResponderID: 100}}); err != nil {
		t.Fatalf("A setFeedbackStatus own: %v", err)
	}
}

func getPostRow(t *testing.T, id int64) *model.CommunityPost {
	t.Helper()
	var p model.CommunityPost
	if err := testDB.First(&p, id).Error; err != nil {
		t.Fatalf("reload post %d: %v", id, err)
	}
	return &p
}

func countFlags(t *testing.T, postID int64) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityFlag{}).Where("post_id = ?", postID).Count(&n).Error; err != nil {
		t.Fatalf("count flags: %v", err)
	}
	return n
}

func feedbackStatus(t *testing.T, threadID int64) *int16 {
	t.Helper()
	var th model.CommunityThread
	if err := testDB.First(&th, threadID).Error; err != nil {
		t.Fatalf("reload feedback thread %d: %v", threadID, err)
	}
	return th.FbStatus
}
