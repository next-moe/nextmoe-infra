package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// OriginEditor marks merge proposals filed through the editor's v2 faces; only
// those are executed by the catalog-merge-requests job once their cooling-off
// ends. Batch tools keep executing their own proposals.
const OriginEditor = "editor"

var ErrCrossMedium = errors.New("catalog merge: the two works belong to different mediums")

type MergeRequestFilter struct {
	ProposedBy *int64
	Status     *int16
	EntityType *int16
	Origin     string
	BeforeID   int64
	Limit      int
}

func (s *MergeService) RequestMerge(ctx context.Context, entityType int16, sourceID, targetID, proposedBy int64, reason string) (*model.CatalogMergeProposal, error) {
	if entityType == model.EntityTypeWork {
		var mediums []int16
		if err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT medium_id FROM catalog_work WHERE id IN (?, ?)`,
			sourceID, targetID).Scan(&mediums).Error; err != nil {
			return nil, err
		}
		if len(mediums) > 1 {
			return nil, ErrCrossMedium
		}
	}
	return s.propose(ctx, entityType, sourceID, targetID, proposedBy, reason, false, OriginEditor)
}

// ListMergeRequests pages newest first by id; BeforeID is the keyset cursor.
func (s *MergeService) ListMergeRequests(ctx context.Context, f MergeRequestFilter) ([]model.CatalogMergeProposal, error) {
	q := s.db.WithContext(ctx).Model(&model.CatalogMergeProposal{})
	if f.ProposedBy != nil {
		q = q.Where("proposed_by = ?", *f.ProposedBy)
	}
	if f.Status != nil {
		q = q.Where("status = ?", *f.Status)
	}
	if f.EntityType != nil {
		q = q.Where("entity_type = ?", *f.EntityType)
	}
	if f.Origin != "" {
		q = q.Where("origin = ?", f.Origin)
	}
	if f.BeforeID > 0 {
		q = q.Where("id < ?", f.BeforeID)
	}
	limit := f.Limit
	if limit <= 0 || limit > 101 {
		limit = 50
	}
	var rows []model.CatalogMergeProposal
	err := q.Order("id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *MergeService) GetMergeRequest(ctx context.Context, id int64) (*model.CatalogMergeProposal, error) {
	var p model.CatalogMergeProposal
	err := s.db.WithContext(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("%w: proposal %d", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *MergeService) CountMergeRequestsSince(ctx context.Context, proposedBy int64, since time.Time) (int64, error) {
	var n int64
	err := s.db.WithContext(ctx).Model(&model.CatalogMergeProposal{}).
		Where("proposed_by = ? AND origin = ? AND proposed_at >= ?", proposedBy, OriginEditor, since).Count(&n).Error
	return n, err
}

type DueMergeReport struct {
	Due, Executed, Failed int
}

// ExecuteDueRequests runs every approved editor-filed merge whose cooling-off
// has ended. A failure (an entity merged away meanwhile, a hidden survivor)
// leaves that proposal approved for a reviewer to reject.
func (s *MergeService) ExecuteDueRequests(ctx context.Context, actorID int64) (DueMergeReport, error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Model(&model.CatalogMergeProposal{}).
		Where("status = ? AND origin = ? AND execute_after <= ?", model.ProposalStatusApproved, OriginEditor, time.Now()).
		Order("id").Pluck("id", &ids).Error; err != nil {
		return DueMergeReport{}, err
	}
	rep := DueMergeReport{Due: len(ids)}
	var firstErr error
	for _, id := range ids {
		if err := s.ExecuteMerge(ctx, id, &actorID); err != nil {
			rep.Failed++
			if firstErr == nil {
				firstErr = fmt.Errorf("execute merge proposal %d: %w", id, err)
			}
			continue
		}
		rep.Executed++
	}
	return rep, firstErr
}
