package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"api/internal/platform/trust/model"
)

type fakeGateway struct {
	configured bool
	verdict    GatewayVerdict
	err        error
	calls      atomic.Int32
	lastKind   string
	lastSite   string
}

func (g *fakeGateway) Configured() bool { return g.configured }
func (g *fakeGateway) Moderate(_ context.Context, site, _, kind string, _ *int64) (GatewayVerdict, error) {
	g.calls.Add(1)
	g.lastSite = site
	g.lastKind = kind
	return g.verdict, g.err
}

type stubTier0 struct{}

func (stubTier0) Tier0Matches(_ context.Context, _, _ string) (string, []string, error) {
	return DecisionAllow, []string{}, nil
}

type fixedTier0 struct {
	decision string
	matched  []string
}

func (f fixedTier0) Tier0Matches(_ context.Context, _, _ string) (string, []string, error) {
	return f.decision, f.matched, nil
}

func seedPending(t *testing.T, subject, text string) int64 {
	t.Helper()
	r := model.TrustScanResult{
		Site: tSite, SubjectKind: tKind, SubjectID: subject, ContentText: text,
		Status: model.ScanStatusPending, Mode: model.ScanModeShadow,
	}
	if err := testDB.Create(&r).Error; err != nil {
		t.Fatalf("seed pending scan %s: %v", subject, err)
	}
	return r.ID
}

func countScanStatus(t *testing.T, status int16) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.TrustScanResult{}).Where("status = ?", status).Count(&n).Error; err != nil {
		t.Fatalf("count scan status %d: %v", status, err)
	}
	return n
}

func f32(v float32) *float32 { return &v }

func TestScanWorkerScoredAndShadow(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-scored", "abusive text")

	g := &fakeGateway{configured: true, verdict: GatewayVerdict{
		Flagged: true, Score: f32(0.92), Categories: []string{"harassment", "abuse"}, Channel: "qwen3guard",
	}}
	w := NewScanWorker(testDB, g, stubTier0{})

	var reviewBefore int64
	testDB.Model(&model.TrustReviewItem{}).Count(&reviewBefore)

	n, err := w.ScorePending(context.Background())
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if n != 1 {
		t.Fatalf("processed = %d, want 1", n)
	}
	if g.calls.Load() != 1 {
		t.Fatalf("gateway calls = %d, want 1", g.calls.Load())
	}
	if g.lastSite != tSite {
		t.Fatalf("gateway site = %q, want %q", g.lastSite, tSite)
	}
	if g.lastKind != tKind {
		t.Fatalf("gateway subjectKind = %q, want %q", g.lastKind, tKind)
	}

	row := getScan(t, id)
	if row.Status != model.ScanStatusScored {
		t.Fatalf("status = %d, want scored(%d)", row.Status, model.ScanStatusScored)
	}
	if row.Flagged == nil || !*row.Flagged {
		t.Fatalf("flagged = %v, want true", row.Flagged)
	}
	if row.Score == nil || *row.Score != 0.92 {
		t.Fatalf("score = %v, want 0.92", row.Score)
	}
	if row.Channel != "qwen3guard" {
		t.Fatalf("channel = %q, want qwen3guard", row.Channel)
	}
	if row.ScoredAt == nil {
		t.Fatal("scored_at must be set on the scored path")
	}
	var cats []string
	if err := json.Unmarshal(row.Categories, &cats); err != nil {
		t.Fatalf("categories not valid JSON array: %v (%s)", err, row.Categories)
	}
	if len(cats) != 2 || cats[0] != "harassment" || cats[1] != "abuse" {
		t.Fatalf("categories = %v, want [harassment abuse]", cats)
	}

	var reviewAfter int64
	testDB.Model(&model.TrustReviewItem{}).Count(&reviewAfter)
	if reviewAfter != reviewBefore {
		t.Fatalf("shadow mode must create ZERO review items: before=%d after=%d", reviewBefore, reviewAfter)
	}
}

