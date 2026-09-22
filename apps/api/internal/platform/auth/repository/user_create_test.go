package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UserRepository.Create is the only statement that inserts a users row in
// production — registration and federated first login are both callers, and
// nothing else in the module writes the table — so the 2026-09-23 rule that
// every account is adult by construction is asserted once, here, rather than
// per caller.
func TestCreateMakesEveryAccountAdult(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		dbtest.Skipf(t, "pgcrypto: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		dbtest.Skipf(t, "migrate: %v", err)
	}
	repo := NewUserRepository(db)
	ctx := context.Background()

	create := func(t *testing.T, slug string, at *time.Time) *model.User {
		t.Helper()
		tag := time.Now().UnixNano() % 1e8
		u := &model.User{
			Name:             fmt.Sprintf("%s%08d", slug, tag),
			Email:            fmt.Sprintf("%s%d@test.local", slug, tag),
			AdultConfirmedAt: at,
		}
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("create: %v", err)
		}
		t.Cleanup(func() { db.Unscoped().Delete(&model.User{}, u.ID) })
		return u
	}

	t.Run("an account created with no attestation is stamped anyway", func(t *testing.T) {
		u := create(t, "adu", nil)
		if u.AdultConfirmedAt == nil {
			t.Fatal("the in-memory record came back without adult_confirmed_at")
		}
		stored, err := repo.FindByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if stored.AdultConfirmedAt == nil {
			t.Fatal("the stored row has a null adult_confirmed_at: the BeforeCreate hook never reached the INSERT")
		}
		if stored.NSFWDisplay != model.NSFWDisplayShow {
			t.Fatalf("stored nsfw_display = %q, want %q", stored.NSFWDisplay, model.NSFWDisplayShow)
		}
		if got := model.EffectiveNSFWDisplay(stored.AdultConfirmedAt, stored.NSFWDisplay); got != model.NSFWDisplayShow {
			t.Fatalf("effective display = %q, want %q", got, model.NSFWDisplayShow)
		}
	})

	t.Run("a caller that supplies a timestamp keeps it", func(t *testing.T) {
		at := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
		u := create(t, "kee", &at)
		stored, err := repo.FindByID(ctx, u.ID)
		if err != nil {
			t.Fatalf("reload: %v", err)
		}
		if stored.AdultConfirmedAt == nil || !stored.AdultConfirmedAt.UTC().Equal(at) {
			t.Fatalf("adult_confirmed_at = %v, want %s", stored.AdultConfirmedAt, at)
		}
	})
}
