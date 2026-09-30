package service

import (
	"fmt"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"

	"gorm.io/gorm"
)

type mergeStmt struct {
	sql          string
	args         []any
	collectWorks bool
}

type mergeStep int

const (
	stepRehang mergeStep = iota
	stepExternalRefs
	stepUsage
)

type mergeRef struct {
	table, column string
	typeSQL       string
	families      []int16
	step          mergeStep
}

// THE LIST IS THE CONTRACT: every column that holds the id of a merge-able
// entity is in mergeRefs, which the merge moves and SweepStragglers audits, or in
// mergeRefExclusions, with the reason it stays on the retired id. A column in
// neither strands its rows on the retired id, where nothing reads them and no
// constraint complains: catalog_work_cover did it with 12 rows (wave 170b), and
// catalog_release_label, born after the prose list this replaced, with 2,536
// rows on 11 merged labels. TestMergeRefsCoverTheSchema fails when the schema
// grows a column neither list names; TestRehangNamesEveryRef fails when a
// rehang ref has no statement for its family. Columns that do not follow the
// <family>_id naming (catalog_character.instance_of) are invisible to that walk
// and must be added here by hand.
var mergeRefs = []mergeRef{
	rehangRef("catalog_character", "instance_of", model.EntityTypeCharacter),
	rehangRef("catalog_character_alias", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_character_intro", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_character_intro_panel_verdict", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_character_intro_panel_verdict", "work_id", model.EntityTypeWork),
	rehangRef("catalog_character_trait_link", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_cover_vote", "work_id", model.EntityTypeWork),
	rehangRef("catalog_credit", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_credit", "credit_name_id", model.EntityTypeCreditName),
	rehangRef("catalog_credit", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_credit", "work_id", model.EntityTypeWork),
	rehangRef("catalog_credit_name", "person_id", model.EntityTypePerson),
	{"catalog_entity_relation", "a_id", "t.entity_type", []int16{model.EntityTypePerson, model.EntityTypeCreditName, model.EntityTypeLabel, model.EntityTypeCharacter}, stepRehang},
	{"catalog_entity_relation", "b_id", "t.entity_type", []int16{model.EntityTypePerson, model.EntityTypeCreditName, model.EntityTypeLabel, model.EntityTypeCharacter}, stepRehang},
	{"catalog_entity_usage", "entity_id", "t.entity_type", nil, stepUsage},
	{"catalog_external_ref", "entity_id", "t.entity_type", nil, stepExternalRefs},
	rehangRef("catalog_label_alias", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_label_intro", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_label_relation", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_label_relation", "other_label_id", model.EntityTypeLabel),
	rehangRef("catalog_name_alias", "credit_name_id", model.EntityTypeCreditName),
	rehangRef("catalog_person", "primary_credit_name_id", model.EntityTypeCreditName),
	rehangRef("catalog_person_intro", "person_id", model.EntityTypePerson),
	rehangRef("catalog_release", "work_id", model.EntityTypeWork),
	rehangRef("catalog_release_label", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_series_member", "work_id", model.EntityTypeWork),
	{"catalog_user_entity_follow", "entity_id", "t.entity_type", []int16{model.EntityTypeLabel}, stepRehang},
	rehangRef("catalog_user_folder_item", "work_id", model.EntityTypeWork),
	rehangRef("catalog_user_playtime", "work_id", model.EntityTypeWork),
	rehangRef("catalog_user_work_state", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_character", "character_id", model.EntityTypeCharacter),
	rehangRef("catalog_work_character", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_cover", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_engine", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_intro", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_label", "label_id", model.EntityTypeLabel),
	rehangRef("catalog_work_label", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_platform", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_playtime", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_popularity", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_rating", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_relation", "a_work_id", model.EntityTypeWork),
	rehangRef("catalog_work_relation", "b_work_id", model.EntityTypeWork),
	rehangRef("catalog_work_screenshot", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_tag", "work_id", model.EntityTypeWork),
	rehangRef("catalog_work_title", "work_id", model.EntityTypeWork),
	{"edit_suppressed_row", "entity_id",
		fmt.Sprintf("CASE t.entity_type WHEN '%s' THEN %d WHEN '%s' THEN %d END",
			editspec.TypeWork, model.EntityTypeWork, editspec.TypeCharacter, model.EntityTypeCharacter),
		[]int16{model.EntityTypeWork, model.EntityTypeCharacter}, stepRehang},
}

var mergeRefExclusions = map[string]string{
	"catalog_claim_event.work_id":             "append-only claim history, addressed to the id that was claimed",
	"catalog_revision.entity_id":              "append-only history; the merge writes its own revision rows on both sides",
	"edit_proposal.entity_id":                 "editing history and open queue, addressed to the id that was actually edited",
	"edit_revision.entity_id":                 "editing history, addressed to the id that was actually edited",
	"catalog_match_candidate.a_id":            "reconciliation queue about the source id; the matcher regenerates it",
	"catalog_match_candidate.b_id":            "reconciliation queue about the source id; the matcher regenerates it",
	"catalog_match_rejection.entity_id":       "audit trail of what was actually compared",
	"catalog_merge_proposal.source_entity_id": "the merge's own record of its source",
	"catalog_merge_proposal.target_entity_id": "the merge's own record of its target",
	"catalog_work.product_work_id":            "a product-side claim id, not a catalog entity id; retireSource frees it",
}

func rehangRef(table, column string, family int16) mergeRef {
	return mergeRef{table, column, fmt.Sprint(family), []int16{family}, stepRehang}
}

// rehangEntity repoints every child/reference of source onto target,
// deleting rows that would violate the target's unique constraints (the
// child already exists on the target — a true duplicate). It returns the work
// ids whose rendered content changed, per the wave-118 touch matrix: a work
// renders credits[], its roster and its brand labels, so credit / character /
// label merges rewrite works that are not themselves part of the merge; person
// and org ids never reach the work face, so those merges touch nothing.
//
// Suppression identity keys that contain a merge-able id (wave 09/D13) are not
// columns mergeRefs could name: a credit_name merge rewrites keys hanging off
// works that are not in the merge at all. Those rewrites come from the editing
// registry, where every field declares its IdentitySpec.
func rehangEntity(tx *gorm.DB, reg *editing.Registry, entityType int16, src, dst int64) ([]int64, error) {
	stmts, err := rehangStmts(reg, entityType, src, dst)
	if err != nil {
		return nil, err
	}
	return execAll(tx, stmts)
}

func rehangStmts(reg *editing.Registry, entityType int16, src, dst int64) ([]mergeStmt, error) {
	switch entityType {
	case model.EntityTypePerson:
		stmts := []mergeStmt{
			{`UPDATE catalog_credit_name SET person_id = ? WHERE person_id = ?`, []any{dst, src}, false},
			{`UPDATE catalog_person_intro i SET person_id = ? WHERE i.person_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_person_intro x
			                     WHERE x.person_id = ? AND x.lang = i.lang AND x.source_id = i.source_id)`,
				[]any{dst, src, dst}, false},
			{`DELETE FROM catalog_person_intro WHERE person_id = ?`, []any{src}, false},
		}
		stmts = append(stmts, identityFollowStmts(reg, editspec.TagPerson, src, dst)...)
		return append(stmts, entityRelationStmts(entityType, src, dst)...), nil

	case model.EntityTypeCreditName:
		stmts := []mergeStmt{
			{`DELETE FROM catalog_credit d
			   WHERE d.credit_name_id = ? AND ` + notCuratedCreditD + `
			     AND EXISTS (SELECT 1 FROM catalog_credit c
			                  WHERE c.credit_name_id = ? AND ` + curatedCreditC + `
			                    AND c.work_id = d.work_id AND c.role_id = d.role_id
			                    AND COALESCE(c.character_id, 0) = COALESCE(d.character_id, 0))
			  RETURNING d.work_id`, []any{dst, src}, true},
			{`UPDATE catalog_credit c SET credit_name_id = ? WHERE c.credit_name_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_credit d
			                     WHERE d.work_id = c.work_id AND d.credit_name_id = ?
			                       AND d.role_id = c.role_id
			                       AND COALESCE(d.character_id, 0) = COALESCE(c.character_id, 0))
			  RETURNING c.work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_credit WHERE credit_name_id = ? RETURNING work_id`, []any{src}, true},
			{`UPDATE catalog_name_alias a SET credit_name_id = ? WHERE a.credit_name_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_name_alias b
			                     WHERE b.credit_name_id = ? AND b.name = a.name AND b.lang = a.lang)`, []any{dst, src, dst}, false},
			{`DELETE FROM catalog_name_alias WHERE credit_name_id = ?`, []any{src}, false},
			{`UPDATE catalog_person SET primary_credit_name_id = ? WHERE primary_credit_name_id = ?`, []any{dst, src}, false},
		}
		stmts = append(stmts, identityFollowStmts(reg, editspec.TagCreditName, src, dst)...)
		return append(stmts, entityRelationStmts(entityType, src, dst)...), nil

	case model.EntityTypeLabel:
		stmts := []mergeStmt{
			{`UPDATE catalog_credit SET label_id = ? WHERE label_id = ? RETURNING work_id`, []any{dst, src}, true},
			{`UPDATE catalog_label_alias a SET label_id = ? WHERE a.label_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_label_alias b
			                     WHERE b.label_id = ? AND b.name = a.name AND b.lang = a.lang)`, []any{dst, src, dst}, false},
			{`DELETE FROM catalog_label_alias WHERE label_id = ?`, []any{src}, false},
			{`UPDATE catalog_label_intro i SET label_id = ? WHERE i.label_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_label_intro x
			                     WHERE x.label_id = ? AND x.lang = i.lang AND x.source_id = i.source_id)`,
				[]any{dst, src, dst}, false},
			{`DELETE FROM catalog_label_intro WHERE label_id = ?`, []any{src}, false},
			{`UPDATE catalog_work_label e SET label_id = ? WHERE e.label_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_work_label x
			                     WHERE x.work_id = e.work_id AND x.label_id = ? AND x.kind = e.kind)
			  RETURNING e.work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_work_label WHERE label_id = ? RETURNING work_id`, []any{src}, true},
			{`UPDATE catalog_release_label e SET label_id = ? WHERE e.label_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_release_label x
			                     WHERE x.release_id = e.release_id AND x.label_id = ? AND x.kind = e.kind)
			  RETURNING (SELECT r.work_id FROM catalog_release r WHERE r.id = e.release_id)`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_release_label e WHERE e.label_id = ?
			  RETURNING (SELECT r.work_id FROM catalog_release r WHERE r.id = e.release_id)`, []any{src}, true},
			{`UPDATE catalog_user_entity_follow f SET entity_id = ? WHERE f.entity_type = ? AND f.entity_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_user_entity_follow x
			                     WHERE x.actor_uid = f.actor_uid AND x.entity_type = ? AND x.entity_id = ?)`,
				[]any{dst, model.EntityTypeLabel, src, model.EntityTypeLabel, dst}, false},
			{`DELETE FROM catalog_user_entity_follow WHERE entity_type = ? AND entity_id = ?`,
				[]any{model.EntityTypeLabel, src}, false},
		}
		stmts = append(stmts, labelRelationStmts(src, dst)...)
		stmts = append(stmts, identityFollowStmts(reg, editspec.TagLabel, src, dst)...)
		return append(stmts, entityRelationStmts(entityType, src, dst)...), nil

	case model.EntityTypeCharacter:
		stmts := []mergeStmt{
			{`DELETE FROM catalog_credit d
			   WHERE d.character_id = ? AND ` + notCuratedCreditD + `
			     AND EXISTS (SELECT 1 FROM catalog_credit c
			                  WHERE c.character_id = ? AND ` + curatedCreditC + `
			                    AND c.work_id = d.work_id AND c.credit_name_id = d.credit_name_id
			                    AND c.role_id = d.role_id)
			  RETURNING d.work_id`, []any{dst, src}, true},
			{`DELETE FROM catalog_character_alias b
			   WHERE b.character_id = ? AND ` + editspec.NotCuratedLaneSQL("b.source_id") + `
			     AND EXISTS (SELECT 1 FROM catalog_character_alias a
			                  WHERE a.character_id = ? AND ` + editspec.CuratedLaneSQL("a.source_id") + `
			                    AND a.name = b.name AND a.lang = b.lang)`, []any{dst, src}, false},
			{`UPDATE catalog_credit c SET character_id = ? WHERE c.character_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_credit d
			                     WHERE d.work_id = c.work_id AND d.credit_name_id = c.credit_name_id
			                       AND d.role_id = c.role_id AND COALESCE(d.character_id, 0) = ?)
			  RETURNING c.work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_credit WHERE character_id = ? RETURNING work_id`, []any{src}, true},
			{`UPDATE catalog_character_alias a SET character_id = ? WHERE a.character_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_character_alias b
			                     WHERE b.character_id = ? AND b.name = a.name AND b.lang = a.lang)`, []any{dst, src, dst}, false},
			{`DELETE FROM catalog_character_alias WHERE character_id = ?`, []any{src}, false},
			{`UPDATE catalog_work_character d
			    ` + rosterSurvivorshipSet + `
			    FROM catalog_work_character s
			    WHERE d.character_id = ? AND s.character_id = ? AND s.work_id = d.work_id`, []any{dst, src}, false},
			{`UPDATE catalog_work_character e SET character_id = ?, updated_at = now() WHERE e.character_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_work_character x
			                     WHERE x.work_id = e.work_id AND x.character_id = ?)
			  RETURNING e.work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_work_character WHERE character_id = ? RETURNING work_id`, []any{src}, true},
			{`UPDATE catalog_character_intro i SET character_id = ? WHERE i.character_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_character_intro x
			                     WHERE x.character_id = ? AND x.lang = i.lang AND x.source_id = i.source_id)`,
				[]any{dst, src, dst}, false},
			{`DELETE FROM catalog_character_intro WHERE character_id = ?`, []any{src}, false},
			// Kept-verdict cache, not data: dropped rather than repointed.
			{`DELETE FROM catalog_character_intro_panel_verdict WHERE character_id = ?`, []any{src}, false},
			{`UPDATE catalog_character_trait_link t SET character_id = ? WHERE t.character_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_character_trait_link x
			                     WHERE x.character_id = ? AND x.trait_id = t.trait_id)`,
				[]any{dst, src, dst}, false},
			{`DELETE FROM catalog_character_trait_link WHERE character_id = ?`, []any{src}, false},
			{`UPDATE catalog_character SET instance_of = ? WHERE instance_of = ?`, []any{dst, src}, false},
		}
		stmts = append(stmts, suppressedRowStmts(editspec.TypeCharacter, src, dst)...)
		stmts = append(stmts, identityFollowStmts(reg, editspec.TagCharacter, src, dst)...)
		return append(stmts, entityRelationStmts(entityType, src, dst)...), nil

	case model.EntityTypeWork:
		stmts := []mergeStmt{
			{`DELETE FROM catalog_work_relation
			   WHERE (a_work_id = ? AND b_work_id = ?) OR (a_work_id = ? AND b_work_id = ?)`, []any{src, dst, dst, src}, false},
			{`UPDATE catalog_work_relation r SET a_work_id = ? WHERE r.a_work_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_work_relation x
			                     WHERE x.a_work_id = ? AND x.b_work_id = r.b_work_id
			                       AND x.relation_type_id = r.relation_type_id)
			  RETURNING r.b_work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_work_relation WHERE a_work_id = ? RETURNING b_work_id`, []any{src}, true},
			{`UPDATE catalog_work_relation r SET b_work_id = ? WHERE r.b_work_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_work_relation x
			                     WHERE x.b_work_id = ? AND x.a_work_id = r.a_work_id
			                       AND x.relation_type_id = r.relation_type_id)
			  RETURNING r.a_work_id`, []any{dst, src, dst}, true},
			{`DELETE FROM catalog_work_relation WHERE b_work_id = ? RETURNING a_work_id`, []any{src}, true},
			{`UPDATE catalog_work_title t SET work_id = ? WHERE t.work_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_work_title u
			                     WHERE u.work_id = ? AND u.lang = t.lang AND u.title = t.title AND u.kind = t.kind)`, []any{dst, src, dst}, false},
			{`DELETE FROM catalog_work_title WHERE work_id = ?`, []any{src}, false},
			{`UPDATE catalog_release SET work_id = ? WHERE work_id = ?`, []any{dst, src}, false},
			{`DELETE FROM catalog_credit d
			   WHERE d.work_id = ? AND ` + notCuratedCreditD + `
			     AND EXISTS (SELECT 1 FROM catalog_credit c
			                  WHERE c.work_id = ? AND ` + curatedCreditC + `
			                    AND c.credit_name_id = d.credit_name_id AND c.role_id = d.role_id
			                    AND COALESCE(c.character_id, 0) = COALESCE(d.character_id, 0))`,
				[]any{dst, src}, false},
			{`UPDATE catalog_credit c SET work_id = ? WHERE c.work_id = ?
			    AND NOT EXISTS (SELECT 1 FROM catalog_credit d
			                     WHERE d.work_id = ? AND d.credit_name_id = c.credit_name_id
			                       AND d.role_id = c.role_id
			                       AND COALESCE(d.character_id, 0) = COALESCE(c.character_id, 0))`, []any{dst, src, dst}, false},
			{`DELETE FROM catalog_credit WHERE work_id = ?`, []any{src}, false},
		}
		stmts = append(stmts, suppressedRowStmts(editspec.TypeWork, src, dst)...)
		stmts = append(stmts, identityFollowStmts(reg, editspec.TagWork, src, dst)...)
		return append(stmts, workFacetStmts(src, dst)...), nil
	}
	return nil, fmt.Errorf("catalog merge: unsupported entity type %d", entityType)
}

// rehangEntity's `NOT EXISTS` + unconditional DELETE pairs settle a unique-key
// collision by keeping whatever the target already had, which for
// catalog_credit and catalog_character_alias silently threw away the human
// lane's row: credit rows never enter a catalog_revision snapshot and
// edit_revision records intentions rather than machine deletes, so a curated
// credit lost to a merge left no retrievable trace anywhere. Pillar 6 ("what a
// person wrote outranks a machine refresh") decides it at row level, so each of
// those pairs is preceded by a statement that removes the upstream row standing
// on the key instead. Both tables hold 0 curated rows today (2026-08), which is
// exactly why this is cheap now.
var (
	curatedCreditC    = editspec.CuratedLaneSQL("c.source_id")
	notCuratedCreditD = editspec.NotCuratedLaneSQL("d.source_id")

	// catalog_work_character has no source column: one (work, character) pair is
	// a single consensus row every importer and every merge writes into, and the
	// only record of who set a column is its field_provenance stamp. The two
	// machine rules stay as they were; a stamped column simply keeps what the
	// person put there. Both axes of the merge share this clause because they
	// were copy-pasted from each other.
	rosterSurvivorshipSet = `SET kind = CASE
		        WHEN ` + editspec.HumanFieldProvenanceSQL("d.field_provenance", "kind") + ` THEN d.kind
		        WHEN d.kind = 0 THEN s.kind
		        ELSE d.kind END,
		    spoiler = CASE
		        WHEN ` + editspec.HumanFieldProvenanceSQL("d.field_provenance", "spoiler") + ` THEN d.spoiler
		        ELSE GREATEST(d.spoiler, s.spoiler) END,
		    updated_at = now()`
)

func identityFollowStmts(reg *editing.Registry, entityTag string, src, dst int64) []mergeStmt {
	follow := reg.IdentityFollowStmts(entityTag, src, dst, nil)
	out := make([]mergeStmt, 0, len(follow))
	for _, s := range follow {
		out = append(out, mergeStmt{sql: s.SQL, args: s.Args})
	}
	return out
}

func suppressedRowStmts(entityType string, src, dst int64) []mergeStmt {
	return []mergeStmt{
		{`UPDATE edit_suppressed_row s SET entity_id = ?
		    WHERE s.entity_type = ? AND s.entity_id = ?
		    AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		                     WHERE x.entity_type = s.entity_type AND x.entity_id = ?
		                       AND x.field_key = s.field_key AND x.identity_key = s.identity_key)`,
			[]any{dst, entityType, src, dst}, false},
		{`DELETE FROM edit_suppressed_row WHERE entity_type = ? AND entity_id = ?`,
			[]any{entityType, src}, false},
	}
}

func workFacetStmts(src, dst int64) []mergeStmt {
	stmts := []mergeStmt{
		{`UPDATE catalog_work_character d
		    ` + rosterSurvivorshipSet + `
		    FROM catalog_work_character s
		    WHERE d.work_id = ? AND s.work_id = ? AND s.character_id = d.character_id`, []any{dst, src}, false},
		{`UPDATE catalog_user_playtime d
		    SET minutes = GREATEST(d.minutes, s.minutes),
		        last_played_at = GREATEST(d.last_played_at, s.last_played_at),
		        updated_at = now()
		    FROM catalog_user_playtime s
		    WHERE d.work_id = ? AND s.work_id = ?
		      AND s.actor_uid = d.actor_uid AND s.client_id = d.client_id`, []any{dst, src}, false},
		// Kept-verdict cache, not data: rows are re-derivable, so a merge drops
		// them instead of repointing (the pair is re-judged once if it recurs).
		{`DELETE FROM catalog_character_intro_panel_verdict WHERE work_id = ?`, []any{src}, false},
		// Folder memberships take a custom move instead of the generic rehang:
		// managers mirror these rows incrementally off updated_at, so the
		// repointed row must surface on the sync cursor or every synced client
		// keeps a membership addressed to the retired id.
		{`UPDATE catalog_user_folder_item f SET work_id = ?, updated_at = now()
		    WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_user_folder_item x
		                     WHERE x.work_id = ? AND x.folder_id = f.folder_id)`, []any{dst, src, dst}, false},
		{`DELETE FROM catalog_user_folder_item WHERE work_id = ?`, []any{src}, false},
		// Every folder the merge touched holds dst afterwards (a src-only row
		// was repointed to dst; a duplicate was deleted because dst was already
		// there), so recounting the dst holders repairs every drifted count.
		{`UPDATE catalog_user_folder f
		    SET item_count = (SELECT count(*) FROM catalog_user_folder_item i WHERE i.folder_id = f.id),
		        updated_at = now()
		    WHERE f.id IN (SELECT folder_id FROM catalog_user_folder_item WHERE work_id = ?)`, []any{dst}, false},
		// Work states sync incrementally off updated_at like folder items, so
		// the same custom move applies. Where a user holds state on both sides,
		// the later-updated row is the intent that survives.
		{`UPDATE catalog_user_work_state d
		    SET state = s.state, completion = s.completion, updated_at = now()
		    FROM catalog_user_work_state s
		    WHERE d.work_id = ? AND s.work_id = ?
		      AND s.actor_uid = d.actor_uid AND s.updated_at > d.updated_at`, []any{dst, src}, false},
		{`UPDATE catalog_user_work_state f SET work_id = ?, updated_at = now()
		    WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_user_work_state x
		                     WHERE x.work_id = ? AND x.actor_uid = f.actor_uid)`, []any{dst, src, dst}, false},
		{`DELETE FROM catalog_user_work_state WHERE work_id = ?`, []any{src}, false},
	}
	for _, f := range []struct {
		table string
		key   []string
	}{
		{"catalog_work_intro", []string{"lang", "source_id"}},
		{"catalog_work_cover", []string{"image_hash"}},
		{"catalog_cover_vote", []string{"actor_uid"}},
		{"catalog_work_screenshot", []string{"image_hash"}},
		{"catalog_work_rating", []string{"source_id"}},
		{"catalog_work_tag", []string{"name", "source_id"}},
		{"catalog_work_popularity", []string{"source_id", "metric"}},
		{"catalog_work_playtime", []string{"source_id"}},
		{"catalog_user_playtime", []string{"actor_uid", "client_id"}},
		{"catalog_work_platform", []string{"platform", "source_id"}},
		{"catalog_work_engine", []string{"engine_id", "source_id"}},
		{"catalog_series_member", []string{"series_id"}},
		{"catalog_work_label", []string{"label_id", "kind"}},
		{"catalog_work_character", []string{"character_id"}},
	} {
		stmts = append(stmts, workFacetRehang(f.table, f.key, src, dst)...)
	}
	return stmts
}

func workFacetRehang(table string, keyCols []string, src, dst int64) []mergeStmt {
	match := make([]string, 0, len(keyCols))
	for _, c := range keyCols {
		match = append(match, fmt.Sprintf("x.%s = f.%s", c, c))
	}
	move := fmt.Sprintf(
		`UPDATE %s f SET work_id = ? WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM %s x WHERE x.work_id = ? AND %s)`,
		table, table, strings.Join(match, " AND "))
	return []mergeStmt{
		{move, []any{dst, src, dst}, false},
		{fmt.Sprintf(`DELETE FROM %s WHERE work_id = ?`, table), []any{src}, false},
	}
}

func labelRelationStmts(src, dst int64) []mergeStmt {
	return []mergeStmt{
		{`DELETE FROM catalog_label_relation
		   WHERE (label_id = ? AND other_label_id = ?) OR (label_id = ? AND other_label_id = ?)`,
			[]any{src, dst, dst, src}, false},
		{`UPDATE catalog_label_relation r SET label_id = ? WHERE r.label_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_label_relation x
		                     WHERE x.label_id = ? AND x.other_label_id = r.other_label_id
		                       AND x.relation = r.relation AND x.source_id = r.source_id)`,
			[]any{dst, src, dst}, false},
		{`DELETE FROM catalog_label_relation WHERE label_id = ?`, []any{src}, false},
		{`UPDATE catalog_label_relation r SET other_label_id = ? WHERE r.other_label_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_label_relation x
		                     WHERE x.other_label_id = ? AND x.label_id = r.label_id
		                       AND x.relation = r.relation AND x.source_id = r.source_id)`,
			[]any{dst, src, dst}, false},
		{`DELETE FROM catalog_label_relation WHERE other_label_id = ?`, []any{src}, false},
	}
}

func entityRelationStmts(entityType int16, src, dst int64) []mergeStmt {
	return []mergeStmt{
		{`DELETE FROM catalog_entity_relation
		   WHERE entity_type = ? AND ((a_id = ? AND b_id = ?) OR (a_id = ? AND b_id = ?))`,
			[]any{entityType, src, dst, dst, src}, false},
		{`UPDATE catalog_entity_relation e SET a_id = ? WHERE e.entity_type = ? AND e.a_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_entity_relation x
		                     WHERE x.entity_type = e.entity_type AND x.a_id = ? AND x.b_id = e.b_id
		                       AND x.relation_type_id = e.relation_type_id)`,
			[]any{dst, entityType, src, dst}, false},
		{`DELETE FROM catalog_entity_relation WHERE entity_type = ? AND a_id = ?`, []any{entityType, src}, false},
		{`UPDATE catalog_entity_relation e SET b_id = ? WHERE e.entity_type = ? AND e.b_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_entity_relation x
		                     WHERE x.entity_type = e.entity_type AND x.b_id = ? AND x.a_id = e.a_id
		                       AND x.relation_type_id = e.relation_type_id)`,
			[]any{dst, entityType, src, dst}, false},
		{`DELETE FROM catalog_entity_relation WHERE entity_type = ? AND b_id = ?`, []any{entityType, src}, false},
	}
}

func execAll(tx *gorm.DB, stmts []mergeStmt) ([]int64, error) {
	var touched []int64
	for _, s := range stmts {
		if !s.collectWorks {
			if err := tx.Exec(s.sql, s.args...).Error; err != nil {
				return nil, err
			}
			continue
		}
		var ids []int64
		if err := tx.Raw(s.sql, s.args...).Scan(&ids).Error; err != nil {
			return nil, err
		}
		touched = append(touched, ids...)
	}
	return touched, nil
}
