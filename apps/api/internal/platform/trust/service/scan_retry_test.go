package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"api/internal/platform/trust/model"
)

type scriptedGateway struct {
	steps []scriptStep
	calls int
}

type scriptStep struct {
	verdict GatewayVerdict
	err     error
}

func (g *scriptedGateway) Configured() bool { return true }

func (g *scriptedGateway) Moderate(_ context.Context, _, _, _ string, _ *int64) (GatewayVerdict, error) {
	s := g.steps[len(g.steps)-1]
	if g.calls < len(g.steps) {
		s = g.steps[g.calls]
	}
	g.calls++
	return s.verdict, s.err
}

func TestScanDegradedReasonRecorded(t *testing.T) {
	cases := []struct {
		name    string
		gateway ScanGateway
		want    int16
	}{
		{"unconfigured", &fakeGateway{configured: false}, model.ScanDegradedGatewayUnconfigured},
		{"call-failed", &fakeGateway{configured: true, err: errors.New("upstream boom")}, model.ScanDegradedGatewayCallFailed},
		{"gateway-degraded", &fakeGateway{configured: true, verdict: GatewayVerdict{Degraded: true}}, model.ScanDegradedGatewayDegraded},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleanTables(t)
			id := seedPending(t, "s-reason", "text")
			w := NewScanWorker(testDB, c.gateway, stubTier0{})
			if _, err := w.ScorePending(context.Background()); err != nil {
				t.Fatalf("score: %v", err)
			}
			row := getScan(t, id)
			if row.Status != model.ScanStatusDegraded {
				t.Fatalf("status = %d, want degraded", row.Status)
			}
			if row.DegradedReason == nil || *row.DegradedReason != c.want {
				t.Fatalf("degraded_reason = %v, want %d", row.DegradedReason, c.want)
			}
			if row.ScanAttempts != 1 {
				t.Fatalf("scan_attempts = %d, want 1", row.ScanAttempts)
			}
		})
	}
}

func TestScanDegradedRetriedThenScored(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-recover", "text")

	g := &scriptedGateway{steps: []scriptStep{
		{err: errors.New("transient 503")},
		{verdict: GatewayVerdict{Flagged: true, Score: f32(0.8), Channel: "glm"}},
	}}
	w := NewScanWorker(testDB, g, stubTier0{})
	clock := time.Now()
	w.now = func() time.Time { return clock }

	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("pass 1: %v", err)
	}
	row := getScan(t, id)
	if row.Status != model.ScanStatusDegraded {
		t.Fatalf("after pass 1 status = %d, want degraded", row.Status)
	}
	if row.NextScanAt == nil || !row.NextScanAt.Equal(clock.Add(scanRetryBackoff[0]).Truncate(time.Microsecond)) {
		t.Fatalf("next_scan_at = %v, want %v", row.NextScanAt, clock.Add(scanRetryBackoff[0]))
	}

	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("early pass: %v", err)
	}
	if g.calls != 1 {
		t.Fatalf("gateway dialed %d times before the backoff was due, want 1", g.calls)
	}

	clock = clock.Add(scanRetryBackoff[0])
	if _, err := w.ScorePending(context.Background()); err != nil {
		t.Fatalf("pass 2: %v", err)
	}
	row = getScan(t, id)
	if row.Status != model.ScanStatusScored {
		t.Fatalf("after pass 2 status = %d, want scored — the retry never happened", row.Status)
	}
	if row.DegradedReason != nil || row.NextScanAt != nil {
		t.Fatalf("a scored row must not keep a degradation reason or retry time, got %v / %v",
			row.DegradedReason, row.NextScanAt)
	}
	if row.ScanAttempts != 2 {
		t.Fatalf("scan_attempts = %d, want 2 (the row took two tries and must say so)", row.ScanAttempts)
	}
	if row.Flagged == nil || !*row.Flagged {
		t.Fatal("the recovered verdict must be recorded")
	}
}

