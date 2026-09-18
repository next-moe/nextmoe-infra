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
		touched, err := rehangEntity(tx, reg, et, src, dst)
		if err != nil {
			return fmt.Errorf("rehang: %w", err)
		}
		if et == model.EntityTypeWork {
			touched = append(touched, dst)
		}

		if err := mergeExternalRefs(tx, et, src, dst); err != nil {
			return fmt.Errorf("external refs: %w", err)
		}

		if err := repository.MergeUsage(tx, et, src, dst); err != nil {
			return fmt.Errorf("usage: %w", err)
		}

		if err := repository.FlattenRedirectsTo(tx, et, src, dst); err != nil {
			return err
		}
		if err := repository.InsertRedirect(tx, et, src, dst, executedBy, fmt.Sprintf("merge proposal %d", p.ID)); err != nil {
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

func mergeExternalRefs(tx *gorm.DB, entityType int16, src, dst int64) error {
	var keep []editionKeep
	if entityType == model.EntityTypeWork {
		if err := tx.Raw(`SELECT source_id, external_id FROM catalog_external_ref
			WHERE entity_type = ? AND entity_id = ? AND link_kind = ? AND source_id IN ?`,
			entityType, dst, model.LinkKindExact, model.EditionSplittingSourceIDs).Scan(&keep).Error; err != nil {
			return err
		}
	}

	stmts := []mergeStmt{
		{`UPDATE catalog_external_ref r SET entity_id = ? WHERE r.entity_type = ? AND r.entity_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_external_ref t
		                     WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                       AND t.source_id = r.source_id AND t.external_id = r.external_id)`,
			[]any{dst, entityType, src, dst}, false},
		{`DELETE FROM catalog_external_ref WHERE entity_type = ? AND entity_id = ?`, []any{entityType, src}, false},
	}
	if _, err := execAll(tx, stmts); err != nil {
		return err
	}

	if entityType == model.EntityTypeWork {
		if err := relateEditionSplitExacts(tx, entityType, dst, keep); err != nil {
			return err
		}
	}

	demote := mergeStmt{`UPDATE catalog_external_ref SET link_kind = ?
		   WHERE entity_type = ? AND entity_id = ? AND link_kind = ?
		     AND source_id NOT IN (SELECT id FROM catalog_source WHERE key IN ?)
		     AND source_id IN (SELECT source_id FROM catalog_external_ref
		                        WHERE entity_type = ? AND entity_id = ? AND link_kind = ?
		                        GROUP BY source_id HAVING COUNT(DISTINCT external_id) > 1)`,
		[]any{model.LinkKindProbable, entityType, dst, model.LinkKindExact, curatedSourceKeys,
			entityType, dst, model.LinkKindExact}, false}
	if entityType == model.EntityTypeWork {
		demote.sql += ` AND source_id NOT IN ?`
		demote.args = append(demote.args, model.EditionSplittingSourceIDs)
	}
	_, err := execAll(tx, []mergeStmt{demote})
	return err
}

type editionKeep struct {
	SourceID   int16  `gorm:"column:source_id"`
	ExternalID string `gorm:"column:external_id"`
}

// relateEditionSplitExacts keeps one exact id per edition-splitting source; see
// model.EditionSplittingSourceIDs for why the others become related.
func relateEditionSplitExacts(tx *gorm.DB, entityType int16, dst int64, keep []editionKeep) error {
	keepBy := map[int16][]string{}
	for _, k := range keep {
		keepBy[k.SourceID] = append(keepBy[k.SourceID], k.ExternalID)
	}

	var conflicts []int16
	if err := tx.Raw(`SELECT source_id FROM catalog_external_ref
		WHERE entity_type = ? AND entity_id = ? AND link_kind = ? AND source_id IN ?
		GROUP BY source_id HAVING COUNT(DISTINCT external_id) > 1`,
		entityType, dst, model.LinkKindExact, model.EditionSplittingSourceIDs).Scan(&conflicts).Error; err != nil {
		return err
	}
	for _, sourceID := range conflicts {
		if ids := keepBy[sourceID]; len(ids) > 0 {
			if err := tx.Exec(`UPDATE catalog_external_ref SET link_kind = ?
				WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND link_kind = ? AND external_id NOT IN ?`,
				model.LinkKindRelated, entityType, dst, sourceID, model.LinkKindExact, ids).Error; err != nil {
				return err
			}
			continue
		}
		var keeper string
		if err := tx.Raw(`SELECT external_id FROM catalog_external_ref
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND link_kind = ?
			ORDER BY length(external_id), external_id LIMIT 1`,
			entityType, dst, sourceID, model.LinkKindExact).Scan(&keeper).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE catalog_external_ref SET link_kind = ?
			WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND link_kind = ? AND external_id <> ?`,
			model.LinkKindRelated, entityType, dst, sourceID, model.LinkKindExact, keeper).Error; err != nil {
			return err
		}
	}
	return nil
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
