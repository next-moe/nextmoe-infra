package service

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/pkg/trustclient"
)

type fakeForwarder struct {
	mu          sync.Mutex
	forwards    []trustclient.ForwardRequest
	resolves    []fakeResolve
	nextID      int64
	failForward bool
	calls       chan struct{}
}

type fakeResolve struct {
	TrustID  int64
	Outcome  string
	ActorRef string
}

func newFakeForwarder() *fakeForwarder { return &fakeForwarder{calls: make(chan struct{}, 16)} }

func (f *fakeForwarder) Forward(_ context.Context, req trustclient.ForwardRequest) (int64, bool, error) {
	f.mu.Lock()
	f.forwards = append(f.forwards, req)
	fail := f.failForward
	f.nextID++
	id := f.nextID
	f.mu.Unlock()
	defer f.signal()
	if fail {
		return 0, false, errors.New("forward boom")
	}
	return id, true, nil
}

func (f *fakeForwarder) Resolve(_ context.Context, trustID int64, outcome, actorRef string) (bool, error) {
	f.mu.Lock()
	f.resolves = append(f.resolves, fakeResolve{trustID, outcome, actorRef})
	f.mu.Unlock()
	f.signal()
	return true, nil
}

func (f *fakeForwarder) signal() {
	select {
	case f.calls <- struct{}{}:
	default:
	}
}

func (f *fakeForwarder) forwardCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forwards)
}

func waitCall(t *testing.T, f *fakeForwarder) {
	t.Helper()
	select {
	case <-f.calls:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a forwarder call")
	}
}

func heldReply(t *testing.T, ps *PostService, threadID, author int64, body string) (*model.CommunityPost, *model.CommunityReviewItem) {
	t.Helper()
	seedTrust(t, author, model.TrustLevelNew, 2)
	p, err := ps.Reply(context.Background(), ReplyParams{ThreadID: threadID, AuthorID: author, BodyRaw: body})
	if err != nil {
		t.Fatalf("held reply: %v", err)
	}
	if p.Status != model.PostStatusHidden {
		t.Fatalf("expected held post, got status %d", p.Status)
	}
	return p, pendingReviewForPost(t, p.ID)
}

func TestForwardSweep(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	_, it1 := heldReply(t, ps, th.ID, 700, "hello one")
	p2, it2 := heldReply(t, ps, th.ID, 701, "hello two")

	if n, err := NewForwardService(testDB, nil).Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("disabled sweep: n=%d err=%v", n, err)
	}

	failing := newFakeForwarder()
	failing.failForward = true
	fsvc := NewForwardService(testDB, failing)
	if n, err := fsvc.Sweep(ctx); err != nil || n != 0 {
		t.Fatalf("failing sweep forwarded %d (err %v), want 0", n, err)
	}
	got := reloadItem(t, it1.ID)
	if got.TrustReviewItemID != nil {
		t.Fatal("failed forward must not back-fill trust id")
	}
	if got.ForwardAttempts != 1 {
		t.Fatalf("forward_attempts = %d, want 1 after one failed sweep", got.ForwardAttempts)
	}
	if got.ForwardAfter == nil || !got.ForwardAfter.After(time.Now()) {
		t.Fatalf("forward_after = %v, want a time ahead of now after a failed sweep", got.ForwardAfter)
	}

	fake := newFakeForwarder()
	svc := NewForwardService(testDB, fake)
	if n, err := svc.Sweep(ctx); err != nil || n != 0 || fake.forwardCount() != 0 {
		t.Fatalf("sweep inside the backoff: forwarded %d with %d calls (err %v), want none", n, fake.forwardCount(), err)
	}
	makeForwardDue(t, it1.ID, it2.ID)
	n, err := svc.Sweep(ctx)
	if err != nil || n != 2 {
		t.Fatalf("sweep forwarded %d (err %v), want 2", n, err)
	}
	for _, id := range []int64{it1.ID, it2.ID} {
		if reloadItem(t, id).TrustReviewItemID == nil {
			t.Fatalf("item %d not back-filled after sweep", id)
		}
	}
	found := false
	for _, req := range fake.forwards {
		if req.SubjectKind != forwardSubjectKind {
			t.Fatalf("subject_kind = %q, want %q", req.SubjectKind, forwardSubjectKind)
		}
		if req.SubjectID == strconv.FormatInt(p2.ID, 10) {
			found = true
			if req.ContextNote == nil || !strings.HasPrefix(*req.ContextNote, "[first_post_hold] post #"+strconv.FormatInt(p2.ID, 10)) {
				t.Fatalf("context note = %v", req.ContextNote)
			}
			if req.AuthorID == nil || *req.AuthorID != 701 {
				t.Fatalf("author_id = %v, want the post's author 701", req.AuthorID)
			}
		}
	}
	if !found {
		t.Fatalf("no forward for post %d", p2.ID)
	}
}

