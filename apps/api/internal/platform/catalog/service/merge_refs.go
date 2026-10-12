package service

import (
	"fmt"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

// mergeExternalRefs never changes the survivor's refs. It used to demote every
// exact of a source both sides held under different ids, the survivor's own
// included: label merge 47793→5147 took 5147's DLsite VG03026 down to
// probable. Now only the loser's conflicting exact is demoted — related for an
// edition-splitting source, whose ids name editions of one work (see
// model.EditionSplittingSourceIDs), probable for the rest, where the conflict
// stays a review signal. Curated ids stay exact. A loser exact from a source
// the survivor has none of stays exact; when the loser brings several from an
// edition-splitting source, the shortest stays and the rest are related.
func mergeExternalRefs(tx *gorm.DB, p mergePlan) error {
	demoteTo := fmt.Sprint(model.LinkKindProbable)
	var args []any
	if p.entityType == model.EntityTypeWork {
		demoteTo = fmt.Sprintf("CASE WHEN r.source_id IN ? THEN %d ELSE %d END", model.LinkKindRelated, model.LinkKindProbable)
		args = append(args, model.EditionSplittingSourceIDs)
	}
	args = append(args, p.entityType, p.src, model.LinkKindExact, curatedSourceKeys, p.dst, model.LinkKindExact)
	stmts := []mergeStmt{
		{`UPDATE catalog_external_ref r SET link_kind = ` + demoteTo + `
		   WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ?
		     AND r.source_id NOT IN (SELECT id FROM catalog_source WHERE key IN ?)
		     AND EXISTS (SELECT 1 FROM catalog_external_ref t
		                  WHERE t.entity_type = r.entity_type AND t.entity_id = ? AND t.source_id = r.source_id
		                    AND t.link_kind = ? AND t.external_id <> r.external_id)`, args, false},
	}
	if p.entityType == model.EntityTypeWork {
		stmts = append(stmts, mergeStmt{`UPDATE catalog_external_ref r SET link_kind = ?
		   WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? AND r.source_id IN ?
		     AND NOT EXISTS (SELECT 1 FROM catalog_external_ref t
		                      WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                        AND t.source_id = r.source_id AND t.link_kind = ?)
		     AND EXISTS (SELECT 1 FROM catalog_external_ref k
		                  WHERE k.entity_type = r.entity_type AND k.entity_id = r.entity_id
		                    AND k.source_id = r.source_id AND k.link_kind = ?
		                    AND (length(k.external_id), k.external_id) < (length(r.external_id), r.external_id))`,
			[]any{model.LinkKindRelated, p.entityType, p.src, model.LinkKindExact, model.EditionSplittingSourceIDs,
				p.dst, model.LinkKindExact, model.LinkKindExact}, false})
	}
	stmts = append(stmts, moveRefStmts(p)...)
	_, err := execAll(tx, stmts)
	return err
}

func moveRefStmts(p mergePlan) []mergeStmt {
	return []mergeStmt{
		{`UPDATE catalog_external_ref r SET entity_id = ? WHERE r.entity_type = ? AND r.entity_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_external_ref t
		                     WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                       AND t.source_id = r.source_id AND t.external_id = r.external_id)`,
			[]any{p.dst, p.entityType, p.src, p.dst}, false},
		archiveDelete(p, "catalog_external_ref", "t.entity_type = ? AND t.entity_id = ?", p.entityType, p.src),
	}
}

// absorbExternalRefs carries every work-level ref of an absorbed edition over
// as related: the base work's identity is its VNDB VN, and an edition's
// Bangumi or EGS subject attaches to it but never anchors it. The original
// rows are archived, so an unmerge hands them back.
func absorbExternalRefs(tx *gorm.DB, p mergePlan) error {
	return tx.Exec(`WITH gone AS (DELETE FROM catalog_external_ref t
		                  WHERE t.entity_type = ? AND t.entity_id = ? RETURNING t.*),
		     archived AS (INSERT INTO catalog_merge_archive (proposal_id, table_name, row)
		                  SELECT ?, 'catalog_external_ref', to_jsonb(g) FROM gone g)
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by, created_at, dead_at)
		SELECT entity_type, ?, source_id, external_id, ?, ?, now(), dead_at FROM gone
		ON CONFLICT DO NOTHING`,
		p.entityType, p.src, p.proposalID, p.dst, model.LinkKindRelated, absorbMatchedBy).Error
}

// moveStragglerRefs is mergeExternalRefs for a row written after the merge
// was decided, where a conflict is no evidence the pair is wrong: 10 of the 11
// VNDB producers anchored onto retired labels in 2026-08 would have taken the
// survivor's VNDB id down with them. A straggler that would be a second exact
// id from a source lands as related. Curated ids stay exact, as in a merge.
func moveStragglerRefs(tx *gorm.DB, p mergePlan) error {
	stmts := append([]mergeStmt{
		{`UPDATE catalog_external_ref r SET link_kind = ?
		   WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ?
		     AND r.source_id NOT IN (SELECT id FROM catalog_source WHERE key IN ?)
		     AND (EXISTS (SELECT 1 FROM catalog_external_ref t
		                   WHERE t.entity_type = r.entity_type AND t.entity_id = ?
		                     AND t.source_id = r.source_id AND t.link_kind = ?)
		          OR (SELECT count(*) FROM catalog_external_ref s
		               WHERE s.entity_type = r.entity_type AND s.entity_id = r.entity_id
		                 AND s.source_id = r.source_id AND s.link_kind = ?) > 1)`,
			[]any{model.LinkKindRelated, p.entityType, p.src, model.LinkKindExact, curatedSourceKeys,
				p.dst, model.LinkKindExact, model.LinkKindExact}, false},
	}, moveRefStmts(p)...)
	_, err := execAll(tx, stmts)
	return err
}
