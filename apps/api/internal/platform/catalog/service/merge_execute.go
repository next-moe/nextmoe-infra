package service

import (
	"context"
	"fmt"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

func (s *MergeService) ExecuteMerge(ctx context.Context, proposalID int64, executedBy *int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, proposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, proposalID)
		}
		if p.Status != model.ProposalStatusApproved {
			return fmt.Errorf("%w: proposal %d is in status %d, want approved", ErrProposalState, proposalID, p.Status)
		}
		if p.ExecuteAfter == nil || time.Now().Before(*p.ExecuteAfter) {
			return fmt.Errorf("%w: proposal %d executable after %v", ErrCoolingOff, proposalID, p.ExecuteAfter)
		}
		plan, err := planFromProposal(p)
		if err != nil {
			return err
		}
		et, src, dst := p.EntityType, p.SourceEntityID, p.TargetEntityID

		for _, id := range []int64{src, dst} {
			if err := assertEntityAlive(tx, et, id); err != nil {
				return err
			}
		}

		first, second := src, dst
		if second < first {
			first, second = second, first
		}
		if err := repository.LockEntityRow(tx, et, first); err != nil {
			return err
		}
		if err := repository.LockEntityRow(tx, et, second); err != nil {
			return err
		}
		if plan.absorb {
			if err := assertAbsorbable(tx, src); err != nil {
				return err
			}
		}

		sourceSnap, err := takeSnapshot(tx, et, src)
		if err != nil {
			return fmt.Errorf("snapshot source: %w", err)
		}

		changed, err := applySurvivorship(tx, et, src, dst, p.FieldResolution)
		if err != nil {
			return fmt.Errorf("survivorship: %w", err)
		}

		reg, err := s.editReg()
		if err != nil {
			return fmt.Errorf("edit registry: %w", err)
		}
		touched, err := rehangEntity(tx, reg, plan)
		if err != nil {
			return fmt.Errorf("rehang: %w", err)
		}
		if et == model.EntityTypeWork {
			touched = append(touched, dst)
		}

		refs := mergeExternalRefs
		if plan.absorb {
			refs = absorbExternalRefs
		}
		if err := refs(tx, plan); err != nil {
			return fmt.Errorf("external refs: %w", err)
		}

		if err := repository.MergeUsage(tx, et, src, dst); err != nil {
			return fmt.Errorf("usage: %w", err)
		}

		if err := repository.FlattenRedirectsTo(tx, et, src, dst); err != nil {
			return err
		}
		if err := repository.InsertRedirect(tx, et, src, dst, executedBy, redirectReason(plan)); err != nil {
			return err
		}

		note := fmt.Sprintf("merge proposal %d", p.ID)
		if err := writeRevision(tx, et, src, model.RevisionActionMergedSource, sourceSnap, nil, executedBy, note); err != nil {
			return err
		}
		if et == model.EntityTypeWork {
			res := tx.Exec(`UPDATE catalog_work SET status = ? WHERE id = ? AND status = ?`,
				model.WorkStatusLive, dst, model.WorkStatusQuarantine)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected > 0 {
				if changed == nil {
					changed = map[string]any{}
				}
				changed["status"] = model.WorkStatusLive
			}
		}
		targetSnap, err := takeSnapshot(tx, et, dst)
		if err != nil {
			return fmt.Errorf("snapshot target: %w", err)
		}
		changedJSON, err := marshalChangedFields(changed)
		if err != nil {
			return err
		}
		if err := writeRevision(tx, et, dst, model.RevisionActionMergedTarget, targetSnap, changedJSON, executedBy, note); err != nil {
			return err
		}

		a, b := first, second
		if err := tx.Exec(`DELETE FROM catalog_match_candidate WHERE entity_type = ? AND a_id = ? AND b_id = ?`,
			et, a, b).Error; err != nil {
			return err
		}

		if err := retireSource(tx, et, src); err != nil {
			return fmt.Errorf("retire source: %w", err)
		}

		if err := repository.TouchWorks(ctx, tx, touched); err != nil {
			return fmt.Errorf("touch works: %w", err)
		}

		return tx.Model(p).Updates(map[string]any{
			"status":      model.ProposalStatusExecuted,
			"executed_at": time.Now(),
		}).Error
	})
}

func retireSource(tx *gorm.DB, entityType int16, src int64) error {
	switch entityType {
	case model.EntityTypePerson:
		return tx.Delete(&model.CatalogPerson{}, src).Error
	case model.EntityTypeCreditName:
		return tx.Exec(`DELETE FROM catalog_credit_name WHERE id = ?`, src).Error
	case model.EntityTypeLabel:
		return tx.Delete(&model.CatalogLabel{}, src).Error
	case model.EntityTypeCharacter:
		return tx.Delete(&model.CatalogCharacter{}, src).Error
	case model.EntityTypeWork:
		// The updated_at bump is what puts the retired id on the changes feed as
		// a gone entry. GORM's soft delete below writes deleted_at only, so
		// without it the merge source left the public population without ever
		// surfacing, and downstream mirrors kept the dead id until a full sweep.
		if err := tx.Exec(`UPDATE catalog_work SET status = ?, site = NULL, product_work_id = NULL, updated_at = now() WHERE id = ?`,
			model.WorkStatusMerged, src).Error; err != nil {
			return err
		}
		return tx.Delete(&model.CatalogWork{}, src).Error
	}
	return fmt.Errorf("catalog merge: unsupported entity type %d", entityType)
}
