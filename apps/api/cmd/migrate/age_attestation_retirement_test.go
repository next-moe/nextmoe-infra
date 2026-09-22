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

// TestNeutralizeAgeAttestationLandsUnconfirmedRowsOnShow covers the three
// states a users row could be in on 2026-09-23, when the age attestation was
// retired and, the same day, the second ruling made every account see adult
// content. An unconfirmed row lands on 'show' regardless of what it held —
// only a row that is already confirmed is out of reach, which is the guard
// that lets this rerun on every deploy without stomping a choice made since.
// The attested seed deliberately holds a NON-'show' value so that "kept the
// value it chose" cannot pass by coincidence with "was flipped to 'show'".
func TestNeutralizeAgeAttestationLandsUnconfirmedRowsOnShow(t *testing.T) {
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
	chose := seed(t, "cho", authModel.NSFWDisplayHide, &attested)

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
		{"an unconfirmed blur row lands on show", straggler.ID, authModel.NSFWDisplayShow},
		{"an unconfirmed hide row lands on show too", hidden.ID, authModel.NSFWDisplayShow},
		{"a confirmed account keeps the value it chose", chose.ID, authModel.NSFWDisplayHide},
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
