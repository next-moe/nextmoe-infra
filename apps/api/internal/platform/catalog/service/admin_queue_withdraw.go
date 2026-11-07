package service

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type WorkAcceptWithdrawal struct {
	ProposalID int64
	AID, BID   int64
	Actor      int64
	// ToStatus is where the candidate goes back to: pending to be planned
	// again, needs_manual to wait for a person.
	ToStatus int16
	Reason   string
}

// WithdrawWorkAccept takes back an accepted work candidate whose merge has not
// executed: the proposal is rejected and the candidate returns to ToStatus, in
// one transaction together with also.
func (s *AdminQueueService) WithdrawWorkAccept(ctx context.Context, w WorkAcceptWithdrawal, also func(tx *gorm.DB) error) error {
	if w.ToStatus != model.CandidateStatusPending && w.ToStatus != model.CandidateStatusNeedsManual {
		return fmt.Errorf("%w: withdraw to status %d", ErrProposalState, w.ToStatus)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, w.ProposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, w.ProposalID)
		}
		if p.EntityType != model.EntityTypeWork ||
			(p.Status != model.ProposalStatusOpen && p.Status != model.ProposalStatusApproved) {
			return fmt.Errorf("%w: proposal %d is in status %d", ErrProposalState, w.ProposalID, p.Status)
		}
		lo, hi := min(w.AID, w.BID), max(w.AID, w.BID)
		if min(p.SourceEntityID, p.TargetEntityID) != lo || max(p.SourceEntityID, p.TargetEntityID) != hi {
			return fmt.Errorf("%w: proposal %d is not the pair (%d, %d)", ErrProposalState, w.ProposalID, lo, hi)
		}
		if err := tx.Model(p).Updates(map[string]any{
			"status": model.ProposalStatusRejected,
			"note":   appendNote(p.Note, fmt.Sprintf("rejected by user %d: %s", w.Actor, w.Reason)),
		}).Error; err != nil {
			return err
		}
		res := tx.Model(&model.CatalogMatchCandidate{}).
			Where("entity_type = ? AND a_id = ? AND b_id = ? AND status = ?",
				model.EntityTypeWork, lo, hi, model.CandidateStatusAccepted).
			Updates(map[string]any{"status": w.ToStatus, "decided_by": w.Actor, "decided_at": time.Now()})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("%w: candidate (%d, %d) is not accepted", ErrProposalState, lo, hi)
		}
		if also != nil {
			return also(tx)
		}
		return nil
	})
}