func TestScanWorkerScoredNotFlagged(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-clean", "hello")
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "qwen3guard"}}
	w := NewScanWorker(testDB, g, stubTier0{})
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}
	row := getScan(t, id)
	if row.Status != model.ScanStatusScored {
		t.Fatalf("status = %d, want scored", row.Status)
	}
	if row.Flagged == nil || *row.Flagged {
		t.Fatalf("flagged = %v, want an explicit false", row.Flagged)
	}
}

func TestScanWorkerGatewayDegraded(t *testing.T) {
	cases := []struct {
		name string
		g    *fakeGateway
	}{
		{"degraded-verdict", &fakeGateway{configured: true, verdict: GatewayVerdict{Degraded: true}}},
		{"call-error", &fakeGateway{configured: true, err: errors.New("upstream boom")}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleanTables(t)
			id := seedPending(t, "s-deg", "text")
			w := NewScanWorker(testDB, c.g, stubTier0{})
			if _, err := w.ScorePending(context.Background()); err != nil {
				t.Fatalf("score: %v", err)
			}
			if c.g.calls.Load() != 1 {
				t.Fatalf("a configured gateway must be dialed once, got %d", c.g.calls.Load())
			}
			row := getScan(t, id)
			if row.Status != model.ScanStatusDegraded {
				t.Fatalf("status = %d, want degraded(%d)", row.Status, model.ScanStatusDegraded)
			}
			if row.Flagged != nil || row.Score != nil || row.ScoredAt != nil || len(row.Categories) != 0 || row.Channel != "" {
				t.Fatalf("degraded row must carry NO verdict: flagged=%v score=%v scored_at=%v channel=%q categories=%s",
					row.Flagged, row.Score, row.ScoredAt, row.Channel, row.Categories)
			}
		})
	}
}

func TestScanWorkerEnvEmpty(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-env", "text")
	g := &fakeGateway{configured: false}
	w := NewScanWorker(testDB, g, stubTier0{})
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}
	if g.calls.Load() != 0 {
		t.Fatalf("an unconfigured gateway must NOT be dialed, got %d calls", g.calls.Load())
	}
	if getScan(t, id).Status != model.ScanStatusDegraded {
		t.Fatal("env-empty must drain the row to degraded")
	}
}

func TestScanWorkerDrainsNoBacklog(t *testing.T) {
	cleanTables(t)
	const total = scanBatchSize
	for i := range total {
		seedPending(t, subjID(i), "text")
	}
	g := &fakeGateway{configured: false}
	w := NewScanWorker(testDB, g, stubTier0{})
	n, err := w.ScorePending(context.Background())
	if err != nil {
		t.Fatalf("score: %v", err)
	}
	if n != total {
		t.Fatalf("processed = %d, want %d", n, total)
	}
	if pending := countScanStatus(t, model.ScanStatusPending); pending != 0 {
		t.Fatalf("queue did not drain: %d rows still pending", pending)
	}
	if degraded := countScanStatus(t, model.ScanStatusDegraded); degraded != total {
		t.Fatalf("degraded = %d, want %d", degraded, total)
	}
}

