package service

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// holdingCandidateStatuses is the release rule's own list: deferred does not
// hold a work. decidableCandidateStatuses still includes deferred for the
// console. Counting deferred as open is what left a quarantined work whose
// candidate was one of the 164 deferred rows on 2026-09-18 hidden forever.
var holdingCandidateStatuses = []int16{
	model.CandidateStatusPending,
	model.CandidateStatusNeedsManual,
}

var holdingProposalStatuses = []int16{
	model.ProposalStatusOpen,
	model.ProposalStatusApproved,
}

func (s *AdminQueueService) rejectWorkCandidate(ctx context.Context, d CandidateDecision) (*CandidateOutcome, error) {
	var released []int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := s.flipCandidate(tx, d, model.CandidateStatusRejected)
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("%w: candidate decided concurrently", ErrProposalState)
		}
		out, err := releaseQuarantinedPair(tx, d.AID, d.BID, d.DecidedBy, d.Note)
		if err != nil {
			return err
		}
		released = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CandidateOutcome{Released: released}, nil
}

func (s *AdminQueueService) deferWorkCandidate(ctx context.Context, d CandidateDecision) (*CandidateOutcome, error) {
	var released []int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, err := s.flipCandidate(tx, d, model.CandidateStatusDeferred)
		if err != nil {
			return err
		}
		if rows != 1 {
			return fmt.Errorf("%w: candidate decided concurrently", ErrProposalState)
		}
		out, err := releaseQuarantinedPair(tx, d.AID, d.BID, d.DecidedBy, d.Note)
		if err != nil {
			return err
		}
		released = out
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &CandidateOutcome{Released: released}, nil
}

func (s *AdminQueueService) ReleaseWork(ctx context.Context, workID, actor int64, note string) error {
	var row struct {
		ID        int64
		Status    int16
		DeletedAt *time.Time `gorm:"column:deleted_at"`
	}
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id, status, deleted_at FROM catalog_work WHERE id = ?`, workID,
	).Scan(&row).Error; err != nil {
		return err
	}
	if row.ID == 0 {
		return fmt.Errorf("%w: work %d", ErrNotFound, workID)
	}
	if row.DeletedAt != nil || row.Status != model.WorkStatusQuarantine {
		return fmt.Errorf("%w: work %d is not quarantined", ErrProposalState, workID)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return releaseWorkTx(tx, workID, actor, note)
	})
}

func releaseQuarantinedPair(tx *gorm.DB, aID, bID, actor int64, note string) ([]int64, error) {
	var released []int64
	for _, id := range []int64{aID, bID} {
		ok, err := shouldReleaseQuarantine(tx, id)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := releaseWorkTx(tx, id, actor, note); err != nil {
			return nil, err
		}
		released = append(released, id)
	}
	return released, nil
}

func shouldReleaseQuarantine(tx *gorm.DB, id int64) (bool, error) {
	var row struct {
		ID     int64
		Status int16
	}
	if err := tx.Raw(
		`SELECT id, status FROM catalog_work WHERE id = ? AND deleted_at IS NULL`, id,
	).Scan(&row).Error; err != nil {
		return false, err
	}
	if row.ID == 0 || row.Status != model.WorkStatusQuarantine {
		return false, nil
	}
	var open int64
	if err := tx.Raw(
		`SELECT count(*) FROM catalog_match_candidate
		 WHERE entity_type = ? AND status IN ? AND (a_id = ? OR b_id = ?)`,
		model.EntityTypeWork, holdingCandidateStatuses, id, id,
	).Scan(&open).Error; err != nil {
		return false, err
	}
	if open > 0 {
		return false, nil
	}
	var held int64
	if err := tx.Raw(
		`SELECT count(*) FROM catalog_merge_proposal
		 WHERE entity_type = ? AND status IN ? AND (source_entity_id = ? OR target_entity_id = ?)`,
		model.EntityTypeWork, holdingProposalStatuses, id, id,
	).Scan(&held).Error; err != nil {
		return false, err
	}
	return held == 0, nil
}

func releaseWorkTx(tx *gorm.DB, id, actor int64, note string) error {
	res := tx.Exec(
		`UPDATE catalog_work SET status = ?, updated_at = now() WHERE id = ? AND status = ? AND deleted_at IS NULL`,
		model.WorkStatusLive, id, model.WorkStatusQuarantine,
	)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: work %d is not quarantined", ErrProposalState, id)
	}
	snap, err := takeSnapshot(tx, model.EntityTypeWork, id)
	if err != nil {
		return fmt.Errorf("snapshot work %d: %w", id, err)
	}
	changed, err := marshalChangedFields(map[string]any{"status": model.WorkStatusLive})
	if err != nil {
		return err
	}
	actorID := actor
	return writeRevision(tx, model.EntityTypeWork, id, model.RevisionActionUpdated, snap, changed, &actorID, note)
}

func (s *AdminQueueService) ReleaseUnheldQuarantine(ctx context.Context, actor int64, note string, run bool) (quarantined, held, released int, err error) {
	var ids []int64
	if err := s.db.WithContext(ctx).Raw(
		`SELECT id FROM catalog_work WHERE status = ? AND deleted_at IS NULL ORDER BY id`,
		model.WorkStatusQuarantine,
	).Scan(&ids).Error; err != nil {
		return 0, 0, 0, err
	}
	for _, id := range ids {
		quarantined++
		ok, err := shouldReleaseQuarantine(s.db.WithContext(ctx), id)
		if err != nil {
			return quarantined, held, released, err
		}
		if !ok {
			held++
			continue
		}
		if run {
			if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				return releaseWorkTx(tx, id, actor, note)
			}); err != nil {
				return quarantined, held, released, err
			}
		}
		released++
	}
	return quarantined, held, released, nil
}