func TestForwardImmediateAfterCommit(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeForwarder()
	fwd := NewForwardService(testDB, fake)
	sink := NewForwardingSink(NoopSink{}, fwd)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	fs := NewFlagService(testDB, sink)
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	post := visibleReply(t, ps, th.ID, 200)

	for _, r := range []int64{300, 301, 302} {
		if err := fs.Submit(ctx, post.ID, r, nil, nil); err != nil {
			t.Fatalf("flag: %v", err)
		}
	}
	waitCall(t, fake)
	if fake.forwardCount() != 1 {
		t.Fatalf("expected 1 immediate forward, got %d", fake.forwardCount())
	}
	req := fake.forwards[0]
	if req.SubjectID != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("forward subject_id = %q, want %d", req.SubjectID, post.ID)
	}
	if req.ContextNote == nil || !strings.HasPrefix(*req.ContextNote, "[flags] post #") {
		t.Fatalf("forward context note = %v", req.ContextNote)
	}
}

func TestForwardResolveOnDecide(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeForwarder()
	fwd := NewForwardService(testDB, fake)
	sink := NewForwardingSink(NoopSink{}, fwd)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	rs := NewReviewService(testDB, sink)
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	post, item := heldReply(t, ps, th.ID, 700, "held post")
	if err := testDB.Model(&model.CommunityReviewItem{}).Where("id = ?", item.ID).
		Update("trust_review_item_id", int64(555)).Error; err != nil {
		t.Fatalf("mark forwarded: %v", err)
	}
	if err := rs.Approve(ctx, item.ID, 999); err != nil {
		t.Fatalf("approve: %v", err)
	}
	waitCall(t, fake)
	fake.mu.Lock()
	resolves := append([]fakeResolve(nil), fake.resolves...)
	fake.mu.Unlock()
	if len(resolves) != 1 || resolves[0].TrustID != 555 || resolves[0].Outcome != outcomeApproved || resolves[0].ActorRef != "999" {
		t.Fatalf("resolve calls = %+v, want one {555 approved 999}", resolves)
	}
	if getPost(t, post.ID).Status != model.PostStatusVisible {
		t.Fatal("approve should still restore the post locally")
	}

	_, item2 := heldReply(t, ps, th.ID, 701, "another held")
	if err := fwd.resolveItem(ctx, item2.ID, outcomeRejected, 1); err != nil {
		t.Fatalf("resolve un-forwarded: %v", err)
	}
	fake.mu.Lock()
	extra := len(fake.resolves)
	fake.mu.Unlock()
	if extra != 1 {
		t.Fatalf("un-forwarded item must not trigger a resolve; total resolves = %d", extra)
	}
}

func TestForwardSweep_BackoffDoublesToAnHour(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	_, item := heldReply(t, ps, th.ID, 700, "held")

	failing := newFakeForwarder()
	failing.failForward = true
	svc := NewForwardService(testDB, failing)
	for _, c := range []struct {
		attempts int32
		want     time.Duration
	}{
		{0, time.Minute}, {1, 2 * time.Minute}, {5, 32 * time.Minute}, {6, time.Hour}, {113144, time.Hour},
	} {
		if err := testDB.Model(&model.CommunityReviewItem{}).Where("id = ?", item.ID).
			Updates(map[string]any{"forward_attempts": c.attempts, "forward_after": nil}).Error; err != nil {
			t.Fatalf("seed attempts: %v", err)
		}
		before := time.Now()
		if _, err := svc.Sweep(ctx); err != nil {
			t.Fatalf("sweep: %v", err)
		}
		got := reloadItem(t, item.ID)
		if got.ForwardAttempts != c.attempts+1 {
			t.Fatalf("after %d attempts: forward_attempts = %d, want %d", c.attempts, got.ForwardAttempts, c.attempts+1)
		}
		if got.ForwardAfter == nil {
			t.Fatalf("after %d attempts: forward_after is NULL", c.attempts)
		}
		if wait := got.ForwardAfter.Sub(before); wait < c.want-time.Second || wait > c.want+10*time.Second {
			t.Errorf("after %d attempts: next try in %s, want %s", c.attempts, wait.Round(time.Second), c.want)
		}
	}
}

func TestForwardSweep_SkipsAnItemDecidedLocally(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	rs := NewReviewService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	_, decided := heldReply(t, ps, th.ID, 700, "decided before it was forwarded")
	_, pending := heldReply(t, ps, th.ID, 701, "still pending")
	if err := rs.Approve(ctx, decided.ID, 999); err != nil {
		t.Fatalf("approve: %v", err)
	}

	fake := newFakeForwarder()
	n, err := NewForwardService(testDB, fake).Sweep(ctx)
	if err != nil || n != 1 {
		t.Fatalf("sweep forwarded %d (err %v), want only the pending item", n, err)
	}
	if reloadItem(t, decided.ID).TrustReviewItemID != nil {
		t.Fatal("an item decided locally was forwarded: trust would hold a pending item nobody resolves")
	}
	if reloadItem(t, pending.ID).TrustReviewItemID == nil {
		t.Fatal("the pending item was not forwarded")
	}
}

func makeForwardDue(t *testing.T, ids ...int64) {
	t.Helper()
	if err := testDB.Model(&model.CommunityReviewItem{}).Where("id IN ?", ids).
		Update("forward_after", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatalf("make forward due: %v", err)
	}
}

func reloadItem(t *testing.T, id int64) *model.CommunityReviewItem {
	t.Helper()
	var it model.CommunityReviewItem
	if err := testDB.First(&it, id).Error; err != nil {
		t.Fatalf("reload item %d: %v", id, err)
	}
	return &it
}
