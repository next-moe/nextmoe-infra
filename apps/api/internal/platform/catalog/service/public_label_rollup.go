package service

import (
	"context"
	"fmt"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

var labelRollupRelations = []int16{model.LabelRelationSubsidiary, model.LabelRelationImprint}

const labelRollupChildren = `SELECT r.other_label_id FROM catalog_label_relation r
	JOIN catalog_label c ON c.id = r.other_label_id AND c.deleted_at IS NULL
	WHERE r.label_id = ? AND r.relation IN ?`

var labelImprintWorkEdge = taxonomyEdge{sql: fmt.Sprintf(`(SELECT r.label_id AS key_id, wl.work_id
	FROM catalog_label_relation r
	JOIN catalog_label c ON c.id = r.other_label_id AND c.deleted_at IS NULL
	JOIN catalog_work_label wl ON wl.label_id = r.other_label_id
	WHERE r.relation IN (%d, %d)
	  AND %s
	  AND NOT EXISTS (SELECT 1 FROM catalog_work_label own
	    WHERE own.work_id = wl.work_id AND own.label_id = r.label_id)) e`,
	model.LabelRelationSubsidiary, model.LabelRelationImprint, editspec.NotSuppressedWorkLabelSQL("wl"))}

func (s *PublicService) labelRollupVia(ctx context.Context, seedID int64, workIDs []int64) (map[int64]dto.PublicLabelVia, error) {
	out := map[int64]dto.PublicLabelVia{}
	if seedID <= 0 || len(workIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkID  int64  `gorm:"column:work_id"`
		LabelID int64  `gorm:"column:label_id"`
		Name    string `gorm:"column:display_name"`
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (wl.work_id) wl.work_id, wl.label_id, c.display_name
		FROM catalog_work_label wl
		JOIN catalog_label c ON c.id = wl.label_id AND c.deleted_at IS NULL
		WHERE wl.work_id IN ?
		  AND wl.label_id IN (`+labelRollupChildren+`)
		  AND `+editspec.NotSuppressedWorkLabelSQL("wl")+`
		  AND NOT EXISTS (SELECT 1 FROM catalog_work_label own
		    WHERE own.work_id = wl.work_id AND own.label_id = ?)
		ORDER BY wl.work_id, wl.label_id`,
		workIDs, seedID, labelRollupRelations, seedID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	labelIDs := make([]int64, 0, len(rows))
	for _, r := range rows {
		labelIDs = append(labelIDs, r.LabelID)
	}
	loc, err := s.localizedFor(ctx, labelAliasSource, labelIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.WorkID] = dto.PublicLabelVia{ID: r.LabelID, DisplayName: r.Name, Localized: locOrEmpty(loc[r.LabelID])}
	}
	return out, nil
}

func (s *PublicService) imprintWorkCount(ctx context.Context, id int64, nsfw bool) (int, error) {
	counts, err := s.workCountsFor(ctx, labelImprintWorkEdge, []int64{id}, nsfw)
	if err != nil {
		return 0, err
	}
	return counts[id], nil
}
