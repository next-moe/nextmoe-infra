package service

import (
	"context"
	"strings"
	"testing"

	"api/internal/platform/community/model"
)

func TestGetOrCreateCommentsThread_Idempotent(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	p := CommentsThreadParams{Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g1", ContentRating: model.ContentRatingAll, ActorID: 1}

	a, err := ts.GetOrCreateCommentsThread(context.Background(), p)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := ts.GetOrCreateCommentsThread(context.Background(), p)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.ID != b.ID {
		t.Fatalf("comments thread should be idempotent per anchor: %d vs %d", a.ID, b.ID)
	}
	if a.ParticipantsCount != 0 || a.PostsCount != 0 {
		t.Fatalf("comments thread born empty: posts=%d participants=%d", a.PostsCount, a.ParticipantsCount)
	}

	if err := testDB.Model(a).Update("status", model.ThreadStatusDeleted).Error; err != nil {
		t.Fatalf("tombstone: %v", err)
	}
	c, err := ts.GetOrCreateCommentsThread(context.Background(), p)
	if err != nil {
		t.Fatalf("after tombstone: %v", err)
	}
	if c.ID == a.ID {
		t.Fatal("expected a new thread id after tombstoning the prior one")
	}
}

func TestReply_CountersAndNumbering(t *testing.T) {
	cleanTables(t)
	sink := &recordingSink{}
	ts := NewThreadService(testDB, sink)
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	if th.PostsCount != 1 || th.HighestPostNumber != 1 || th.ParticipantsCount != 1 {
		t.Fatalf("topic born with post #1: posts=%d highest=%d participants=%d", th.PostsCount, th.HighestPostNumber, th.ParticipantsCount)
	}

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	p2, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "hi"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if p2.PostNumber != 2 {
		t.Fatalf("post_number: want 2, got %d", p2.PostNumber)
	}
	reloaded := getThread(t, th.ID)
	if reloaded.PostsCount != 2 || reloaded.HighestPostNumber != 2 || reloaded.ParticipantsCount != 2 {
		t.Fatalf("after reply: posts=%d highest=%d participants=%d", reloaded.PostsCount, reloaded.HighestPostNumber, reloaded.ParticipantsCount)
	}
	if reloaded.LastPostedAt == nil || !reloaded.LastPostedAt.After(*th.LastPostedAt) {
		t.Fatal("last_posted_at should advance on reply")
	}

	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "again"}); err != nil {
		t.Fatalf("reply2: %v", err)
	}
	reloaded = getThread(t, th.ID)
	if reloaded.PostsCount != 3 || reloaded.ParticipantsCount != 2 {
		t.Fatalf("same-author reply must not add a participant: posts=%d participants=%d", reloaded.PostsCount, reloaded.ParticipantsCount)
	}

	if sink.count(EventPostCreated) != 3 {
		t.Fatalf("expected 3 post.created events, got %d", sink.count(EventPostCreated))
	}
}

func TestReply_Sanitization(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "hi <script>alert(1)</script> **bold**"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if strings.Contains(post.ContentHTML, "<script") {
		t.Fatalf("content_html must be sanitized: %s", post.ContentHTML)
	}
	if !strings.Contains(post.ContentHTML, "<strong>bold</strong>") {
		t.Fatalf("legit markdown should survive: %s", post.ContentHTML)
	}
	if post.SanitizerVersion != 1 {
		t.Fatalf("sanitizer_version: want 1, got %d", post.SanitizerVersion)
	}
	if post.ContentRaw != "hi <script>alert(1)</script> **bold**" {
		t.Fatal("raw markdown must be preserved verbatim")
	}
}

func TestReply_DerivesRootAndTarget(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	top, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "top"})
	if err != nil {
		t.Fatalf("top reply: %v", err)
	}
	if top.RootPostID != nil || top.TargetUserID != nil {
		t.Fatalf("a top-level reply carries no root/target: root=%v target=%v", top.RootPostID, top.TargetUserID)
	}

	seedTrust(t, 300, model.TrustLevelBasic, 0)
	child, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 300, BodyRaw: "child", ReplyToPostID: &top.ID})
	if err != nil {
		t.Fatalf("child reply: %v", err)
	}
	if child.RootPostID == nil || *child.RootPostID != top.ID {
		t.Fatalf("child root_post_id: want %d, got %v", top.ID, child.RootPostID)
	}
	if child.TargetUserID == nil || *child.TargetUserID != 200 {
		t.Fatalf("child target_user_id: want 200, got %v", child.TargetUserID)
	}

	seedTrust(t, 400, model.TrustLevelBasic, 0)
	grand, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 400, BodyRaw: "grand", ReplyToPostID: &child.ID})
	if err != nil {
		t.Fatalf("grandchild reply: %v", err)
	}
	if grand.RootPostID == nil || *grand.RootPostID != top.ID {
		t.Fatalf("grandchild root inherits the top ancestor: want %d, got %v", top.ID, grand.RootPostID)
	}
	if grand.TargetUserID == nil || *grand.TargetUserID != 300 {
		t.Fatalf("grandchild target: want 300, got %v", grand.TargetUserID)
	}
}

