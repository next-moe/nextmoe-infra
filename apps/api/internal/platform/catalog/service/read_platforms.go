package service

import (
	"context"

	"api/internal/platform/catalog/editspec"
)

type WorkPlatformRow struct {
	Platform string `gorm:"column:platform"`
	SourceID int16  `gorm:"column:source_id"`
}

func (s *ReadService) loadWorkPlatforms(ctx context.Context, subjects []claimSubject) (map[int64][]WorkPlatformRow, error) {
	out := make(map[int64][]WorkPlatformRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	var rows []struct {
		WorkID   int64  `gorm:"column:work_id"`
		Platform string `gorm:"column:platform"`
		SourceID int16  `gorm:"column:source_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT wp.work_id, wp.platform, wp.source_id
		FROM catalog_work_platform wp
		WHERE wp.work_id IN ? AND `+editspec.NotSuppressedWorkPlatformSQL("wp.work_id", "wp.platform")+`
		ORDER BY wp.work_id, wp.platform, wp.source_id`, workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], WorkPlatformRow{Platform: r.Platform, SourceID: r.SourceID})
	}
	return out, nil
}
