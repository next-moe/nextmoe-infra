package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/editing"
	"api/internal/platform/settings/keys"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MergeService struct {
	db        *gorm.DB
	resolve   *ResolveService
	proposals *repository.ProposalRepository
	revisions *repository.RevisionRepository
	editReg   func() (*editing.Registry, error)
}

func NewMergeService(db *gorm.DB, resolve *ResolveService,
	proposals *repository.ProposalRepository, revisions *repository.RevisionRepository) *MergeService {
	return &MergeService{
		db: db, resolve: resolve, proposals: proposals, revisions: revisions,
		editReg: sync.OnceValues(func() (*editing.Registry, error) {
			reg := editing.NewRegistry()
			if err := editspec.RegisterAll(reg, db); err != nil {
				return nil, err
			}
			return reg, nil
		}),
	}
}

func (s *MergeService) ProposeMerge(ctx context.Context, entityType int16, sourceID, targetID, proposedBy int64, note string) (*model.CatalogMergeProposal, error) {
	return s.propose(ctx, entityType, sourceID, targetID, proposedBy, note, false)
}

// ProposeAbsorb files a merge that folds an edition work into its base work:
// the edition's work-level refs attach to the base as related and its imported
// rows are archived, not moved. Executing it refuses a source that holds a
// VNDB exact anchor, and so does filing it.
func (s *MergeService) ProposeAbsorb(ctx context.Context, sourceID, targetID, proposedBy int64, note string) (*model.CatalogMergeProposal, error) {
	return s.propose(ctx, model.EntityTypeWork, sourceID, targetID, proposedBy, note, true)
}

func (s *MergeService) CheckAbsorbable(ctx context.Context, sourceID int64) error {
	return assertAbsorbable(s.db.WithContext(ctx), sourceID)
}

func (s *MergeService) propose(ctx context.Context, entityType int16, sourceID, targetID, proposedBy int64, note string, absorb bool) (*model.CatalogMergeProposal, error) {
	src, _, err := s.resolve.Resolve(ctx, entityType, sourceID)
	if err != nil {
		return nil, err
	}
	dst, _, err := s.resolve.Resolve(ctx, entityType, targetID)
	if err != nil {
		return nil, err
	}
	if src == dst {
		return nil, ErrSameEntity
	}
	for _, id := range []int64{src, dst} {
		if err := assertEntityAlive(s.db.WithContext(ctx), entityType, id); err != nil {
			return nil, err
		}
	}
	resolution := datatypes.JSON(`{}`)
	if absorb {
		if err := assertAbsorbable(s.db.WithContext(ctx), src); err != nil {
			return nil, err
		}
		resolution = absorbResolution()
	}
	p := &model.CatalogMergeProposal{
		EntityType:      entityType,
		SourceEntityID:  src,
		TargetEntityID:  dst,
		Status:          model.ProposalStatusOpen,
		FieldResolution: resolution,
		ProposedBy:      proposedBy,
		Note:            note,
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		if isUniqueViolation(err, "uq_catalog_merge_proposal_open") {
			return nil, ErrDuplicateOpenProposal
		}
		return nil, err
	}
	return p, nil
}

func (s *MergeService) ApproveMerge(ctx context.Context, proposalID, approvedBy int64) error {
	return s.transition(ctx, proposalID, model.ProposalStatusOpen, func(tx *gorm.DB, p *model.CatalogMergeProposal) error {
		after := time.Now().Add(time.Duration(keys.CatalogMergeCoolingOffHours.Get()) * time.Hour)
		return tx.Model(p).Updates(map[string]any{
			"status":        model.ProposalStatusApproved,
			"execute_after": after,
			"note":          appendNote(p.Note, fmt.Sprintf("approved by user %d", approvedBy)),
		}).Error
	})
}

func (s *MergeService) RejectMerge(ctx context.Context, proposalID, rejectedBy int64, reason string) error {
	return s.transitionOneOf(ctx, proposalID, []int16{model.ProposalStatusOpen, model.ProposalStatusApproved}, func(tx *gorm.DB, p *model.CatalogMergeProposal) error {
		return tx.Model(p).Updates(map[string]any{
			"status": model.ProposalStatusRejected,
			"note":   appendNote(p.Note, fmt.Sprintf("rejected by user %d: %s", rejectedBy, reason)),
		}).Error
	})
}

func (s *MergeService) WithdrawMerge(ctx context.Context, proposalID, withdrawnBy int64) error {
	return s.transition(ctx, proposalID, model.ProposalStatusOpen, func(tx *gorm.DB, p *model.CatalogMergeProposal) error {
		return tx.Model(p).Updates(map[string]any{
			"status": model.ProposalStatusWithdrawn,
			"note":   appendNote(p.Note, fmt.Sprintf("withdrawn by user %d", withdrawnBy)),
		}).Error
	})
}

func (s *MergeService) transitionOneOf(ctx context.Context, proposalID int64, allowedStatuses []int16, fn func(*gorm.DB, *model.CatalogMergeProposal) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, proposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, proposalID)
		}
		for _, st := range allowedStatuses {
			if p.Status == st {
				return fn(tx, p)
			}
		}
		return fmt.Errorf("%w: proposal %d is in status %d", ErrProposalState, proposalID, p.Status)
	})
}

func (s *MergeService) transition(ctx context.Context, proposalID int64, requiredStatus int16, fn func(*gorm.DB, *model.CatalogMergeProposal) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, proposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, proposalID)
		}
		if p.Status != requiredStatus {
			return fmt.Errorf("%w: proposal %d is in status %d", ErrProposalState, proposalID, p.Status)
		}
		return fn(tx, p)
	})
}

func appendNote(existing, entry string) string {
	if existing == "" {
		return entry
	}
	return existing + "\n" + entry
}

func isUniqueViolation(err error, name string) bool {
	return err != nil && strings.Contains(err.Error(), name)
}

var ErrEntityNotLive = fmt.Errorf("catalog: entity is deleted or does not exist")

func assertEntityAlive(db *gorm.DB, entityType int16, id int64) error {
	table, ok := entityTableName(entityType)
	if !ok {
		return fmt.Errorf("catalog: unsupported entity type %d", entityType)
	}
	q := `SELECT count(*) FROM ` + table + ` WHERE id = ?`
	if entityType != model.EntityTypeCreditName {
		q += ` AND deleted_at IS NULL`
	}
	var count int64
	if err := db.Raw(q, id).Scan(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return fmt.Errorf("%w: entity type %d id %d", ErrEntityNotLive, entityType, id)
	}
	return nil
}

func entityTableName(entityType int16) (string, bool) {
	switch entityType {
	case model.EntityTypePerson:
		return "catalog_person", true
	case model.EntityTypeCreditName:
		return "catalog_credit_name", true
	case model.EntityTypeLabel:
		return "catalog_label", true
	case model.EntityTypeCharacter:
		return "catalog_character", true
	case model.EntityTypeWork:
		return "catalog_work", true
	}
	return "", false
}
