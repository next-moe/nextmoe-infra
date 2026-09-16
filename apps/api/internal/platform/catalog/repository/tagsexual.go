package repository

import (
	"context"

	"gorm.io/gorm"
)

// InheritTagSexual flags a source's work-tag rows sexual when another row of
// the same source and name already is. classify-tag-safety applies its verdict
// per (source, name) and runs by hand, so rows an importer adds afterwards
// arrived unflagged: by 2026-09-16 the weekly Bangumi lane had left 1,486 and
// the DLsite genre backlog 39,071. Tag writers call this after they insert.
func InheritTagSexual(ctx context.Context, db *gorm.DB, sourceID int16) (int64, error) {
	res := db.WithContext(ctx).Exec(`
		UPDATE catalog_work_tag t SET sexual = true
		WHERE t.source_id = ? AND NOT t.sexual
		  AND EXISTS (SELECT 1 FROM catalog_work_tag s
		              WHERE s.name = t.name AND s.source_id = t.source_id AND s.sexual)`, sourceID)
	return res.RowsAffected, res.Error
}
