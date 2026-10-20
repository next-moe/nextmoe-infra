package service

import (
	"api/internal/platform/catalog/editspec"
	"context"
)

type WorkSeriesRow struct {
	ID          int64  `gorm:"column:id"`
	Name        string `gorm:"column:name"`
	SourceID    int16  `gorm:"column:source_id"`
	MemberCount int    `gorm:"column:member_count"`
}

func (s *ReadService) loadWorkSeries(ctx context.Context, subjects []claimSubject) (map[int64][]WorkSeriesRow, error) {
	out := make(map[int64][]WorkSeriesRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	var rows []struct {
		WorkID   int64  `gorm:"column:work_id"`
		ID       int64  `gorm:"column:id"`
		Name     string `gorm:"column:name"`
		SourceID int16  `gorm:"column:source_id"`
	}
	// No member count here: it used to be a bare count(*) over
	// catalog_series_member with no soft-delete, status, medium, claim-state or
	// r18 predicate, so works/{id}.series[].member_count disagreed with
	// series/{id}.work_count for the same series. publicWorkSeries fills it from
	// the same workCountsFor the series faces use.
	if err := s.db.WithContext(ctx).Raw(`
		SELECT m.work_id, se.id, se.display_name AS name, se.source_id
		FROM catalog_series_member m
		JOIN catalog_series se ON se.id = m.series_id
		WHERE m.work_id IN ? AND `+editspec.NotSuppressedWorkSeriesSQL("m")+`
		ORDER BY m.work_id, se.id`, workIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], WorkSeriesRow{
			ID: r.ID, Name: r.Name, SourceID: r.SourceID,
		})
	}
	return out, nil
}
