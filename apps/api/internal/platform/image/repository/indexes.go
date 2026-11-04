package repository

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

// adminIndexes back the admin /images list and the stats cards. Without them
// every page load ran seven sequential scans of the 1.2 GB images heap
// (887k rows, 5-15s each on prod 2026-10-31). The INCLUDE columns let the
// counts and sums run as index-only scans.
var adminIndexes = []struct{ name, def string }{
	{"idx_images_created_at", `images (created_at) INCLUDE (size_bytes, deleted_at)`},
	{"idx_images_review_status_created_at", `images (review_status, created_at) INCLUDE (deleted_at)`},
	{"idx_image_site_usage_site_hash", `image_site_usage (site, hash) INCLUDE (upload_count)`},
}

// EnsureIndexes builds them CONCURRENTLY so a deploy never blocks uploads on
// the images table. A concurrent build that fails leaves an INVALID index
// behind, which IF NOT EXISTS would then skip forever, so an invalid one is
// dropped and rebuilt.
func EnsureIndexes(ctx context.Context, db *gorm.DB) error {
	db = db.WithContext(ctx)
	for _, ix := range adminIndexes {
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
		if err := db.Exec(`CREATE INDEX CONCURRENTLY IF NOT EXISTS ` + ix.name + ` ON ` + ix.def).Error; err != nil {
			return fmt.Errorf("create %s: %w", ix.name, err)
		}
	}
	return nil
}
