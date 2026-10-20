package service

import (
	"api/internal/platform/catalog/editspec"
	"context"
)

func (s *ReadService) loadWorkLabels(ctx context.Context, workIDs []int64) (map[int64][]LabelAttribution, error) {
	out := make(map[int64][]LabelAttribution, len(workIDs))
	if len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID      int64  `gorm:"column:work_id"`
		LabelID     int64  `gorm:"column:label_id"`
		DisplayName string `gorm:"column:display_name"`
		LabelKind   int16  `gorm:"column:label_kind"`
		Kind        int16  `gorm:"column:kind"`
		Lang        string `gorm:"column:lang"`
		LogoHash    string `gorm:"column:logo_hash"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT wl.work_id, wl.label_id, l.display_name, l.kind AS label_kind, wl.kind AS kind, l.lang, l.logo_hash
		FROM catalog_work_label wl JOIN catalog_label l ON l.id = wl.label_id AND l.deleted_at IS NULL
		WHERE wl.work_id IN ? AND `+editspec.NotSuppressedWorkLabelSQL("wl")+`
		ORDER BY wl.work_id, wl.kind, l.display_name`, workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], LabelAttribution{
			LabelID: r.LabelID, DisplayName: r.DisplayName, LabelKind: r.LabelKind, Kind: r.Kind,
			Lang: r.Lang, LogoHash: r.LogoHash,
		})
	}
	return out, nil
}
