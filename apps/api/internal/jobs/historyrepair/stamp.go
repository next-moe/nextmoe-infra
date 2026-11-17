package historyrepair

import (
	"context"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/provenance"

	"gorm.io/gorm"
)

// StampUploads marks the curated covers the catalog editor inserted before
// applyCovers stamped its inserts. Every curated cover row created on or after
// Cut came from that insert: the galgame-era rows were bulk-loaded on
// 2026-07-29 and 07-30, and no job writes source 12.
func StampUploads(ctx context.Context, db *gorm.DB, apply bool) (*Stats, error) {
	st := newStats("stamp-uploads", apply)
	where := `source_id = ? AND created_at >= ? AND field_provenance -> ? IS NULL`
	args := []any{model.SourceCurated, Cut, model.CoverUploadStampColumn}
	var rows []struct {
		WorkID    int64
		ImageHash string
	}
	if err := db.WithContext(ctx).Table("catalog_work_cover").Select("work_id, image_hash").
		Where(where, args...).Order("work_id, image_hash").Scan(&rows).Error; err != nil {
		return nil, err
	}
	st.Scanned = len(rows)
	seen := map[int64]struct{}{}
	for _, r := range rows {
		if _, ok := seen[r.WorkID]; !ok {
			seen[r.WorkID] = struct{}{}
			st.Works = append(st.Works, r.WorkID)
		}
	}
	if !apply || len(rows) == 0 {
		return st, nil
	}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Exec(`UPDATE catalog_work_cover
			SET field_provenance = jsonb_set(field_provenance, ARRAY[?::text], jsonb_build_array(jsonb_build_object(
				'source', ?::text,
				'at', to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'))))
			WHERE `+where,
			append([]any{model.CoverUploadStampColumn, provenance.SourceCurated}, args...)...)
		if res.Error != nil {
			return res.Error
		}
		st.Written = int(res.RowsAffected)
		return repository.TouchWorks(ctx, tx, st.Works)
	})
	return st, err
}
