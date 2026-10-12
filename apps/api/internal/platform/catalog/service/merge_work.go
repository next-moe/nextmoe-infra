package service

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
)

func workRehangStmts(reg *editing.Registry, p mergePlan) []mergeStmt {
	stmts := []mergeStmt{coverVoteRepoint(p)}
	if p.absorb {
		stmts = append(stmts, absorbArchiveStmts(p)...)
	}
	stmts = append(stmts, workRelationStmts(p)...)
	stmts = append(stmts, workTitleStmts(p)...)
	stmts = append(stmts, mergeStmt{`UPDATE catalog_release SET work_id = ? WHERE work_id = ?`, []any{p.dst, p.src}, false})
	stmts = append(stmts, workCreditStmts(p)...)
	stmts = append(stmts, suppressedRowStmts(editspec.TypeWork, p.src, p.dst)...)
	stmts = append(stmts, identityFollowStmts(reg, editspec.TagWork, p.src, p.dst)...)
	stmts = append(stmts, workFacetStmts(p)...)
	return append(stmts, mergeStmt{editspec.PurgeSuppressedWorkTagsSQL, []any{p.dst}, false})
}

// absorbArchived and absorbKept together name every work-family table in
// mergeRefs; TestAbsorbClassifiesEveryWorkRef fails on a table in neither. An
// absorbed edition contributes no imported data to its base work: these rows
// are archived and dropped, and the rows a person wrote follow the ordinary
// merge that runs after them.
var absorbArchived = map[string]string{
	"catalog_work_cover":      importedRows,
	"catalog_work_screenshot": importedRows,
	"catalog_work_intro":      importedRows,
	"catalog_work_rating":     importedRows,
	"catalog_work_tag":        importedRows,
	"catalog_work_popularity": importedRows,
	"catalog_work_playtime":   importedRows,
	"catalog_work_platform":   importedRows,
	"catalog_work_engine":     importedRows,
	"catalog_work_label":      importedRows,
	"catalog_credit":          importedRows,
	"catalog_series_member":   "t.work_id = ?",
	"catalog_work_character":  "t.work_id = ?",
}

var absorbKept = map[string]string{
	"catalog_work_title":                    "every title becomes a language-less alias (workTitleStmts)",
	"catalog_work_relation":                 "imported edges are archived by absorbArchiveStmts, human ones move",
	"catalog_release":                       "releases of the same VN move with their release-level refs",
	"catalog_external_ref":                  "work-level refs move as related (absorbExternalRefs)",
	"catalog_entity_usage":                  "site usage moves",
	"catalog_cover_vote":                    "user data moves; votes on an archived cover are archived with it",
	"catalog_user_folder_item":              "user data moves",
	"catalog_user_playtime":                 "user data moves",
	"catalog_user_work_rating":              "user data moves",
	"catalog_user_work_state":               "user data moves",
	"edit_suppressed_row":                   "editorial state moves",
	"catalog_character_intro_panel_verdict": "a cache; dropped",
}

var importedRows = "t.work_id = ? AND NOT " + humanLaneSQL("t.source_id")

func absorbArchiveStmts(p mergePlan) []mergeStmt {
	stmts := []mergeStmt{
		archiveDelete(p, "catalog_cover_vote", `t.cover_id IN (SELECT c.id FROM catalog_work_cover c
			WHERE c.work_id = ? AND NOT `+humanLaneSQL("c.source_id")+`)`, p.src),
		archiveDeleteReturning(p, "catalog_work_relation",
			"(t.a_work_id = ? OR t.b_work_id = ?) AND NOT "+humanLaneSQL("t.source_id"), []any{p.src, p.src},
			"CASE WHEN a_work_id = ? THEN b_work_id ELSE a_work_id END", p.src),
	}
	for _, table := range slices.Sorted(maps.Keys(absorbArchived)) {
		stmts = append(stmts, archiveDelete(p, table, absorbArchived[table], p.src))
	}
	return stmts
}

func workRelationStmts(p mergePlan) []mergeStmt {
	src, dst := p.src, p.dst
	return []mergeStmt{
		archiveDelete(p, "catalog_work_relation",
			"(t.a_work_id = ? AND t.b_work_id = ?) OR (t.a_work_id = ? AND t.b_work_id = ?)", src, dst, dst, src),
		{`UPDATE catalog_work_relation r SET a_work_id = ? WHERE r.a_work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_work_relation x
		                     WHERE x.a_work_id = ? AND x.b_work_id = r.b_work_id
		                       AND x.relation_type_id = r.relation_type_id)
		  RETURNING r.b_work_id`, []any{dst, src, dst}, true},
		archiveDeleteReturning(p, "catalog_work_relation", "t.a_work_id = ?", []any{src}, "b_work_id"),
		{`UPDATE catalog_work_relation r SET b_work_id = ? WHERE r.b_work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_work_relation x
		                     WHERE x.b_work_id = ? AND x.a_work_id = r.a_work_id
		                       AND x.relation_type_id = r.relation_type_id)
		  RETURNING r.a_work_id`, []any{dst, src, dst}, true},
		archiveDeleteReturning(p, "catalog_work_relation", "t.b_work_id = ?", []any{src}, "a_work_id"),
	}
}

