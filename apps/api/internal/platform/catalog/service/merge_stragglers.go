package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"

	"gorm.io/gorm"
)

type StragglerReport struct {
	Pairs     int              `json:"pairs"`
	Found     map[string]int64 `json:"found"`
	Repaired  int              `json:"repaired"`
	Orphaned  int              `json:"orphaned"`
	Uncovered map[string]int64 `json:"uncovered,omitempty"`
}

type stragglerPair struct {
	EntityType int16
	OldID      int64
	CurrentID  int64
	Absorb     bool
}

type stragglerRow struct {
	stragglerPair
	ref string
	n   int64
}

var errOrphanedRedirect = errors.New("redirect target is not alive")

// SweepStragglers finds rows that still name a merged-away id and replays the
// merge's own moves for that pair. A merge moves everything it can see, so a
// straggler is a row written after it: an importer that resolved the source id
// from a list loaded before the merge committed, or one that did not check. It
// fails when a replay leaves rows behind, which means a mergeRefs column has no
// statement that moves it.
func (s *MergeService) SweepStragglers(ctx context.Context) (StragglerReport, error) {
	rep := StragglerReport{Found: map[string]int64{}}
	rows, err := censusStragglers(s.db.WithContext(ctx))
	if err != nil {
		return rep, err
	}
	var pairs []stragglerPair
	seen := map[stragglerPair]bool{}
	for _, r := range rows {
		rep.Found[r.ref] += r.n
		if !seen[r.stragglerPair] {
			seen[r.stragglerPair] = true
			pairs = append(pairs, r.stragglerPair)
		}
	}
	rep.Pairs = len(pairs)
	if len(pairs) == 0 {
		return rep, nil
	}
	reg, err := s.editReg()
	if err != nil {
		return rep, fmt.Errorf("edit registry: %w", err)
	}
	orphaned := map[stragglerPair]bool{}
	for _, p := range pairs {
		err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := assertEntityAlive(tx, p.EntityType, p.CurrentID); err != nil {
				if errors.Is(err, ErrEntityNotLive) {
					return errOrphanedRedirect
				}
				return err
			}
			if err := repository.LockEntityRow(tx, p.EntityType, p.CurrentID); err != nil {
				return err
			}
			plan, err := stragglerPlan(tx, p)
			if err != nil {
				return err
			}
			touched, err := rehangEntity(tx, reg, plan)
			if err != nil {
				return fmt.Errorf("rehang: %w", err)
			}
			if p.EntityType == model.EntityTypeWork {
				touched = append(touched, p.CurrentID)
			}
			refs := moveStragglerRefs
			if plan.absorb {
				refs = absorbExternalRefs
			}
			if err := refs(tx, plan); err != nil {
				return fmt.Errorf("external refs: %w", err)
			}
			if err := repository.MergeUsage(tx, p.EntityType, p.OldID, p.CurrentID); err != nil {
				return fmt.Errorf("usage: %w", err)
			}
			return repository.TouchWorks(ctx, tx, touched)
		})
		switch {
		case errors.Is(err, errOrphanedRedirect):
			orphaned[p] = true
			slog.Warn("merge stragglers: redirect target is not alive",
				"entity_type", p.EntityType, "old_id", p.OldID, "current_id", p.CurrentID)
		case err != nil:
			return rep, fmt.Errorf("replay %d:%d→%d: %w", p.EntityType, p.OldID, p.CurrentID, err)
		default:
			rep.Repaired++
		}
	}
	rep.Orphaned = len(orphaned)
	left, err := censusStragglers(s.db.WithContext(ctx))
	if err != nil {
		return rep, err
	}
	for _, r := range left {
		if orphaned[r.stragglerPair] {
			continue
		}
		if rep.Uncovered == nil {
			rep.Uncovered = map[string]int64{}
		}
		rep.Uncovered[r.ref] += r.n
	}
	if len(rep.Uncovered) > 0 {
		return rep, fmt.Errorf("merge stragglers: replay left rows on retired ids: %v", rep.Uncovered)
	}
	return rep, nil
}

func censusStragglers(db *gorm.DB) ([]stragglerRow, error) {
	var out []stragglerRow
	for _, ref := range mergeRefs {
		var rows []struct {
			EntityType int16  `gorm:"column:entity_type"`
			OldID      int64  `gorm:"column:old_id"`
			CurrentID  int64  `gorm:"column:current_id"`
			Reason     string `gorm:"column:reason"`
			N          int64  `gorm:"column:n"`
		}
		q := fmt.Sprintf(`SELECT r.entity_type, r.old_id, r.current_id, r.reason, count(*) AS n
			FROM %s t JOIN catalog_redirect r ON r.entity_type = %s AND r.old_id = t.%s AND r.revoked_at IS NULL
			GROUP BY 1, 2, 3, 4`, ref.table, ref.typeSQL, ref.column)
		if err := db.Raw(q).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("census %s.%s: %w", ref.table, ref.column, err)
		}
		for _, r := range rows {
			out = append(out, stragglerRow{
				stragglerPair: stragglerPair{EntityType: r.EntityType, OldID: r.OldID, CurrentID: r.CurrentID,
					Absorb: redirectIsAbsorb(r.Reason)},
				ref: ref.table + "." + ref.column,
				n:   r.N,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].EntityType != out[j].EntityType {
			return out[i].EntityType < out[j].EntityType
		}
		return out[i].OldID < out[j].OldID
	})
	return out, nil
}

func stragglerPlan(tx *gorm.DB, p stragglerPair) (mergePlan, error) {
	plan := mergePlan{entityType: p.EntityType, src: p.OldID, dst: p.CurrentID, absorb: p.Absorb}
	err := tx.Raw(`SELECT COALESCE((SELECT id FROM catalog_merge_proposal
		WHERE entity_type = ? AND source_entity_id = ? AND status = ?
		ORDER BY executed_at DESC NULLS LAST, id DESC LIMIT 1), 0)`,
		p.EntityType, p.OldID, model.ProposalStatusExecuted).Scan(&plan.proposalID).Error
	return plan, err
}
