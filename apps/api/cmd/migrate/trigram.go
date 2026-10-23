package main

import (
	"fmt"

	"gorm.io/gorm"
)

// usersTrigramIndexes back the substring searches on users. /users/search
// runs name ILIKE '%q%' on every mention-picker keystroke and the admin user
// list searches name and email the same way; with only btree indexes each call
// read all ~140k users (76ms mean, 15h of database time 2026-10-11..23) while
// holding one of oauth's pool connections, which the 2026-10-22 disk stall
// turned into a queue. pg_trgm tokenises CJK names too (checked on the prod
// image, postgres:18-alpine with en_US.utf8).
var usersTrigramIndexes = []struct{ name, column string }{
	{"idx_users_name_trgm", "name"},
	{"idx_users_email_trgm", "email"},
}

// ensureUsersTrigramIndexes builds them CONCURRENTLY so a deploy never blocks
// sign-ups on the users table. A concurrent build that fails leaves an INVALID
// index behind, which IF NOT EXISTS would then skip forever, so an invalid one
// is dropped and rebuilt. Existing rows need nothing: the build indexes them.
func ensureUsersTrigramIndexes(db *gorm.DB) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`).Error; err != nil {
		return fmt.Errorf("create pg_trgm: %w", err)
	}
	for _, ix := range usersTrigramIndexes {
		var invalid bool
		if err := db.Raw(`
			SELECT EXISTS (
				SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid
				 WHERE c.relname = ? AND NOT i.indisvalid)`, ix.name).Scan(&invalid).Error; err != nil {
			return fmt.Errorf("inspect %s: %w", ix.name, err)
		}
		if invalid {
			if err := db.Exec(`DROP INDEX CONCURRENTLY IF EXISTS ` + ix.name).Error; err != nil {
				return fmt.Errorf("drop invalid %s: %w", ix.name, err)
			}
		}
		if err := db.Exec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + ix.name +
			` ON users USING gin (` + ix.column + ` gin_trgm_ops)`).Error; err != nil {
			return fmt.Errorf("create %s: %w", ix.name, err)
		}
	}
	return nil
}
