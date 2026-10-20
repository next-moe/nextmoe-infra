package service

import (
	"context"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

type WorkRelationRow struct {
	Key           string  `gorm:"column:key"`
	Phrase        string  `gorm:"column:phrase"`
	OtherID       int64   `gorm:"column:other_id"`
	DisplayName   string  `gorm:"column:display_name"`
	MediumID      int16   `gorm:"column:medium_id"`
	ContentRating int16   `gorm:"column:content_rating"`
	Status        int16   `gorm:"column:status"`
	Site          *string `gorm:"column:site"`
	ProductWorkID *int64  `gorm:"column:product_work_id"`
	ClaimState    *int16  `gorm:"column:claim_state"`
}

func (s *ReadService) loadWorkRelations(ctx context.Context, workID int64) ([]WorkRelationRow, error) {
	var rows []WorkRelationRow
	if err := s.db.WithContext(ctx).Raw(`
		SELECT rt.key, rt.forward_phrase AS phrase, w.id AS other_id, w.display_name,
		       w.medium_id, w.content_rating, w.status, w.site, w.product_work_id, w.claim_state
		FROM catalog_work_relation r
		JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
		JOIN catalog_work w ON w.id = r.b_work_id AND w.deleted_at IS NULL AND w.status = ?
		WHERE r.a_work_id = ? AND `+editspec.NotSuppressedWorkRelationSQL("r")+`
		UNION ALL
		SELECT rt.key,
		       CASE WHEN rt.is_symmetric THEN rt.forward_phrase ELSE rt.reverse_phrase END AS phrase,
		       w.id AS other_id, w.display_name,
		       w.medium_id, w.content_rating, w.status, w.site, w.product_work_id, w.claim_state
		FROM catalog_work_relation r
		JOIN catalog_relation_type rt ON rt.id = r.relation_type_id
		JOIN catalog_work w ON w.id = r.a_work_id AND w.deleted_at IS NULL AND w.status = ?
		WHERE r.b_work_id = ? AND `+editspec.NotSuppressedWorkRelationSQL("r")+`
		ORDER BY key, other_id`, model.WorkStatusLive, workID, model.WorkStatusLive, workID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

const seriesRelationTypeID = 7

type SeriesSiblingRow struct {
	WorkID        int64   `gorm:"column:work_id"`
	DisplayName   string  `gorm:"column:display_name"`
	MediumID      int16   `gorm:"column:medium_id"`
	ContentRating int16   `gorm:"column:content_rating"`
	Status        int16   `gorm:"column:status"`
	Site          *string `gorm:"column:site"`
	ProductWorkID *int64  `gorm:"column:product_work_id"`
	ClaimState    *int16  `gorm:"column:claim_state"`
}

// The closure walk and the row fetch are ONE statement on purpose, and there is
// no `EXISTS(series edge?)` pre-probe in front of them. The probe looks like it
// should skip the walk for the ~98% of works with no series edge; measured on
// the prod snapshot it does not, because the recursive term for an edgeless
// work is the same two index-only scans the probe would run (CTE 0.064 ms,
// probe 0.059 ms). It is a round-trip that saves no work.
func (s *ReadService) loadSeriesSiblings(ctx context.Context, workID int64) ([]SeriesSiblingRow, error) {
	var rows []SeriesSiblingRow
	if err := s.db.WithContext(ctx).Raw(`
		WITH RECURSIVE reach(node) AS (
			SELECT ?::bigint
			UNION
			SELECT x.other FROM reach rc CROSS JOIN LATERAL (
				SELECT r.b_work_id AS other FROM catalog_work_relation r
				WHERE r.relation_type_id = ? AND r.a_work_id = rc.node AND `+editspec.NotSuppressedWorkRelationSQL("r")+`
				UNION ALL
				SELECT r.a_work_id FROM catalog_work_relation r
				WHERE r.relation_type_id = ? AND r.b_work_id = rc.node AND `+editspec.NotSuppressedWorkRelationSQL("r")+`
			) x
		)
		SELECT w.id AS work_id, w.display_name, w.medium_id, w.content_rating, w.status, w.site, w.product_work_id, w.claim_state
		FROM catalog_work w
		WHERE w.id IN (SELECT node FROM reach WHERE node <> ?) AND w.deleted_at IS NULL AND w.status = ?
		ORDER BY w.id`,
		workID, seriesRelationTypeID, seriesRelationTypeID, workID, model.WorkStatusLive).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
