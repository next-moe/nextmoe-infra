package officialcovers

import (
	"context"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

type candidate struct {
	WorkID int64  `gorm:"column:work_id"`
	VNDBID string `gorm:"column:vndb_id"`
	BgmID  string `gorm:"column:bgm_id"`
}

func loadCandidates(ctx context.Context, db *gorm.DB, ids []int64) ([]candidate, error) {
	anchor := func(source int16) string {
		return `(SELECT min(r.external_id) FROM catalog_external_ref r
			WHERE r.entity_type = @work AND r.entity_id = w.id AND r.link_kind = @exact
			  AND r.dead_at IS NULL AND r.source_id = ` + itoa(source) + `)`
	}
	sql := `SELECT * FROM (
		SELECT w.id AS work_id,
		       CASE WHEN NOT EXISTS (SELECT 1 FROM catalog_work_cover d
		                              WHERE d.work_id = w.id AND d.source_id = @vndb AND d.kind = 'dig')
		            THEN ` + anchor(model.SourceVNDB) + ` END AS vndb_id,
		       CASE WHEN NOT EXISTS (SELECT 1 FROM catalog_work_cover b
		                              WHERE b.work_id = w.id AND b.source_id = @bgm)
		            THEN ` + anchor(model.SourceBangumi) + ` END AS bgm_id
		  FROM catalog_work w
		 WHERE w.status = @live AND w.deleted_at IS NULL
		   AND EXISTS (SELECT 1 FROM catalog_work_cover u
		                WHERE u.work_id = w.id AND u.source_id IN @uploaded)`
	args := map[string]any{
		"work": model.EntityTypeWork, "exact": model.LinkKindExact, "live": model.WorkStatusLive,
		"vndb": model.SourceVNDB, "bgm": model.SourceBangumi, "uploaded": model.UploadedCoverSourceIDs,
	}
	if len(ids) > 0 {
		sql += ` AND w.id IN @ids`
		args["ids"] = ids
	}
	sql += `) c WHERE vndb_id IS NOT NULL OR bgm_id IS NOT NULL ORDER BY work_id`
	var out []candidate
	return out, db.WithContext(ctx).Raw(sql, args).Scan(&out).Error
}