func workCreditStmts(p mergePlan) []mergeStmt {
	src, dst := p.src, p.dst
	return []mergeStmt{
		archiveDelete(p, "catalog_credit", `t.work_id = ? AND `+editspec.NotCuratedLaneSQL("t.source_id")+`
		     AND EXISTS (SELECT 1 FROM catalog_credit c
		                  WHERE c.work_id = ? AND `+curatedCreditC+`
		                    AND c.credit_name_id = t.credit_name_id AND c.role_id = t.role_id
		                    AND COALESCE(c.character_id, 0) = COALESCE(t.character_id, 0))`, dst, src),
		{`UPDATE catalog_credit c SET work_id = ? WHERE c.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_credit d
		                     WHERE d.work_id = ? AND d.credit_name_id = c.credit_name_id
		                       AND d.role_id = c.role_id
		                       AND COALESCE(d.character_id, 0) = COALESCE(c.character_id, 0))`, []any{dst, src, dst}, false},
		archiveDelete(p, "catalog_credit", "t.work_id = ?", src),
	}
}

func coverVoteRepoint(p mergePlan) mergeStmt {
	return mergeStmt{`UPDATE catalog_cover_vote v SET cover_id = d.id, updated_at = now()
		FROM catalog_work_cover s JOIN catalog_work_cover d ON d.work_id = ? AND d.image_hash = s.image_hash
		WHERE s.work_id = ? AND v.cover_id = s.id`, []any{p.dst, p.src}, false}
}

func mergedMediaStmts(p mergePlan, table, extraSet string) []mergeStmt {
	return []mergeStmt{
		{fmt.Sprintf(`UPDATE %[1]s f SET work_id = ?, merged_in = true%[2]s, updated_at = now(),
		      sort_order = f.sort_order + (SELECT COALESCE(max(x.sort_order) + 1, 0) FROM %[1]s x WHERE x.work_id = ?)
		    WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.work_id = ? AND x.image_hash = f.image_hash)`, table, extraSet),
			[]any{p.dst, p.dst, p.src, p.dst}, false},
		archiveDelete(p, table, "t.work_id = ?", p.src),
	}
}

func workFacetStmts(p mergePlan) []mergeStmt {
	src, dst := p.src, p.dst
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
	stmts = append(stmts, ratingMergeStmts(src, dst)...)
	stmts = append(stmts, introStmts(p, "catalog_work_intro", "work_id")...)
	stmts = append(stmts, mergedMediaStmts(p, "catalog_work_cover", ", portrait_pinned = false")...)
	stmts = append(stmts, mergedMediaStmts(p, "catalog_work_screenshot", "")...)
	for _, f := range []struct {
		table string
		key   []string
		user  bool
	}{
		{"catalog_cover_vote", []string{"actor_uid"}, true},
		{"catalog_work_rating", []string{"source_id"}, false},
		{"catalog_work_tag", []string{"name", "source_id"}, false},
		{"catalog_work_popularity", []string{"source_id", "metric"}, false},
		{"catalog_work_playtime", []string{"source_id"}, false},
		{"catalog_user_playtime", []string{"actor_uid", "client_id"}, true},
		{"catalog_work_platform", []string{"platform", "source_id"}, false},
		{"catalog_work_engine", []string{"engine_id", "source_id"}, false},
		{"catalog_series_member", []string{"series_id"}, false},
		{"catalog_work_label", []string{"label_id", "kind"}, false},
		{"catalog_work_character", []string{"character_id"}, false},
	} {
		stmts = append(stmts, workFacetRehang(p, f.table, f.key, f.user)...)
	}
	return stmts
}

// A rating keeps its id across a merge, because site mirrors hang likes and
// threads on it. Where a user rated both works the later-updated row survives
// and the other leaves a tombstone, like any delete. Every surviving rating is
// touched: the play state it carries may have come over from the merged work.
func ratingMergeStmts(src, dst int64) []mergeStmt {
	upsert, prune := ratingAggregateSQL(true)
	scope := []any{dst}
	return []mergeStmt{
		{`WITH gone AS (DELETE FROM catalog_user_work_rating d USING catalog_user_work_rating s
		      WHERE d.work_id = ? AND s.work_id = ? AND s.actor_uid = d.actor_uid AND s.updated_at > d.updated_at
		      RETURNING d.id, d.work_id)
		  INSERT INTO catalog_user_work_rating_tombstone (id, work_id, deleted_at)
		  SELECT id, work_id, now() FROM gone`, []any{dst, src}, false},
		{`UPDATE catalog_user_work_rating f SET work_id = ?, changed_at = now()
		    WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM catalog_user_work_rating x
		                     WHERE x.work_id = ? AND x.actor_uid = f.actor_uid)`, []any{dst, src, dst}, false},
		{`WITH gone AS (DELETE FROM catalog_user_work_rating WHERE work_id = ? RETURNING id, work_id)
		  INSERT INTO catalog_user_work_rating_tombstone (id, work_id, deleted_at)
		  SELECT id, work_id, now() FROM gone`, []any{src}, false},
		{`UPDATE catalog_user_work_rating SET changed_at = now() WHERE work_id = ?`, []any{dst}, false},
		{upsert, []any{scope, scope}, false},
		{prune, []any{scope}, false},
	}
}

func workFacetRehang(p mergePlan, table string, keyCols []string, user bool) []mergeStmt {
	match := make([]string, 0, len(keyCols))
	for _, c := range keyCols {
		match = append(match, fmt.Sprintf("x.%s = f.%s", c, c))
	}
	move := fmt.Sprintf(
		`UPDATE %s f SET work_id = ? WHERE f.work_id = ?
		    AND NOT EXISTS (SELECT 1 FROM %s x WHERE x.work_id = ? AND %s)`,
		table, table, strings.Join(match, " AND "))
	leftover := archiveDelete(p, table, "t.work_id = ?", p.src)
	if user {
		leftover = mergeStmt{fmt.Sprintf(`DELETE FROM %s WHERE work_id = ?`, table), []any{p.src}, false}
	}
	return []mergeStmt{{move, []any{p.dst, p.src, p.dst}, false}, leftover}
}
