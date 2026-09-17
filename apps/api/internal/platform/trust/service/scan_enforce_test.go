package service

import (
	"context"
	"strings"
	"testing"

	"api/internal/platform/trust/model"
)

func liveWorker(score float32, categories ...string) *ScanWorker {
	return NewScanWorker(testDB, &fakeGateway{
		configured: true,
		verdict: GatewayVerdict{
			Flagged: true, Score: f32(score), Categories: categories, Channel: "test-channel",
		},
	}, stubTier0{}, WithScanMode("live"))
}

func countRows(t *testing.T, dest any, where ...any) int64 {
	t.Helper()
	var n int64
	q := testDB.Model(dest)
	if len(where) > 0 {
		q = q.Where(where[0], where[1:]...)
	}
	if err := q.Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestScanShadowModeEnforcesNothing(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))
	seedPending(t, "shadow-1", "convict me")

	w := NewScanWorker(testDB, &fakeGateway{
		configured: true,
		verdict:    GatewayVerdict{Flagged: true, Score: f32(0.9), Channel: "test-channel"},
	}, stubTier0{})

	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	if n := countScanStatus(t, model.ScanStatusScored); n != 1 {
		t.Fatalf("scored rows = %d, want 1", n)
	}
	if n := countRows(t, &model.TrustReviewItem{}); n != 0 {
		t.Fatalf("shadow mode opened %d review items, want 0", n)
	}
	if n := countRows(t, &model.TrustDisposition{}); n != 0 {
		t.Fatalf("shadow mode queued %d dispositions, want 0", n)
	}
}

func TestScanLiveModeOpensItemAndQueuesHide(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))
	author := int64(4242)
	r := model.TrustScanResult{
		Site: tSite, SubjectKind: tKind, SubjectID: "live-1", AuthorID: &author,
		ContentText: "something the classifier convicts",
		Status:      model.ScanStatusPending, Mode: model.ScanModeShadow,
	}
	if err := testDB.Create(&r).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := liveWorker(0.87, "harassment").ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	var scored model.TrustScanResult
	if err := testDB.Take(&scored, r.ID).Error; err != nil {
		t.Fatalf("reload scan: %v", err)
	}
	if scored.Mode != model.ScanModeLive {
		t.Fatalf("scored row mode = %d, want live(%d)", scored.Mode, model.ScanModeLive)
	}

	var item model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "live-1").Take(&item).Error; err != nil {
		t.Fatalf("expected one review item: %v", err)
	}
	if item.Source != model.ReviewSourceAIText {
		t.Fatalf("item source = %d, want ai_text(%d)", item.Source, model.ReviewSourceAIText)
	}
	if item.Status != model.ReviewStatusPending {
		t.Fatalf("item status = %d, want pending", item.Status)
	}
	if item.ClassifierScore == nil || *item.ClassifierScore != 0.87 {
		t.Fatalf("classifier_score = %v, want 0.87", item.ClassifierScore)
	}
	if item.ContextNote == nil {
		t.Fatal("context_note is nil; the reviewer has no evidence")
	}
	for _, want := range []string{"[ai]", "harassment", "4242", "convicts"} {
		if !strings.Contains(*item.ContextNote, want) {
			t.Fatalf("context_note %q missing %q", *item.ContextNote, want)
		}
	}

	var disp model.TrustDisposition
	if err := testDB.Where("review_item_id = ?", item.ID).Take(&disp).Error; err != nil {
		t.Fatalf("expected one disposition: %v", err)
	}
	if disp.Action != model.ActionHide {
		t.Fatalf("action = %d, want hide(%d)", disp.Action, model.ActionHide)
	}
	if disp.ReasonCode != scanReasonCode {
		t.Fatalf("reason_code = %q, want %q", disp.ReasonCode, scanReasonCode)
	}
	if disp.CallbackStatus == nil || *disp.CallbackStatus != model.CallbackStatusPending {
		t.Fatalf("callback_status = %v, want pending", disp.CallbackStatus)
	}
	if disp.NextAttemptAt == nil {
		t.Fatal("next_attempt_at is nil; the callback worker would never claim it")
	}
}

