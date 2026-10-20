package seriesorder

import (
	"context"

	"api/internal/platform/catalog/editspec"

	"gorm.io/gorm"
)

type Current struct {
	Position int16
	Kind     int16
}

func LoadCurrent(ctx context.Context, db *gorm.DB, seriesID int64) (map[int64]Current, error) {
	var rows []struct {
		WorkID   int64 `gorm:"column:work_id"`
		Position int16 `gorm:"column:position"`
		Kind     int16 `gorm:"column:kind"`
	}
	if err := db.WithContext(ctx).Raw(`
		SELECT work_id, position, kind FROM catalog_series_member WHERE series_id = ?`,
		seriesID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]Current, len(rows))
	for _, r := range rows {
		out[r.WorkID] = Current{Position: r.Position, Kind: r.Kind}
	}
	return out, nil
}

func Apply(ctx context.Context, db *gorm.DB, seriesID int64, want []Assignment,
	have map[int64]Current, apply bool) ([]int64, error) {
	var changed []int64
	for _, a := range want {
		cur, ok := have[a.WorkID]
		if !ok {
			continue
		}
		if cur.Position == a.Position && cur.Kind == a.Kind {
			continue
		}
		changed = append(changed, a.WorkID)
		if !apply {
			continue
		}
		if err := db.WithContext(ctx).Exec(`
			UPDATE catalog_series_member sm
			SET position = CASE WHEN `+editspec.SeriesMemberColumnIsHumanSQL("sm", "position")+` THEN sm.position ELSE ? END,
			    kind = CASE WHEN `+editspec.SeriesMemberColumnIsHumanSQL("sm", "kind")+` THEN sm.kind ELSE ? END,
			    updated_at = now()
			WHERE sm.series_id = ? AND sm.work_id = ?`, a.Position, a.Kind, seriesID, a.WorkID).Error; err != nil {
			return nil, err
		}
	}
	return changed, nil
}
