package main

import (
	"fmt"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// TestNeutralizeAgeAttestationKeepsEveryRowsEffectiveDisplay covers the three
// states a users row could be in on 2026-09-23, when the age attestation was
// retired. The one that is easy to get wrong is the straggler: it carries the
// previous day's 'blur' backfill with a null attestation, so it renders as
// 'hide', and confirming it without first flipping the column to 'hide' would
// silently un-blur an account that never asked for it.
func TestNeutralizeAgeAttestationKeepsEveryRowsEffectiveDisplay(t *testing.T) {
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
	if err := db.AutoMigrate(&authModel.User{}); err != nil {
		dbtest.Skipf(t, "migrate users: %v", err)
	}
	if err := authModel.AddUserContentPreferenceColumns(db); err != nil {
		dbtest.Skipf(t, "content preference columns: %v", err)
	}

	attested := time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC)
	// The hook stamps adult_confirmed_at on every insert, so the pre-retirement
	// states have to be written back over the row in raw SQL.
	seed := func(t *testing.T, slug, display string, confirmedAt *time.Time) *authModel.User {
		t.Helper()
		tag := time.Now().UnixNano() % 1e8
		u := &authModel.User{
			Name:  fmt.Sprintf("%s%08d", slug, tag),
			Email: fmt.Sprintf("%s%d@example.test", slug, tag),
		}
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
		t.Cleanup(func() { db.Unscoped().Delete(&authModel.User{}, u.ID) })
		if err := db.Exec(
			`UPDATE users SET nsfw_display = ?, adult_confirmed_at = ? WHERE id = ?`,
			display, confirmedAt, u.ID,
		).Error; err != nil {
			t.Fatalf("rewind %s: %v", slug, err)
		}
		return u
	}

	straggler := seed(t, "str", authModel.NSFWDisplayBlur, nil)
	hidden := seed(t, "hid", authModel.NSFWDisplayHide, nil)
	chose := seed(t, "cho", authModel.NSFWDisplayShow, &attested)

	for pass := 1; pass <= 2; pass++ {
		if err := authModel.NeutralizeAgeAttestation(db); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	reload := func(t *testing.T, id uint) authModel.User {
		t.Helper()
		var u authModel.User
		if err := db.First(&u, id).Error; err != nil {
			t.Fatalf("reload %d: %v", id, err)
		}
		return u
	}

	for _, tc := range []struct {
		name    string
		id      uint
		display string
	}{
		{"a straggler registered under the old default loses the blur it never saw", straggler.ID, authModel.NSFWDisplayHide},
		{"an account already on hide stays there", hidden.ID, authModel.NSFWDisplayHide},
		{"an account that attested keeps the value it chose", chose.ID, authModel.NSFWDisplayShow},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := reload(t, tc.id)
			if got.AdultConfirmedAt == nil {
				t.Fatal("adult_confirmed_at is still null after the retirement backfill")
			}
			if got.NSFWDisplay != tc.display {
				t.Fatalf("nsfw_display = %q, want %q", got.NSFWDisplay, tc.display)
			}
		})
	}

	if got := reload(t, chose.ID); !got.AdultConfirmedAt.UTC().Equal(attested) {
		t.Fatalf("the backfill moved a real attestation from %s to %s", attested, got.AdultConfirmedAt.UTC())
	}
}
