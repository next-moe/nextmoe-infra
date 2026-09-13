package repository

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestUserSiteRole_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run (scripts/ephemeral-test-db.sh create <slug>)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.UserSiteRole{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewUserSiteRoleRepository(db)
	ctx := context.Background()

	const uid, sid = uint(990001), uint(42)
	clean := func() { db.Where("user_id = ?", uid).Delete(&model.UserSiteRole{}) }
	clean()
	t.Cleanup(clean)

	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	grant := func(role string, exp *time.Time) {
		t.Helper()
		if err := repo.Grant(ctx, &model.UserSiteRole{
			UserID: uid, SiteID: sid, RoleName: role, GrantedBy: 1, GrantedAt: time.Now(), ExpiresAt: exp,
		}); err != nil {
			t.Fatalf("grant %s: %v", role, err)
		}
	}
	grant("moderator", &future)
	grant("event_organizer", &past)
	grant("creator", nil)

	names, err := repo.ActiveRoleNames(ctx, uid, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(names, []string{"creator", "moderator"}) {
		t.Fatalf("ActiveRoleNames = %v, want [creator moderator]", names)
	}

	grant("moderator", &future)
	var count int64
	db.Model(&model.UserSiteRole{}).
		Where("user_id = ? AND site_id = ? AND role_name = ?", uid, sid, "moderator").Count(&count)
	if count != 1 {
		t.Fatalf("re-grant produced %d rows, want 1", count)
	}

	byUser, err := repo.ActiveRoleNamesForUsers(ctx, []uint{uid}, sid)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(byUser[uid], []string{"creator", "moderator"}) {
		t.Fatalf("batch = %v, want [creator moderator]", byUser[uid])
	}

	if err := repo.Revoke(ctx, uid, sid, "moderator"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Revoke(ctx, uid, sid, "moderator"); err != nil {
		t.Fatalf("second revoke: %v", err)
	}
	names, _ = repo.ActiveRoleNames(ctx, uid, sid)
	if !slices.Equal(names, []string{"creator"}) {
		t.Fatalf("after revoke = %v, want [creator]", names)
	}
}
