package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

const sourceKeyNextMoe = "nextmoe"

type WorkPopularityRow struct {
	SourceID int16
	Metric   int16
	Value    int64
}

func (s *ReadService) loadWorkPopularity(ctx context.Context, subjects []claimSubject) (map[int64][]WorkPopularityRow, error) {
	out := make(map[int64][]WorkPopularityRow, len(subjects))
	if len(subjects) == 0 {
		return out, nil
	}
	workIDs := make([]int64, 0, len(subjects))
	for _, sub := range subjects {
		workIDs = append(workIDs, sub.WorkID)
	}
	if err := s.nativeWorkPopularity(ctx, workIDs, out); err != nil {
		return nil, err
	}
	return out, s.folderFavorites(ctx, workIDs, out)
}

// folderFavorites counts the distinct users holding each work in any folder and
// appends it as a nextmoe/favorites popularity row.
//
// Computed on read rather than rolled up: catalog_user_folder_item IS the
// canonical favorites store, so a rollup table would be a second answer to the
// same question with nothing to keep the two honest. The playtime rollup
// (jobs/userplaytime) exists because a median over per-user rows is not a
// count; if this ever has to back sort=popularity it needs the same treatment.
//
// Private folders count. The number is an aggregate over users and names none
// of them, and excluding them would silently drop most of the imported corpus.
//
// Emitted even when it is zero: every other popularity row is absent when that
// source publishes nothing, but this count is always known, so absence here
// would read as "unknown" instead of "nobody".
func (s *ReadService) folderFavorites(ctx context.Context, workIDs []int64, out map[int64][]WorkPopularityRow) error {
	var sourceID int16
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_source WHERE key = ?`, sourceKeyNextMoe).Scan(&sourceID).Error; err != nil {
		return err
	}
	if sourceID == 0 {
		return nil
	}
	var rows []struct {
		WorkID int64 `gorm:"column:work_id"`
		Value  int64 `gorm:"column:value"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT work_id, count(DISTINCT owner_uid) AS value FROM catalog_user_folder_item
		WHERE work_id IN ? GROUP BY work_id`, workIDs).Scan(&rows).Error; err != nil {
		return err
	}
	byWork := make(map[int64]int64, len(rows))
	for _, r := range rows {
		byWork[r.WorkID] = r.Value
	}
	for _, id := range workIDs {
		out[id] = append(out[id], WorkPopularityRow{
			SourceID: sourceID, Metric: model.PopularityMetricFavorites, Value: byWork[id],
		})
	}
	return nil
}

func (s *ReadService) nativeWorkPopularity(ctx context.Context, workIDs []int64, out map[int64][]WorkPopularityRow) error {
	var rows []struct {
		WorkID   int64 `gorm:"column:work_id"`
		SourceID int16 `gorm:"column:source_id"`
		Metric   int16 `gorm:"column:metric"`
		Value    int64 `gorm:"column:value"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT work_id, source_id, metric, value FROM catalog_work_popularity
		WHERE work_id IN ? ORDER BY work_id, source_id, metric`, workIDs).Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		out[r.WorkID] = append(out[r.WorkID], WorkPopularityRow{
			SourceID: r.SourceID, Metric: r.Metric, Value: r.Value,
		})
	}
	return nil
}
