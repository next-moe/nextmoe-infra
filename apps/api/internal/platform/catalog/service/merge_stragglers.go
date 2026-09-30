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
			touched, err := rehangEntity(tx, reg, p.EntityType, p.OldID, p.CurrentID)
			if err != nil {
				return fmt.Errorf("rehang: %w", err)
			}
			if p.EntityType == model.EntityTypeWork {
				touched = append(touched, p.CurrentID)
			}
			if err := moveStragglerRefs(tx, p.EntityType, p.OldID, p.CurrentID); err != nil {
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
			EntityType int16 `gorm:"column:entity_type"`
			OldID      int64 `gorm:"column:old_id"`
			CurrentID  int64 `gorm:"column:current_id"`
			N          int64 `gorm:"column:n"`
		}
		q := fmt.Sprintf(`SELECT r.entity_type, r.old_id, r.current_id, count(*) AS n
			FROM %s t JOIN catalog_redirect r ON r.entity_type = %s AND r.old_id = t.%s
			GROUP BY 1, 2, 3`, ref.table, ref.typeSQL, ref.column)
		if err := db.Raw(q).Scan(&rows).Error; err != nil {
			return nil, fmt.Errorf("census %s.%s: %w", ref.table, ref.column, err)
		}
		for _, r := range rows {
			out = append(out, stragglerRow{
				stragglerPair: stragglerPair{EntityType: r.EntityType, OldID: r.OldID, CurrentID: r.CurrentID},
				ref:           ref.table + "." + ref.column,
				n:             r.N,
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

// moveStragglerRefs is mergeExternalRefs without its exact-conflict demotion.
// The merge demotes when both sides hold different exact ids from one source,
// because at merge time that conflict is evidence the pair may be wrong. A
// straggler was written after the merge was decided, and demoting would strip
// the survivor's own anchor: 10 of the 11 VNDB producers anchored onto retired
// labels in 2026-08 would have taken the survivor's VNDB id down with them. So a
// straggler that would be a second exact id from a source lands as related, and
// the survivor's refs are never touched. Curated ids stay exact, as in a merge:
// several wiki ids folding into one entity is what the curated source records.
func moveStragglerRefs(tx *gorm.DB, entityType int16, src, dst int64) error {
	_, err := execAll(tx, []mergeStmt{
		{`UPDATE catalog_external_ref r SET link_kind = ?
		   WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ?
		     AND r.source_id NOT IN (SELECT id FROM catalog_source WHERE key IN ?)
		     AND (EXISTS (SELECT 1 FROM catalog_external_ref t
		                   WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                     AND t.source_id = r.source_id AND t.link_kind = ?)
		          OR (SELECT count(*) FROM catalog_external_ref s
		               WHERE s.entity_type = r.entity_type AND s.entity_id = r.entity_id
		                 AND s.source_id = r.source_id AND s.link_kind = ?) > 1)`,
			[]any{model.LinkKindRelated, entityType, src, model.LinkKindExact, curatedSourceKeys,
				dst, model.LinkKindExact, model.LinkKindExact}, false},
		{`UPDATE catalog_external_ref r SET entity_id = ? WHERE r.entity_type = ? AND r.entity_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_external_ref t
		                     WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                       AND t.source_id = r.source_id AND t.external_id = r.external_id)`,
			[]any{dst, entityType, src, dst}, false},
		{`DELETE FROM catalog_external_ref WHERE entity_type = ? AND entity_id = ?`, []any{entityType, src}, false},
	})
	return err
}