func TestScanDegradedRetryIsBounded(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-exhaust", "text")

	g := &fakeGateway{configured: true, err: errors.New("permanently down")}
	w := NewScanWorker(testDB, g, stubTier0{})
	clock := time.Now()
	w.now = func() time.Time { return clock }

	for pass := range maxScanAttempts + 3 {
		if _, err := w.ScorePending(context.Background()); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		clock = clock.Add(scanRetryBackoff[len(scanRetryBackoff)-1])
	}
	row := getScan(t, id)
	if row.Status != model.ScanStatusDegraded {
		t.Fatalf("status = %d, want degraded", row.Status)
	}
	if row.ScanAttempts != maxScanAttempts {
		t.Fatalf("scan_attempts = %d, want it capped at %d", row.ScanAttempts, maxScanAttempts)
	}
	if row.NextScanAt != nil {
		t.Fatalf("an exhausted row must not be scheduled again, next_scan_at = %v", row.NextScanAt)
	}
	if g.calls.Load() != int32(maxScanAttempts) {
		t.Fatalf("gateway dialed %d times, want exactly %d — the bound is not holding",
			g.calls.Load(), maxScanAttempts)
	}
}

func TestScanUnconfiguredIsNotRetried(t *testing.T) {
	cleanTables(t)
	id := seedPending(t, "s-unconfigured", "text")
	w := NewScanWorker(testDB, &fakeGateway{configured: false}, stubTier0{})
	clock := time.Now()
	w.now = func() time.Time { return clock }

	for range 3 {
		if _, err := w.ScorePending(context.Background()); err != nil {
			t.Fatalf("score: %v", err)
		}
		clock = clock.Add(scanRetryBackoff[len(scanRetryBackoff)-1])
	}
	row := getScan(t, id)
	if row.ScanAttempts != 1 || row.NextScanAt != nil {
		t.Fatalf("scan_attempts = %d, next_scan_at = %v; want 1 and nil", row.ScanAttempts, row.NextScanAt)
	}
}

func TestScanOutageStopsTheBatch(t *testing.T) {
	cases := []struct {
		name string
		g    *fakeGateway
	}{
		{"call-failed", &fakeGateway{configured: true, err: errors.New("429 capacity")}},
		{"gateway-degraded", &fakeGateway{configured: true, verdict: GatewayVerdict{Degraded: true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cleanTables(t)
			for i := range 5 {
				seedPending(t, subjID(i), "text")
			}
			n, err := NewScanWorker(testDB, c.g, stubTier0{}).ScorePending(context.Background())
			if err != nil {
				t.Fatalf("score: %v", err)
			}
			if n != 1 || c.g.calls.Load() != 1 {
				t.Fatalf("processed %d, dialed %d; want 1 and 1", n, c.g.calls.Load())
			}
			if got := countScanStatus(t, model.ScanStatusPending); got != 4 {
				t.Fatalf("pending = %d, want 4 left untouched by the outage", got)
			}
		})
	}
}

func TestScanPendingNotStarvedByRetries(t *testing.T) {
	cleanTables(t)

	past := time.Now().Add(-time.Hour)
	for i := range scanBatchSize {
		reason := model.ScanDegradedGatewayDegraded
		r := model.TrustScanResult{
			Site: tSite, SubjectKind: tKind, SubjectID: "s-old-" + subjID(i), ContentText: "text",
			Status: model.ScanStatusDegraded, Mode: model.ScanModeShadow,
			DegradedReason: &reason, ScanAttempts: 1, NextScanAt: &past,
		}
		if err := testDB.Create(&r).Error; err != nil {
			t.Fatalf("seed due retry: %v", err)
		}
	}

	fresh := seedPending(t, "s-fresh", "text")
	good := &fakeGateway{configured: true, verdict: GatewayVerdict{Flagged: false, Score: f32(0.1), Channel: "omni"}}
	if _, err := NewScanWorker(testDB, good, stubTier0{}).ScorePending(context.Background()); err != nil {
		t.Fatalf("mixed pass: %v", err)
	}
	if row := getScan(t, fresh); row.Status != model.ScanStatusScored {
		t.Fatalf("fresh intake status = %d, want scored — retries starved the live queue", row.Status)
	}
}
