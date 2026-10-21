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
	OriginSite string
	BeforeID   int64
	Limit      int
}

func (s *MergeService) RequestMerge(ctx context.Context, entityType int16, sourceID, targetID, proposedBy int64, reason string, choices MergeChoices) (*model.CatalogMergeProposal, error) {
	if err := validateChoices(s.db.WithContext(ctx), entityType, sourceID, choices); err != nil {
		return nil, err
	}
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
	return s.proposeWith(ctx, entityType, sourceID, targetID, proposedBy, reason, OriginEditor, choicesDoc("", choices))
}

// AmendMergeRequest replaces the requester's choices with a reviewer's while
// the request has not run; the note records who changed them.
func (s *MergeService) AmendMergeRequest(ctx context.Context, proposalID, amendedBy int64, choices MergeChoices) error {
	return s.transitionOneOf(ctx, proposalID, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved}, func(tx *gorm.DB, p *model.CatalogMergeProposal) error {
		if err := validateChoices(tx, p.EntityType, p.SourceEntityID, choices); err != nil {
			return err
		}
		prior, err := parseMergeDoc(p.FieldResolution)
		if err != nil {
			return err
		}
		raw, err := choicesDoc(prior.mode, choices).encode()
		if err != nil {
			return err
		}
		now := time.Now()
		return tx.Model(p).Updates(map[string]any{
			"field_resolution": raw,
			"amended_by":       amendedBy,
			"amended_at":       now,
			"note":             appendNote(p.Note, fmt.Sprintf("choices amended by user %d", amendedBy)),
		}).Error
	})
}

// EntityNames reads display names for ids of one family, retired rows
// included, so a merge request keeps naming the entry it folded away.
func (s *MergeService) EntityNames(ctx context.Context, entityType int16, ids []int64) (map[int64]string, error) {
	table, ok := entityTableName(entityType)
	if !ok || len(ids) == 0 {
		return map[int64]string{}, nil
	}
	nameCol := "display_name"
	if entityType == model.EntityTypeCreditName {
		nameCol = "name"
	}
	var rows []struct {
		ID   int64
		Name string
	}
	if err := s.db.WithContext(ctx).Raw(fmt.Sprintf(`SELECT id, %s AS name FROM %s WHERE id IN ?`, nameCol, table), ids).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

// MergeChoicesOf reads the choices stored on a request.
func MergeChoicesOf(p *model.CatalogMergeProposal) (MergeChoices, error) {
	doc, err := parseMergeDoc(p.FieldResolution)
	return doc.MergeChoices, err
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
	if f.OriginSite != "" {
		q = q.Where("origin_site = ?", f.OriginSite)
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
