package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/auth/repository"
)

func (r *ssoRig) ageRotation(t *testing.T, id uint, late bool) {
	t.Helper()
	if err := r.db.Exec(`UPDATE sessions SET rotated_at = ?, rotation_late = ? WHERE id = ?`,
		time.Now().Add(-10*time.Minute), late, id).Error; err != nil {
		t.Fatalf("age rotation: %v", err)
	}
}

func TestClientRefreshReplaysTheTokenALateRotationNeverDelivered(t *testing.T) {
	r := newSSORig(t, false)
	ctx := context.Background()
	cs := r.clientSession(t, "")
	old := cs.RefreshToken

	first, err := r.oauth.RefreshWithClient(ctx, old, r.client.ID, "")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	r.ageRotation(t, cs.ID, true)

	replay, err := r.oauth.RefreshWithClient(ctx, old, r.client.ID, "")
	if err != nil {
		t.Fatalf("old token after a late rotation must replay, got %v", err)
	}
	if replay.RefreshToken != first.RefreshToken {
		t.Fatalf("replay handed out %q, want the current token %q", replay.RefreshToken, first.RefreshToken)
	}

	r.ageRotation(t, cs.ID, false)
	if _, err := r.oauth.RefreshWithClient(ctx, old, r.client.ID, ""); err == nil {
		t.Fatal("an old token past the grace window of a prompt rotation must still be refused")
	}
	var n int64
	r.db.Raw(`SELECT count(*) FROM sessions WHERE id = ?`, cs.ID).Scan(&n)
	if n != 0 {
		t.Fatal("reuse of a promptly rotated token must still revoke the session")
	}
}

func TestTheNextRotationClearsTheLateMark(t *testing.T) {
	r := newSSORig(t, false)
	ctx := context.Background()
	cs := r.clientSession(t, "")

	first, err := r.oauth.RefreshWithClient(ctx, cs.RefreshToken, r.client.ID, "")
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	r.ageRotation(t, cs.ID, true)
	if _, err := r.oauth.RefreshWithClient(ctx, first.RefreshToken, r.client.ID, ""); err != nil {
		t.Fatalf("refresh with the current token: %v", err)
	}
	if r.reload(t, cs.ID).RotationLate {
		t.Fatal("a fresh rotation must clear rotation_late")
	}
}

func TestAccountRefreshReplaysAfterALateRotation(t *testing.T) {
	r := newSSORig(t, false)
	ctx := context.Background()
	now := time.Now()
	op := r.opSession(t, "browser-l-"+r.tag, now, now.Add(time.Hour))

	first, err := r.auth.RefreshToken(ctx, op.RefreshToken)
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	r.ageRotation(t, op.ID, true)
	replay, err := r.auth.RefreshToken(ctx, op.RefreshToken)
	if err != nil {
		t.Fatalf("old account token after a late rotation must replay, got %v", err)
	}
	if replay.RefreshToken != first.RefreshToken {
		t.Fatalf("replay handed out %q, want %q", replay.RefreshToken, first.RefreshToken)
	}
}

func TestMarkIfRotatedLateOnlyMarksSlowRotationsOfTheCurrentToken(t *testing.T) {
	r := newSSORig(t, false)
	ctx := context.Background()
	repo := repository.NewSessionRepository(r.db)
	cs := r.clientSession(t, "")

	markIfRotatedLate(ctx, repo, cs.ID, cs.RefreshToken, time.Now())
	if r.reload(t, cs.ID).RotationLate {
		t.Fatal("a prompt rotation was marked late")
	}
	markIfRotatedLate(ctx, repo, cs.ID, "someone-elses-token", time.Now().Add(-6*time.Second))
	if r.reload(t, cs.ID).RotationLate {
		t.Fatal("a slow rotation marked a token that is no longer current")
	}
	markIfRotatedLate(ctx, repo, cs.ID, cs.RefreshToken, time.Now().Add(-6*time.Second))
	if !r.reload(t, cs.ID).RotationLate {
		t.Fatal("a rotation slower than LateRotationAfter was not marked")
	}
}