type blockingGateway struct {
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (g *blockingGateway) Configured() bool { return true }
func (g *blockingGateway) Moderate(_ context.Context, _, _, _ string, _ *int64) (GatewayVerdict, error) {
	g.calls.Add(1)
	g.entered <- struct{}{}
	<-g.release
	return GatewayVerdict{Flagged: false, Channel: "test"}, nil
}

func TestScanWorkerSkipLockedNoDoubleScore(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-lock", "text")

	blocking := &blockingGateway{entered: make(chan struct{}, 1), release: make(chan struct{})}
	wA := NewScanWorker(testDB, blocking, stubTier0{})

	doneA := make(chan int, 1)
	errA := make(chan error, 1)
	go func() {
		n, err := wA.ScorePending(context.Background())
		errA <- err
		doneA <- n
	}()

	<-blocking.entered

	gB := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "b"}}
	wB := NewScanWorker(testDB, gB, stubTier0{})
	nB, err := wB.ScorePending(context.Background())
	if err != nil {
		t.Fatalf("worker B score: %v", err)
	}
	if nB != 0 {
		t.Fatalf("worker B must skip the row A locked, processed %d", nB)
	}
	if gB.calls.Load() != 0 {
		t.Fatalf("worker B must not have scored a locked row, %d calls", gB.calls.Load())
	}

	close(blocking.release)
	if err := <-errA; err != nil {
		t.Fatalf("worker A score: %v", err)
	}
	if nA := <-doneA; nA != 1 {
		t.Fatalf("worker A processed = %d, want 1", nA)
	}
	if blocking.calls.Load() != 1 {
		t.Fatalf("the row was dialed %d times, want exactly 1 (no double-score)", blocking.calls.Load())
	}
	if getScan(t, id).Status != model.ScanStatusScored {
		t.Fatal("the row must end scored")
	}
}

func subjID(i int) string {
	return "drain-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
}

func seedTerm(t *testing.T, svc *TermService, site *string, raw string, kind int16) {
	t.Helper()
	if _, err := svc.Create(context.Background(), CreateTermParams{Site: site, Term: raw, Kind: kind}); err != nil {
		t.Fatalf("seed term %q: %v", raw, err)
	}
}

func tier0Of(t *testing.T, id int64) (raw []byte, matched []string) {
	t.Helper()
	row := getScan(t, id)
	if len(row.Tier0Matched) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(row.Tier0Matched, &matched); err != nil {
		t.Fatalf("tier0_matched not a JSON array: %v (%s)", err, row.Tier0Matched)
	}
	return row.Tier0Matched, matched
}

func TestScanWorkerTier0RecordedOnScored(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	seedTerm(t, svc, nil, "坏词", model.TermKindBanned)

	id := seedPending(t, "s-t0-hit", "这是一段含有坏词的文本")
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "qwen3guard"}}
	w := NewScanWorker(testDB, g, svc)

	var reviewBefore int64
	testDB.Model(&model.TrustReviewItem{}).Count(&reviewBefore)

	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}

	row := getScan(t, id)
	if row.Status != model.ScanStatusScored {
		t.Fatalf("status = %d, want scored — tier0 must not change status", row.Status)
	}
	_, matched := tier0Of(t, id)
	if len(matched) != 1 || matched[0] != "坏词" {
		t.Fatalf("tier0_matched = %v, want [坏词]", matched)
	}

	var reviewAfter int64
	testDB.Model(&model.TrustReviewItem{}).Count(&reviewAfter)
	if reviewAfter != reviewBefore {
		t.Fatalf("tier0 recording must enqueue nothing: before=%d after=%d", reviewBefore, reviewAfter)
	}
}

func TestScanWorkerTier0EmptyArrayOnNoMatch(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	seedTerm(t, svc, nil, "坏词", model.TermKindBanned)

	id := seedPending(t, "s-t0-clean", "totally fine text")
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "qwen3guard"}}
	w := NewScanWorker(testDB, g, svc)
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}
	raw, matched := tier0Of(t, id)
	if string(raw) != "[]" {
		t.Fatalf("tier0_matched raw = %q, want []", string(raw))
	}
	if len(matched) != 0 {
		t.Fatalf("tier0_matched = %v, want empty", matched)
	}
}

