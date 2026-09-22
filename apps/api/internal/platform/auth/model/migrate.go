package model

import (
	"fmt"

	"gorm.io/gorm"
)

// AddUserContentPreferenceColumns adds the 2026-09-22 account-level content
// preference columns to the populated users table in raw SQL, before
// AutoMigrate sees them.
//
//   - adult_confirmed_at is nullable and every existing row backfills to NULL:
//     nobody attested an age before the column existed, and reading "registered
//     earlier" as "already attested" would un-gate every account at once. No API
//     may set it back to NULL once written. (That gate lasted one day —
//     NeutralizeAgeAttestation, which runs right after this, now fills exactly
//     those NULLs. Read the two together before concluding anything about what
//     a row ends up holding.)
//   - nsfw_display is NOT NULL and existing rows backfill to the column
//     DEFAULT. The DEFAULT is KEPT rather than dropped, for the INSERTs that do
//     not go through the model at all — the seed rows in cmd/migrate's own
//     tests, and anything operational — since a NOT NULL column with no default
//     rejects them. (GORM's own INSERTs do carry the value: a `default` tag
//     holding a parseable literal is sent explicitly, not left to the database.
//     The 2026-09-22 version of this comment claimed the opposite.) That
//     default was 'blur' on 2026-09-22 and is 'hide' since 2026-09-23 — see
//     NeutralizeAgeAttestation for why a database arriving late must not
//     backfill 'blur' any more.
//
// Per-site NSFW states held by downstream products are deliberately NOT
// migrated in: they were never an age attestation.
//
// On a brand-new database there is no users table yet — AutoMigrate creates it
// from the model a moment later — so this is a no-op there and the CHECK
// constraint is EnsureNSFWDisplayCheck's job, after AutoMigrate.
func AddUserContentPreferenceColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable("users") {
		return nil
	}

	stmts := []string{
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS adult_confirmed_at timestamptz`,
		`CREATE INDEX IF NOT EXISTS idx_users_adult_confirmed_at ON users (adult_confirmed_at)`,
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS nsfw_display varchar(8) NOT NULL DEFAULT 'hide'`,
		`ALTER TABLE users ALTER COLUMN nsfw_display SET DEFAULT 'hide'`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("content preference migrate %q: %w", s, err)
		}
	}
	return nil
}

// NeutralizeAgeAttestation carries out the 2026-09-23 decision that retired the
// age attestation shipped the day before: every account is adult by
// construction. The column and the three-state nsfw_display model both stay —
// only the precondition is gone — so the effective rule the deployed downstream
// sites already ship, `adult_confirmed ? nsfw_display : 'hide'`, keeps working
// untouched with adult_confirmed simply always true.
//
// What happened to existing rows:
//
//   - Production was backfilled by hand on 2026-09-23, before this code
//     existed: 130,292 unconfirmed rows got adult_confirmed_at = now(), and
//     their nsfw_display was flipped 'blur' -> 'hide' first. The flip is the
//     part that is easy to miss. An unconfirmed row carried the 2026-09-22
//     'blur' backfill and rendered as 'hide'; confirming it without the flip
//     would have un-blurred 130k feeds nobody asked to change. The 33 accounts
//     that had genuinely attested kept the values they chose.
//   - This function repeats both halves in the same order for the accounts
//     registered between that manual UPDATE and this code deploying, and for
//     every dev/staging database that never saw the manual pass. Idempotent:
//     after one run no row matches adult_confirmed_at IS NULL again.
//
// New rows do not depend on it — User.BeforeCreate stamps the column — so on a
// database that has only ever run this version both statements match nothing.
func NeutralizeAgeAttestation(db *gorm.DB) error {
	if !db.Migrator().HasTable("users") {
		return nil
	}

	stmts := []string{
		`UPDATE users SET nsfw_display = 'hide' WHERE adult_confirmed_at IS NULL AND nsfw_display <> 'hide'`,
		`UPDATE users SET adult_confirmed_at = now() WHERE adult_confirmed_at IS NULL`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("age attestation retirement %q: %w", s, err)
		}
	}
	return nil
}

// EnsureNSFWDisplayCheck runs after AutoMigrate, which is the only point at
// which the users table is guaranteed to exist on both an established and a
// brand-new database. The constraint is dropped and recreated so a later change
// to the value set replaces it instead of failing on the existing one.
func EnsureNSFWDisplayCheck(db *gorm.DB) error {
	stmts := []string{
		`ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_nsfw_display`,
		`ALTER TABLE users ADD CONSTRAINT chk_users_nsfw_display CHECK (nsfw_display IN ('hide', 'blur', 'show'))`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("content preference migrate %q: %w", s, err)
		}
	}
	return nil
}
