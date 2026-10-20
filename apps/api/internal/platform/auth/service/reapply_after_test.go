package service

import (
	"testing"
	"time"
)

func TestReapplyAfterIsTheEndOfACooldownStillRunning(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	reviewed := now.Add(-2 * time.Hour)

	got := reapplyAfter(&reviewed, 24*time.Hour, now)
	if got == nil || !got.Equal(reviewed.Add(24*time.Hour)) {
		t.Fatalf("reapply_after = %v, want %v", got, reviewed.Add(24*time.Hour))
	}
	if got := reapplyAfter(&reviewed, time.Hour, now); got != nil {
		t.Fatalf("an elapsed cooldown answers null, got %v", got)
	}
	if got := reapplyAfter(&reviewed, 0, now); got != nil {
		t.Fatalf("a zero cooldown answers null, got %v", got)
	}
	if got := reapplyAfter(nil, 24*time.Hour, now); got != nil {
		t.Fatalf("an unreviewed application answers null, got %v", got)
	}
}