func TestScanWorkerTier0RecordedOnEnvEmptyDegraded(t *testing.T) {
	cleanTables(t)
	svc := NewTermService(testDB, nil)
	seedTerm(t, svc, nil, "坏词", model.TermKindSuspect)

	id := seedPending(t, "s-t0-deg", "含坏词的降级文本")
	g := &fakeGateway{configured: false}
	w := NewScanWorker(testDB, g, svc)
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}
	if g.calls.Load() != 0 {
		t.Fatalf("unconfigured gateway must not be dialed, got %d", g.calls.Load())
	}
	row := getScan(t, id)
	if row.Status != model.ScanStatusDegraded {
		t.Fatalf("status = %d, want degraded", row.Status)
	}
	_, matched := tier0Of(t, id)
	if len(matched) != 1 || matched[0] != "坏词" {
		t.Fatalf("env-empty degraded row tier0_matched = %v, want [坏词]", matched)
	}
}

func seedSkippedCheck(t *testing.T, subject, text string) int64 {
	t.Helper()
	r := model.TrustScanResult{
		Site: tSite, SubjectKind: tKind, SubjectID: subject, ContentText: text,
		Status: model.ScanStatusPending, Mode: model.ScanModeShadow, CheckSkipped: true,
	}
	if err := testDB.Create(&r).Error; err != nil {
		t.Fatalf("seed skipped-check scan %s: %v", subject, err)
	}
	return r.ID
}

func TestScanWorkerSkippedCheckHidesTier0Hit(t *testing.T) {
	cleanTables(t)
	url, sec := "https://site.example/cb", "s3cret"
	registerKind(t, tSite, tKind, &url, &sec)
	svc := NewTermService(testDB, nil)
	seedTerm(t, svc, nil, "坏词", model.TermKindBanned)

	id := seedSkippedCheck(t, "s-skip-deny", "这是一段含有坏词的文本")
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Degraded: true}}
	w := NewScanWorker(testDB, g, svc)
	for range 2 {
		if _, err := w.ScorePending(context.Background()); err != nil {
			t.Fatalf("score: %v", err)
		}
	}

	var items []model.TrustReviewItem
	testDB.Where("subject_id = ?", "s-skip-deny").Find(&items)
	if len(items) != 1 || items[0].Source != model.ReviewSourceSkippedCheck {
		t.Fatalf("items = %+v, want one skipped_check item", items)
	}
	var disps []model.TrustDisposition
	testDB.Where("review_item_id = ?", items[0].ID).Find(&disps)
	if len(disps) != 1 || disps[0].Action != model.ActionHide || disps[0].CallbackStatus == nil ||
		*disps[0].CallbackStatus != model.CallbackStatusPending {
		t.Fatalf("dispositions = %+v, want one hide with a pending callback", disps)
	}
	if getScan(t, id).CheckSkipped {
		t.Fatalf("check_skipped still set after enforcement")
	}
}

func TestScanWorkerSkippedCheckHoldWithoutCallback(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "qwen3guard"}}
	w := NewScanWorker(testDB, g, fixedTier0{decision: DecisionHold, matched: []string{"可疑"}})
	seedSkippedCheck(t, "s-skip-hold", "可疑的文本")
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}

	var item model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "s-skip-hold").Take(&item).Error; err != nil {
		t.Fatalf("no review item: %v", err)
	}
	var disp model.TrustDisposition
	if err := testDB.Where("review_item_id = ?", item.ID).Take(&disp).Error; err != nil {
		t.Fatalf("no disposition: %v", err)
	}
	if disp.Action != model.ActionHide || disp.CallbackStatus != nil {
		t.Fatalf("disposition = %+v, want a hide with no callback", disp)
	}
}

func TestScanWorkerCheckedContentIsNotQueuedAgain(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	g := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Channel: "qwen3guard"}}
	w := NewScanWorker(testDB, g, fixedTier0{decision: DecisionDeny, matched: []string{"坏词"}})
	seedPending(t, "s-checked", "坏词")
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score: %v", err)
	}
	if n := countOpenItems(t, tSite, tKind, "s-checked"); n != 0 {
		t.Fatalf("open items = %d; content the synchronous check already saw must not be queued again", n)
	}
}
