package migrate

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// catalog_split_request (editor phase 2, 2026-10-21) records requests to split
// releases of a work off into a work of their own: the chosen releases, the new
// work's titles and the source entries to promote to it, the reviewer's
// decision, the cooling-off, the execution (new_work_id plus an execution
// record of every row the split created or moved, which undo replays) and an
// undo. changed_at moves on execution and undo; it is the cursor of the public
// split feed. A new, empty table; no existing row is touched.
//
// uq_catalog_split_request_pending allows one open or approved split per work,
// so two approved splits cannot race for the same releases.
func migrateSplitRequests(db *gorm.DB) error {
	if err := db.AutoMigrate(&model.CatalogSplitRequest{}); err != nil {
		return fmt.Errorf("automigrate catalog_split_request: %w", err)
	}
	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_catalog_split_request_pending
		    ON catalog_split_request(entity_type, entity_id)
		    WHERE status IN (0, 1)`).Error; err != nil {
		return fmt.Errorf("create index uq_catalog_split_request_pending: %w", err)
	}
	return nil
}
