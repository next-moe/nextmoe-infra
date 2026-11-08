package migrate

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// backfillFolderItemPositions numbers the items that still hold the column
// default 0 — every row that existed before 2026-11-07, plus any the previous
// image wrote while this one was rolling out — after the folder's highest
// position, in the order they were added. Rows that already have a position
// are never touched, so an owner's reordering survives every re-run, and
// updated_at is left alone: bumping it would re-deliver all ~400k memberships
// on every manager's incremental sync.
func backfillFolderItemPositions(db *gorm.DB) error {
	if err := db.Exec(`
		WITH numbered AS (
		    SELECT i.id,
		           COALESCE((SELECT max(p.position) FROM catalog_user_folder_item p
		                      WHERE p.folder_id = i.folder_id AND p.position <> 0), 0)
		           + ? * row_number() OVER (PARTITION BY i.folder_id ORDER BY i.created_at, i.id) AS pos
		      FROM catalog_user_folder_item i
		     WHERE i.position = 0)
		UPDATE catalog_user_folder_item i SET position = n.pos
		  FROM numbered n WHERE i.id = n.id`, model.FolderItemPositionGap).Error; err != nil {
		return fmt.Errorf("backfill folder item positions: %w", err)
	}
	return nil
}
