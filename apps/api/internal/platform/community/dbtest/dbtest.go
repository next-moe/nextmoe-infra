package dbtest

import (
	"context"
	"database/sql"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

const suiteLockKey = 0x636f6d6d

func AcquireSuiteLock(db *sql.DB) func() {
	if db == nil {
		return func() {}
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", suiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", suiteLockKey)
		_ = conn.Close()
	}
}

func Board(db *gorm.DB, site, slug string) (int64, error) {
	b := model.CommunityBoard{Site: site, Slug: slug, Name: slug}
	err := db.Where("site = ? AND slug = ?", site, slug).FirstOrCreate(&b).Error
	return b.ID, err
}
