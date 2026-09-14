package service

import (
	"context"

	"api/internal/platform/catalog/model"
)

const workRefEvidenceLimit = 6

type CandidateBucketCount struct {
	EntityType int16 `gorm:"column:entity_type" json:"entity_type"`
	Status     int16 `gorm:"column:status" json:"status"`
	Count      int64 `gorm:"column:count" json:"count"`
}

type ProbableRefBucketCount struct {
	EntityType int16 `gorm:"column:entity_type" json:"entity_type"`
	Count      int64 `gorm:"column:count" json:"count"`
}

type QueueSummary struct {
	Candidates   []CandidateBucketCount   `json:"candidates"`
	ProbableRefs []ProbableRefBucketCount `json:"probable_refs"`
}

func (s *AdminQueueService) QueueSummary(ctx context.Context) (QueueSummary, error) {
	db := s.db.WithContext(ctx)

	var candidates []CandidateBucketCount
	if err := db.Raw(`SELECT entity_type, status, count(*) AS count
	                  FROM catalog_match_candidate
	                  GROUP BY entity_type, status
	                  ORDER BY entity_type, status`).Scan(&candidates).Error; err != nil {
		return QueueSummary{}, err
	}

	// Scope copied from ListProbableRefs; it deliberately does not filter
	// dead_at, so the chip totals match the queue the reviewer pages through.
	var refs []ProbableRefBucketCount
	if err := db.Raw(`SELECT entity_type, count(*) AS count
	                  FROM catalog_external_ref
	                  WHERE link_kind = ? AND verified_at IS NULL AND `+ExactSlotFreeSQL+`
	                  GROUP BY entity_type
	                  ORDER BY entity_type`, model.LinkKindProbable).Scan(&refs).Error; err != nil {
		return QueueSummary{}, err
	}

	if candidates == nil {
		candidates = []CandidateBucketCount{}
	}
	if refs == nil {
		refs = []ProbableRefBucketCount{}
	}
	return QueueSummary{Candidates: candidates, ProbableRefs: refs}, nil
}

type EntityRefSummary struct {
	SourceID   int16  `gorm:"column:source_id" json:"source_id"`
	ExternalID string `gorm:"column:external_id" json:"external_id"`
	LinkKind   int16  `gorm:"column:link_kind" json:"link_kind"`
}

func (s *AdminQueueService) enrichWorkContext(ctx context.Context, items []CandidateItem) {
	var ids []int64
	for _, it := range items {
		if it.EntityType == model.EntityTypeWork {
			ids = append(ids, it.AID, it.BID)
		}
	}
	if len(ids) == 0 {
		return
	}
	db := s.db.WithContext(ctx)

	type workRow struct {
		ID            int64   `gorm:"column:id"`
		MediumID      int16   `gorm:"column:medium_id"`
		OLang         string  `gorm:"column:olang"`
		ContentRating int16   `gorm:"column:content_rating"`
		Site          *string `gorm:"column:site"`
		ClaimState    *int16  `gorm:"column:claim_state"`
	}
	works := map[int64]workRow{}
	var workRows []workRow
	if err := db.Raw(`SELECT id, medium_id, olang, content_rating, site, claim_state
		FROM catalog_work WHERE id IN ?`, ids).Scan(&workRows).Error; err == nil {
		for _, r := range workRows {
			works[r.ID] = r
		}
	}

	years := map[int64]int16{}
	var yearRows []struct {
		WorkID int64 `gorm:"column:work_id"`
		Year   int16 `gorm:"column:year"`
	}
	if err := db.Raw(`SELECT work_id, min(released_y) AS year FROM catalog_release
		WHERE work_id IN ? AND released_y IS NOT NULL AND deleted_at IS NULL
		GROUP BY work_id`, ids).Scan(&yearRows).Error; err == nil {
		for _, r := range yearRows {
			years[r.WorkID] = r.Year
		}
	}

	refs := map[int64][]EntityRefSummary{}
	var refRows []struct {
		EntityID   int64  `gorm:"column:entity_id"`
		SourceID   int16  `gorm:"column:source_id"`
		ExternalID string `gorm:"column:external_id"`
		LinkKind   int16  `gorm:"column:link_kind"`
	}
	if err := db.Raw(`SELECT entity_id, source_id, external_id, link_kind FROM (
			SELECT entity_id, source_id, external_id, link_kind,
			       row_number() OVER (PARTITION BY entity_id
			                          ORDER BY link_kind, source_id, external_id) AS rn
			FROM catalog_external_ref
			WHERE entity_type = ? AND entity_id IN ? AND link_kind IN ? AND dead_at IS NULL
		) ranked WHERE rn <= ?`,
		model.EntityTypeWork, ids,
		[]int16{model.LinkKindExact, model.LinkKindProbable},
		workRefEvidenceLimit).Scan(&refRows).Error; err == nil {
		for _, r := range refRows {
			refs[r.EntityID] = append(refs[r.EntityID], EntityRefSummary{
				SourceID: r.SourceID, ExternalID: r.ExternalID, LinkKind: r.LinkKind,
			})
		}
	}

	fill := func(sum *EntitySummary) {
		if w, ok := works[sum.ID]; ok {
			medium, rating := w.MediumID, w.ContentRating
			sum.MediumID = &medium
			sum.ContentRating = &rating
			sum.OLang = w.OLang
			sum.ClaimState = w.ClaimState
			if w.Site != nil {
				sum.Site = *w.Site
			}
		}
		if y, ok := years[sum.ID]; ok {
			sum.ReleaseYear = &y
		}
		sum.Refs = refs[sum.ID]
	}
	for i := range items {
		if items[i].EntityType == model.EntityTypeWork {
			fill(&items[i].A)
			fill(&items[i].B)
		}
	}
}
