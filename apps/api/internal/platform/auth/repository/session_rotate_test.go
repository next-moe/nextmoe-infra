package repository

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRotateRefreshToken_CAS_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run (scripts/ephemeral-test-db.sh create <slug>)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}, &model.Session{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewSessionRepository(db)
	ctx := context.Background()

	// Anchor on a user we create ourselves — an ephemeral test DB has no
	// ambient rows, and skipping here made the suite green without running.
	tag := strconv.FormatInt(time.Now().UnixNano(), 10)
	anchor := &model.User{
		Name:  "cas-" + tag[len(tag)-10:],
		Email: "cas-" + tag + "@test.local",
	}
	if err := db.Create(anchor).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	defer db.Exec("DELETE FROM users WHERE id = ?", anchor.ID)
	uid := anchor.ID
	oldRT := "test-cas-rt-old-" + tag
	s := &model.Session{
		UserID:       uid,
		SessionToken: "test-cas-at-" + tag,
		RefreshToken: oldRT,
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := repo.Create(ctx, s); err != nil {
		t.Fatalf("create session: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, s.ID) }()

	now := time.Now()
	newRT := "test-cas-rt-new-" + tag

	won, err := repo.RotateRefreshToken(ctx, s.ID, oldRT, "test-cas-at2-"+tag, newRT, now, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("rotate (current): %v", err)
	}
	if !won {
		t.Fatal("CAS with the current token should win")
	}
	got, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("reread: %v", err)
	}
	if got.RefreshToken != newRT {
		t.Fatalf("current not rotated: got %q want %q", got.RefreshToken, newRT)
	}
	if got.PrevRefreshToken != oldRT {
		t.Fatalf("old token not demoted to prev: got %q want %q", got.PrevRefreshToken, oldRT)
	}

	won2, err := repo.RotateRefreshToken(ctx, s.ID, oldRT, "test-cas-evil-at-"+tag, "test-cas-evil-rt-"+tag, now, now.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("rotate (stale): %v", err)
	}
	if won2 {
		t.Fatal("CAS with a stale token must NOT win (that would be the lost-update bug)")
	}
	got2, err := repo.FindByID(ctx, s.ID)
	if err != nil {
		t.Fatalf("reread 2: %v", err)
	}
	if got2.RefreshToken != newRT {
		t.Fatalf("losing CAS clobbered the current token: got %q want %q", got2.RefreshToken, newRT)
	}
}
