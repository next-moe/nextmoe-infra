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
//     may set it back to NULL once written.
//   - nsfw_display is NOT NULL and existing rows backfill to 'blur'. The DEFAULT
//     is KEPT rather than dropped: "" is Go's zero value for a string, so GORM
//     omits the column from every user INSERT (registration, federation, the
//     admin console) and a NOT NULL column with no default would reject the row.
//     'blur' with a NULL adult_confirmed_at still reads as 'hide' — the
//     effective value is a function of both columns, see EffectiveNSFWDisplay.
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
		`ALTER TABLE users ADD COLUMN IF NOT EXISTS nsfw_display varchar(8) NOT NULL DEFAULT 'blur'`,
		`ALTER TABLE users ALTER COLUMN nsfw_display SET DEFAULT 'blur'`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("content preference migrate %q: %w", s, err)
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