func TestScanLiveModeCleanVerdictDoesNothing(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))
	seedPending(t, "clean-1", "a perfectly ordinary galgame comment")

	w := NewScanWorker(testDB, &fakeGateway{
		configured: true,
		verdict:    GatewayVerdict{Flagged: false, Score: f32(0.02), Channel: "test-channel"},
	}, stubTier0{}, WithScanMode("live"))
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	if n := countRows(t, &model.TrustReviewItem{}); n != 0 {
		t.Fatalf("clean verdict opened %d review items, want 0", n)
	}
	if n := countRows(t, &model.TrustDisposition{}); n != 0 {
		t.Fatalf("clean verdict queued %d dispositions, want 0", n)
	}
}

func TestScanLiveModeIsIdempotentPerSubject(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))

	seedPending(t, "dup-1", "first version")
	if _, err := liveWorker(0.55).ScorePending(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	seedPending(t, "dup-1", "edited version")
	if _, err := liveWorker(0.91).ScorePending(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if n := countRows(t, &model.TrustReviewItem{}, "subject_id = ?", "dup-1"); n != 1 {
		t.Fatalf("review items for subject = %d, want 1 (invariant 4)", n)
	}
	if n := countRows(t, &model.TrustDisposition{}); n != 1 {
		t.Fatalf("dispositions = %d, want 1 (no second hide on re-conviction)", n)
	}
	var item model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "dup-1").Take(&item).Error; err != nil {
		t.Fatalf("reload item: %v", err)
	}
	if item.ClassifierScore == nil || *item.ClassifierScore != 0.91 {
		t.Fatalf("classifier_score = %v, want the raised 0.91", item.ClassifierScore)
	}
	if item.ContextNote == nil || !strings.Contains(*item.ContextNote, "first version") {
		t.Fatalf("context_note = %v, want the FIRST excerpt preserved", item.ContextNote)
	}
}

func TestScanLiveModeAdoptsExistingOpenItem(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))

	existing := model.TrustReviewItem{
		Site: tSite, SubjectKind: tKind, SubjectID: "adopt-1",
		Source: model.ReviewSourceReports, Priority: 1, Status: model.ReviewStatusPending,
	}
	if err := testDB.Create(&existing).Error; err != nil {
		t.Fatalf("seed existing item: %v", err)
	}

	seedPending(t, "adopt-1", "also convicted by the classifier")
	if _, err := liveWorker(0.77).ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	if n := countRows(t, &model.TrustReviewItem{}, "subject_id = ?", "adopt-1"); n != 1 {
		t.Fatalf("review items = %d, want 1 (adopted, not forked)", n)
	}
	if n := countRows(t, &model.TrustDisposition{}); n != 0 {
		t.Fatalf("dispositions = %d, want 0 (a human owns this item)", n)
	}
	var reloaded model.TrustReviewItem
	if err := testDB.Take(&reloaded, existing.ID).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Source != model.ReviewSourceReports {
		t.Fatalf("source = %d, want the original reports source preserved", reloaded.Source)
	}
	if reloaded.ClassifierScore == nil || *reloaded.ClassifierScore != 0.77 {
		t.Fatalf("classifier_score = %v, want the AI signal merged in", reloaded.ClassifierScore)
	}
}

func TestScanLiveModeWithoutCallbackURLStaysHumanOnly(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, nil, nil)
	seedPending(t, "nocb-1", "convict me")

	if _, err := liveWorker(0.8).ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	var disp model.TrustDisposition
	if err := testDB.Take(&disp).Error; err != nil {
		t.Fatalf("expected one disposition: %v", err)
	}
	if disp.CallbackStatus != nil {
		t.Fatalf("callback_status = %v, want NULL (no endpoint to call)", disp.CallbackStatus)
	}
	if disp.NextAttemptAt != nil {
		t.Fatalf("next_attempt_at = %v, want NULL", disp.NextAttemptAt)
	}
}

