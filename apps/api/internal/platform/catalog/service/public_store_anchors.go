package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

type StoreAnchor struct{ Source, ExternalID string }

func (s *PublicService) StoreAnchorsFor(ctx context.Context, workIDs []int64) (anchors map[int64][]StoreAnchor, visible map[int64]bool, err error) {
	anchors = map[int64][]StoreAnchor{}
	visible = map[int64]bool{}
	if len(workIDs) == 0 {
		return anchors, visible, nil
	}
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_work WHERE id IN ? AND deleted_at IS NULL AND medium_id = ? AND status = ?`,
		workIDs, galgameMediumID, model.WorkStatusLive,
	).Scan(&ids).Error; err != nil {
		return nil, nil, err
	}
	for _, id := range ids {
		visible[id] = true
		anchors[id] = []StoreAnchor{}
	}
	if len(ids) == 0 {
		return anchors, visible, nil
	}
	var rows []struct {
		WorkID     int64  `gorm:"column:work_id"`
		Source     string `gorm:"column:source"`
		ExternalID string `gorm:"column:external_id"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT a.work_id, src.key AS source, a.external_id
		FROM `+model.WorkExactRefsSQL+` a
		JOIN catalog_source src ON src.id = a.source_id
		WHERE a.work_id IN ? AND src.key IN ('dlsite','steam','getchu')
		ORDER BY a.work_id, src.key, a.external_id`, ids,
	).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, r := range rows {
		anchors[r.WorkID] = append(anchors[r.WorkID], StoreAnchor{Source: r.Source, ExternalID: r.ExternalID})
	}
	return anchors, visible, nil
}
