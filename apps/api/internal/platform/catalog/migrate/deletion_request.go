package migrate

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// catalog_deletion_request (editor phase 2, 2026-10-21) records requests to
// delete a catalog entry, works first: who asked and why, the reviewer's
// decision, the cooling-off, the execution (a soft delete that keeps every
// external ref as a tombstone) and a later restore. It is a new, empty table;
// no existing row is touched. origin_site is nullable because the request
// faces fill it only once source-site attribution lands.
//
// uq_catalog_deletion_request_pending allows one open or approved request per
// entry, so two reviewers cannot approve two deletions of the same work.
func migrateDeletionRequests(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.CatalogDeletionRequest{}); err != nil {
		return fmt.Errorf("automigrate catalog_deletion_request: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_deletion_request_pending
		    ON catalog_deletion_request(entity_type, entity_id)
		    WHERE status IN (0, 1)`).Error; err != nil {
		return fmt.Errorf("create index uq_catalog_deletion_request_pending: %w", err)
	}
	return nil
}
