package service

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"
	"api/pkg/trustclient"
)

type fakeScanner struct {
	mu    sync.Mutex
	calls []trustclient.ScanRequest
	fail  bool
	ch    chan struct{}
}

func newFakeScanner() *fakeScanner { return &fakeScanner{ch: make(chan struct{}, 16)} }

func (f *fakeScanner) Scan(_ context.Context, req trustclient.ScanRequest) (int64, bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, req)
	fail := f.fail
	id := int64(len(f.calls))
	f.mu.Unlock()
	defer f.signal()
	if fail {
		return 0, false, errors.New("scan boom")
	}
	return id, false, nil
}

func (f *fakeScanner) signal() {
	select {
	case f.ch <- struct{}{}:
	default:
	}
}

func (f *fakeScanner) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeScanner) last() trustclient.ScanRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func waitScan(t *testing.T, f *fakeScanner) {
	t.Helper()
	select {
	case <-f.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a scanner call")
	}
}

func TestScanDisabledZeroDial(t *testing.T) {
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeScanner()
	scanSvc := NewScanService(testDB, nil)
	if scanSvc.Enabled() {
		t.Fatal("nil scanner must report disabled")
	}
	sink := NewScanningSink(NoopSink{}, scanSvc)
	ts := NewThreadService(testDB, sink)
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "reply body"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: 200, BodyRaw: "edited body"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if fake.count() != 0 {
		t.Fatalf("disabled scanning must not dial, got %d calls", fake.count())
	}
}

func TestScanReplyPayload(t *testing.T) {
	settings.Override(t, keys.TrustScanEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeScanner()
	scanSvc := NewScanService(testDB, fake)
	sink := NewScanningSink(NoopSink{}, scanSvc)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "reply raw body"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitScan(t, fake)
	if fake.count() != 1 {
		t.Fatalf("expected exactly one scan, got %d", fake.count())
	}
	req := fake.last()
	if req.SubjectKind != scanSubjectKind {
		t.Fatalf("subject_kind = %q, want %q", req.SubjectKind, scanSubjectKind)
	}
	if req.SubjectID != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("subject_id = %q, want post id %d", req.SubjectID, post.ID)
	}
	if req.AuthorID == nil || *req.AuthorID != 200 {
		t.Fatalf("author_id = %v, want 200", req.AuthorID)
	}
	if req.Text != "reply raw body" {
		t.Fatalf("text = %q, want the raw body (no title prefix for a reply)", req.Text)
	}
	if req.Site != "letmoe" {
		t.Fatalf("site = %q, want letmoe", req.Site)
	}
}

func TestScanFirstPostTitlePrefix(t *testing.T) {
	settings.Override(t, keys.TrustScanEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeScanner()
	scanSvc := NewScanService(testDB, fake)
	sink := NewScanningSink(NoopSink{}, scanSvc)
	ts := NewThreadService(testDB, sink)

	seedTrust(t, 100, model.TrustLevelBasic, 0)
	_, post, err := ts.OpenTopic(ctx, OpenTopicParams{
		Site: "letmoe", AuthorID: 100, BoardID: testBoard(t, "letmoe", "b1"),
		Title: "My Title", ContentRating: model.ContentRatingAll, BodyRaw: "opening body",
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	waitScan(t, fake)
	req := fake.last()
	if want := "My Title\n\nopening body"; req.Text != want {
		t.Fatalf("text = %q, want %q", req.Text, want)
	}
	if req.SubjectID != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("subject_id = %q, want post id %d", req.SubjectID, post.ID)
	}
}

func TestScanOnEdit(t *testing.T) {
	settings.Override(t, keys.TrustScanEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeScanner()
	scanSvc := NewScanService(testDB, fake)
	sink := NewScanningSink(NoopSink{}, scanSvc)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "original"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	waitScan(t, fake)

	if _, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: 200, BodyRaw: "edited body"}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	waitScan(t, fake)
	if fake.count() != 2 {
		t.Fatalf("expected create+edit scans (2), got %d", fake.count())
	}
	req := fake.last()
	if req.Text != "edited body" {
		t.Fatalf("edit text = %q, want the new raw body", req.Text)
	}
	if req.SubjectID != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("edit subject_id = %q, want post id %d", req.SubjectID, post.ID)
	}
}

func TestScanErrorNeverBreaksWrite(t *testing.T) {
	settings.Override(t, keys.TrustScanEnabled, true)
	cleanTables(t)
	ctx := context.Background()
	fake := newFakeScanner()
	fake.fail = true
	scanSvc := NewScanService(testDB, fake)
	sink := NewScanningSink(NoopSink{}, scanSvc)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	seedTrust(t, 200, model.TrustLevelBasic, 0)
	post, err := ps.Reply(ctx, ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "reply"})
	if err != nil {
		t.Fatalf("reply must succeed despite scan error: %v", err)
	}
	waitScan(t, fake)

	edited, err := ps.Edit(ctx, EditParams{PostID: post.ID, AuthorID: 200, BodyRaw: "edited"})
	if err != nil {
		t.Fatalf("edit must succeed despite scan error: %v", err)
	}
	waitScan(t, fake)

	if edited.ContentRaw != "edited" {
		t.Fatalf("returned post raw = %q, want edited", edited.ContentRaw)
	}
	if got := getPost(t, post.ID); got.ContentRaw != "edited" || got.EditedAt == nil {
		t.Fatalf("edit must persist despite scan error: raw=%q edited_at=%v", got.ContentRaw, got.EditedAt)
	}
}

func TestScanForwardSinkDisjoint(t *testing.T) {
	settings.Override(t, keys.TrustScanEnabled, true)
	cleanTables(t)
	fakeScan := newFakeScanner()
	fakeFwd := newFakeForwarder()
	scanSvc := NewScanService(testDB, fakeScan)
	fwdSvc := NewForwardService(testDB, fakeFwd)
	sink := NewScanningSink(NewForwardingSink(NoopSink{}, fwdSvc), scanSvc)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, sink)

	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")
	post, _ := heldReply(t, ps, th.ID, 700, "held reply")

	waitScan(t, fakeScan)
	waitCall(t, fakeFwd)
	if fakeScan.count() != 1 {
		t.Fatalf("post.created → exactly one scan, got %d", fakeScan.count())
	}
	if fakeFwd.forwardCount() != 1 {
		t.Fatalf("review.enqueued → exactly one forward, got %d", fakeFwd.forwardCount())
	}
	if fakeScan.last().SubjectID != strconv.FormatInt(post.ID, 10) {
		t.Fatalf("scan carried subject_id %q, want the post id %d", fakeScan.last().SubjectID, post.ID)
	}
}