func TestReactionToggle(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, _ := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "hi"})

	rs := NewReactionService(testDB)
	res, err := rs.Toggle(context.Background(), post.ID, 300, model.ReactionKindLike)
	added, pc := res.Added, res.Post
	if err != nil || !added {
		t.Fatalf("first toggle should add: added=%v err=%v", added, err)
	}
	if res.Count != 1 {
		t.Fatalf("first toggle: want count 1, got %d", res.Count)
	}
	if pc.AuthorID != 200 || pc.ThreadID != th.ID || pc.AnchorKind != model.AnchorKindBoard || pc.AnchorID != "b1" {
		t.Fatalf("post context: author=%d thread=%d anchor=%d/%q", pc.AuthorID, pc.ThreadID, pc.AnchorKind, pc.AnchorID)
	}
	var n int64
	testDB.Model(&model.CommunityReaction{}).Where("post_id = ?", post.ID).Count(&n)
	if n != 1 {
		t.Fatalf("expected 1 reaction row, got %d", n)
	}
	res, err = rs.Toggle(context.Background(), post.ID, 300, model.ReactionKindLike)
	if err != nil || res.Added {
		t.Fatalf("second toggle should remove: added=%v err=%v", res.Added, err)
	}
	if res.Count != 0 {
		t.Fatalf("second toggle: want count 0, got %d", res.Count)
	}
	testDB.Model(&model.CommunityReaction{}).Where("post_id = ?", post.ID).Count(&n)
	if n != 0 {
		t.Fatalf("expected 0 reaction rows after un-toggle, got %d", n)
	}
}

func TestFeedbackFlow(t *testing.T) {
	cleanTables(t)
	sink := &recordingSink{}
	ts := NewThreadService(testDB, sink)
	fs := NewFeedbackService(testDB, sink)

	seedTrust(t, 100, model.TrustLevelBasic, 0)
	th, _, err := ts.OpenFeedback(context.Background(), OpenThreadParams{
		Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "r1",
		Title: "broken link", ContentRating: model.ContentRatingAll, BodyRaw: "the download 404s",
	})
	if err != nil {
		t.Fatalf("open feedback: %v", err)
	}
	if th.FbStatus == nil || *th.FbStatus != model.FeedbackStatusOpen {
		t.Fatalf("feedback born open, got %v", th.FbStatus)
	}

	resp := "fixed in v2"
	if err := fs.SetStatus(context.Background(), th.ID, model.FeedbackStatusFixed, 999, &resp); err != nil {
		t.Fatalf("set status: %v", err)
	}
	got := getThread(t, th.ID)
	if got.FbStatus == nil || *got.FbStatus != model.FeedbackStatusFixed || got.FbResponse == nil || *got.FbResponse != resp || got.FbResponderID == nil || *got.FbResponderID != 999 {
		t.Fatalf("status/response not applied: %+v", got)
	}
	if sink.count(EventFeedbackStatusChanged) != 1 {
		t.Fatalf("expected 1 feedback.status_changed event, got %d", sink.count(EventFeedbackStatusChanged))
	}

	into, _, _ := ts.OpenFeedback(context.Background(), OpenThreadParams{Site: "letmoe", AuthorID: 100, AnchorKind: model.AnchorKindSiteResource, AnchorID: "r1", Title: "dup target", ContentRating: model.ContentRatingAll, BodyRaw: "x"})
	if err := fs.Merge(context.Background(), th.ID, into.ID); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got = getThread(t, th.ID)
	if got.MergedIntoID == nil || *got.MergedIntoID != into.ID || got.Status != model.ThreadStatusClosed {
		t.Fatalf("merge not applied: merged=%v status=%d", got.MergedIntoID, got.Status)
	}
	if err := fs.Unmerge(context.Background(), th.ID); err != nil {
		t.Fatalf("unmerge: %v", err)
	}
	got = getThread(t, th.ID)
	if got.MergedIntoID != nil || got.Status != model.ThreadStatusOpen {
		t.Fatalf("unmerge should reverse: merged=%v status=%d", got.MergedIntoID, got.Status)
	}

	if err := fs.SetAnswer(context.Background(), th.ID, 42); err != nil {
		t.Fatalf("set answer: %v", err)
	}
	if got = getThread(t, th.ID); got.AnswerPostID == nil || *got.AnswerPostID != 42 {
		t.Fatalf("answer not set: %v", got.AnswerPostID)
	}

	topic := openTopic(t, ts, "letmoe", 100, "b1", "x")
	if err := fs.SetStatus(context.Background(), topic.ID, model.FeedbackStatusOpen, 1, nil); err != ErrNotFeedback {
		t.Fatalf("feedback op on a topic should return ErrNotFeedback, got %v", err)
	}
}
