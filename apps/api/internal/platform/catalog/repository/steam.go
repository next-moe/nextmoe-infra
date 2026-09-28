package repository

import (
	"context"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func SteamAnchoredWorks(ctx context.Context, db *gorm.DB, steamSource int16) (map[string][]int64, error) {
	var rows []struct {
		WorkID int64  `gorm:"column:work_id"`
		Appid  string `gorm:"column:appid"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT DISTINCT a.work_id, a.external_id AS appid
		FROM `+model.WorkExactRefsSQL+` a
		JOIN catalog_work w ON w.id = a.work_id AND w.deleted_at IS NULL
		WHERE a.source_id = ?
		  AND w.medium_id = (SELECT id FROM catalog_medium WHERE key = 'galgame')
		ORDER BY a.external_id, a.work_id`, steamSource).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string][]int64, len(rows))
	for _, r := range rows {
		out[r.Appid] = append(out[r.Appid], r.WorkID)
	}
	return out, nil
}
