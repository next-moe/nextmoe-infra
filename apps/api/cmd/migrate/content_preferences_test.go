package main

import (
	"testing"

	authModel "api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestContentPreferenceMigrationIsIdempotentAndBackfills runs the platform
// migration's content-preference steps in the same order main() does, twice,
// against whatever state the test database is in. The ordering is the point: on
// an established database the raw pre-step has to backfill nsfw_display before
// AutoMigrate sees a NOT NULL column, and on a brand-new one it finds no users
// table at all, so the CHECK constraint can only be created after AutoMigrate.
func TestContentPreferenceMigrationIsIdempotentAndBackfills(t *testing.T) {
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

	// A user row that predates the columns, to prove what the backfill gives it.
	if err := db.AutoMigrate(&authModel.User{}); err != nil {
		dbtest.Skipf(t, "migrate users: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_nsfw_display`).Error; err != nil {
		t.Fatalf("drop constraint: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS nsfw_display`).Error; err != nil {
		t.Fatalf("drop nsfw_display: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS adult_confirmed_at`).Error; err != nil {
		t.Fatalf("drop adult_confirmed_at: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO users (name, email, created_at, updated_at) VALUES ('legacy_prefs', 'legacy_prefs@example.test', now(), now())`,
	).Error; err != nil {
		t.Fatalf("seed legacy user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE name = 'legacy_prefs'`) })

	for pass := 1; pass <= 2; pass++ {
		if err := authModel.AddUserContentPreferenceColumns(db); err != nil {
			t.Fatalf("pass %d pre-AutoMigrate: %v", pass, err)
		}
		if err := authModel.NeutralizeAgeAttestation(db); err != nil {
			t.Fatalf("pass %d attestation retirement: %v", pass, err)
		}
		if err := db.AutoMigrate(&authModel.User{}, &authModel.UserPreference{}); err != nil {
			t.Fatalf("pass %d AutoMigrate: %v", pass, err)
		}
		if err := authModel.EnsureNSFWDisplayCheck(db); err != nil {
			t.Fatalf("pass %d check constraint: %v", pass, err)
		}
	}

	var legacy authModel.User
	if err := db.Where("name = ?", "legacy_prefs").First(&legacy).Error; err != nil {
		t.Fatalf("reload legacy user: %v", err)
	}
	if legacy.NSFWDisplay != authModel.NSFWDisplayHide {
		t.Fatalf("a row that predates the column backfilled to %q, want %q",
			legacy.NSFWDisplay, authModel.NSFWDisplayHide)
	}
	if legacy.AdultConfirmedAt == nil {
		t.Fatal("a row that predates the column must still come out adult: the attestation was retired 2026-09-23")
	}
	if got := authModel.EffectiveNSFWDisplay(legacy.AdultConfirmedAt, legacy.NSFWDisplay); got != authModel.NSFWDisplayHide {
		t.Fatalf("the backfilled row is effectively %q, want %q", got, authModel.NSFWDisplayHide)
	}

	// The DEFAULT stays on the column: "" is Go's zero value, so GORM omits
	// nsfw_display from every user INSERT and a NOT NULL column without one
	// would reject registration outright.
	fresh := &authModel.User{Name: "fresh_prefs", Email: "fresh_prefs@example.test"}
	if err := db.Create(fresh).Error; err != nil {
		t.Fatalf("insert a user the way registration does: %v", err)
	}
	t.Cleanup(func() { db.Unscoped().Delete(&authModel.User{}, fresh.ID) })
	if err := db.First(fresh, fresh.ID).Error; err != nil {
		t.Fatalf("reload fresh user: %v", err)
	}
	if fresh.NSFWDisplay != authModel.NSFWDisplayHide {
		t.Fatalf("a newly registered user got nsfw_display %q, want %q",
			fresh.NSFWDisplay, authModel.NSFWDisplayHide)
	}
	if fresh.AdultConfirmedAt == nil {
		t.Fatal("a newly registered user has a null adult_confirmed_at: the BeforeCreate hook did not reach the INSERT")
	}

	if err := db.Exec(`UPDATE users SET nsfw_display = 'reveal' WHERE id = ?`, fresh.ID).Error; err == nil {
		t.Fatal("the CHECK constraint is missing: the database took a fourth display value")
	}
}