func TestScanModeParsing(t *testing.T) {
	for _, tc := range []struct {
		name string
		want int16
	}{
		{"live", model.ScanModeLive},
		{"shadow", model.ScanModeShadow},
		{"", model.ScanModeShadow},
		{"LIVE", model.ScanModeShadow},
		{"liv", model.ScanModeShadow},
		{"enforce", model.ScanModeShadow},
	} {
		w := NewScanWorker(testDB, &fakeGateway{}, stubTier0{}, WithScanMode(tc.name))
		if w.mode != tc.want {
			t.Fatalf("WithScanMode(%q) → mode %d, want %d", tc.name, w.mode, tc.want)
		}
	}
}

func TestScanLiveModeCarriesReachIntoPriority(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))

	reach := int64(10_000)
	r := model.TrustScanResult{
		Site: tSite, SubjectKind: tKind, SubjectID: "reach-1",
		ContentText: "convict me", SubjectReach: &reach,
		Status: model.ScanStatusPending, Mode: model.ScanModeShadow,
	}
	if err := testDB.Create(&r).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := liveWorker(0.8).ScorePending(context.Background()); err != nil {
		t.Fatalf("score pending: %v", err)
	}

	var item model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "reach-1").Take(&item).Error; err != nil {
		t.Fatalf("reload item: %v", err)
	}
	if item.SubjectReach == nil || *item.SubjectReach != reach {
		t.Fatalf("subject_reach = %v, want %d", item.SubjectReach, reach)
	}
	unboosted := scanPriority(f32(0.8))
	if item.Priority <= unboosted {
		t.Fatalf("priority %.3f was not lifted by reach (unboosted %.3f)", item.Priority, unboosted)
	}
	if want := rankPriority(unboosted, &reach); item.Priority != want {
		t.Fatalf("priority = %.4f, want %.4f", item.Priority, want)
	}
}

func TestScanRescanRepricesOpenItemUpward(t *testing.T) {
	cleanTables(t)
	registerKind(t, tSite, tKind, strptr("http://product.invalid/cb"), strptr("s3cr3t"))

	seed := func(subject string, reach int64) {
		t.Helper()
		row := model.TrustScanResult{
			Site: tSite, SubjectKind: tKind, SubjectID: subject,
			ContentText: "convict me", SubjectReach: &reach,
			Status: model.ScanStatusPending, Mode: model.ScanModeShadow,
		}
		if err := testDB.Create(&row).Error; err != nil {
			t.Fatalf("seed %s: %v", subject, err)
		}
	}

	seed("burn-1", 20)
	if _, err := liveWorker(0.6).ScorePending(context.Background()); err != nil {
		t.Fatalf("first pass: %v", err)
	}
	var first model.TrustReviewItem
	if err := testDB.Where("subject_id = ?", "burn-1").Take(&first).Error; err != nil {
		t.Fatalf("reload after first pass: %v", err)
	}

	seed("burn-1", 250_000)
	if _, err := liveWorker(0.6).ScorePending(context.Background()); err != nil {
		t.Fatalf("second pass: %v", err)
	}

	if n := countRows(t, &model.TrustReviewItem{}, "subject_id = ?", "burn-1"); n != 1 {
		t.Fatalf("items = %d, want 1 (reprice must not fork — invariant 4)", n)
	}
	var second model.TrustReviewItem
	if err := testDB.Take(&second, first.ID).Error; err != nil {
		t.Fatalf("reload after second pass: %v", err)
	}
	if second.SubjectReach == nil || *second.SubjectReach != 250_000 {
		t.Fatalf("subject_reach = %v, want the grown 250000", second.SubjectReach)
	}
	if second.Priority <= first.Priority {
		t.Fatalf("priority did not climb with reach: %.3f → %.3f", first.Priority, second.Priority)
	}
	if n := countRows(t, &model.TrustDisposition{}); n != 1 {
		t.Fatalf("dispositions = %d, want 1", n)
	}
}
