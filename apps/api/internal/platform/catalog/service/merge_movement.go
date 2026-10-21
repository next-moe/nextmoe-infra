package service

import (
	"fmt"
	"slices"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const (
	movementSource   = "__source__"
	movementTarget   = "__target__"
	movementInserted = "__inserted__"
	movementHidden   = "__hidden__"
	movementIdentity = "__identity__"
	// A row whose key, with the moved column set to the survivor, already
	// exists there is not moved: the merge folds it into the survivor's row
	// and archives it. Undo must leave the survivor's row alone and bring this
	// one back from the archive, so it is recorded under its own marker.
	movementFolded = "~"
)

// movementRefs are the mergeRefs that point at an entity of this family:
// exactly the columns a merge of that family rewrites.
func movementRefs(entityType int16) []mergeRef {
	var out []mergeRef
	for _, r := range mergeRefs {
		if r.families == nil || slices.Contains(r.families, entityType) {
			out = append(out, r)
		}
	}
	return out
}

// captureMovements runs inside ExecuteMerge before anything changes, so the
// recorded rows are the source's state as it stood.
func captureMovements(tx *gorm.DB, p mergePlan) error {
	table, ok := entityTableName(p.entityType)
	if !ok {
		return fmt.Errorf("catalog merge: unsupported entity type %d", p.entityType)
	}
	stmts := []mergeStmt{
		{fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', '%[2]s', to_jsonb(t) FROM %[1]s t WHERE t.id = ?`, table, movementSource),
			[]any{p.proposalID, p.src}, false},
		{fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', '%[2]s', to_jsonb(t) FROM %[1]s t WHERE t.id = ?`, table, movementTarget),
			[]any{p.proposalID, p.dst}, false},
		{`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, 'catalog_redirect', 'current_id', to_jsonb(t) FROM catalog_redirect t
			WHERE t.entity_type = ? AND t.current_id = ? AND t.revoked_at IS NULL`,
			[]any{p.proposalID, p.entityType, p.src}, false},
		// Suppression identity keys embed merge-able ids and identityFollowStmts
		// rewrites them; keys are colon-separated, so this is every key that can
		// name the source. Undo restores a recorded key only where it changed.
		{`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, 'edit_suppressed_row', '` + movementIdentity + `', to_jsonb(t) FROM edit_suppressed_row t
			WHERE t.identity_key ~ ('(^|:)' || ? || '(:|$)')`,
			[]any{p.proposalID, fmt.Sprint(p.src)}, false},
	}
	for _, r := range movementRefs(p.entityType) {
		pk, err := primaryKey(tx, r.table)
		if err != nil {
			return err
		}
		insert := fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', ?, to_jsonb(t) FROM %[1]s t WHERE t.%[2]q = ? AND (%[3]s) = ?`, r.table, r.column, r.typeSQL)
		if !slices.Contains(pk, r.column) {
			stmts = append(stmts, mergeStmt{insert, []any{p.proposalID, r.column, p.src, p.entityType}, false})
			continue
		}
		match, args := survivorKeyMatch(pk, r.column, p.dst)
		collides := fmt.Sprintf(`EXISTS (SELECT 1 FROM %s d WHERE %s)`, r.table, match)
		stmts = append(stmts,
			mergeStmt{insert + " AND NOT " + collides, append([]any{p.proposalID, r.column, p.src, p.entityType}, args...), false},
			mergeStmt{insert + " AND " + collides, append([]any{p.proposalID, movementFolded + r.column, p.src, p.entityType}, args...), false},
		)
	}
	stmts = append(stmts, captureSurvivorUserRows(p)...)
	_, err := execAll(tx, stmts)
	return err
}

func survivorKeyMatch(pk []string, column string, dst int64) (string, []any) {
	parts := make([]string, 0, len(pk))
	var args []any
	for _, c := range pk {
		if c == column {
			parts = append(parts, fmt.Sprintf(`d.%q = ?`, c))
			args = append(args, dst)
			continue
		}
		parts = append(parts, fmt.Sprintf(`d.%[1]q = t.%[1]q`, c))
	}
	return strings.Join(parts, " AND "), args
}

// recordInserted wraps an INSERT … RETURNING id so the rows a merge writes on
// the survivor (language-less title and alias copies) are known to an undo.
func recordInserted(p mergePlan, table, insertSQL string, args []any) mergeStmt {
	return mergeStmt{
		sql: fmt.Sprintf(`WITH ins AS (%s RETURNING id)
			INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%s', '%s', jsonb_build_object('id', id) FROM ins`, insertSQL, table, movementInserted),
		args: append(append([]any{}, args...), p.proposalID),
	}
}

func hideReleasesStmt(p mergePlan) mergeStmt {
	return mergeStmt{
		sql: fmt.Sprintf(`WITH hid AS (UPDATE catalog_release SET deleted_at = now()
			    WHERE work_id = ? AND id IN ? AND deleted_at IS NULL RETURNING id)
			INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, 'catalog_release', '%s', jsonb_build_object('id', id) FROM hid`, movementHidden),
		args: []any{p.dst, p.hideReleases, p.proposalID},
	}
}

func hasMovements(db *gorm.DB, proposalID int64) (bool, error) {
	var ok bool
	err := db.Raw(`SELECT EXISTS (SELECT 1 FROM catalog_merge_movement WHERE proposal_id = ? AND column_name = ?)`,
		proposalID, movementSource).Scan(&ok).Error
	return ok, err
}

func primaryKey(tx *gorm.DB, table string) ([]string, error) {
	var cols []string
	err := tx.Raw(`SELECT a.attname FROM pg_index i
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE i.indrelid = ?::regclass AND i.indisprimary
		ORDER BY array_position(i.indkey, a.attnum)`, table).Scan(&cols).Error
	if err == nil && len(cols) == 0 {
		err = fmt.Errorf("catalog undo: %s has no primary key", table)
	}
	return cols, err
}

func tableColumns(tx *gorm.DB, table string) ([]string, error) {
	var cols []string
	err := tx.Raw(`SELECT column_name FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = ? AND is_generated = 'NEVER'
		ORDER BY ordinal_position`, table).Scan(&cols).Error
	return cols, err
}

// movementKeys names every table a merge records in the editor's vocabulary;
// several tables share a key (the three alias tables are all "aliases").
// TestMovementKeysCoverEveryRecordedTable fails when mergeRefs grows a table
// this map does not name.
var movementKeys = map[string]string{
	"catalog_release":                       "releases",
	"catalog_work_title":                    "titles",
	"catalog_work_intro":                    "intros",
	"catalog_character_intro":               "intros",
	"catalog_person_intro":                  "intros",
	"catalog_label_intro":                   "intros",
	"catalog_work_tag":                      "tags",
	"catalog_credit":                        "credits",
	"catalog_work_character":                "characters",
	"catalog_character":                     "character_instances",
	"catalog_work_label":                    "companies",
	"catalog_release_label":                 "release_companies",
	"catalog_work_relation":                 "relations",
	"catalog_entity_relation":               "entity_relations",
	"catalog_label_relation":                "company_relations",
	"catalog_series_member":                 "series",
	"catalog_work_cover":                    "covers",
	"catalog_work_screenshot":               "screenshots",
	"catalog_work_platform":                 "platforms",
	"catalog_work_engine":                   "engines",
	"catalog_external_ref":                  "anchors",
	"catalog_entity_usage":                  "site_usage",
	"catalog_work_rating":                   "source_ratings",
	"catalog_work_popularity":               "popularity",
	"catalog_work_playtime":                 "source_playtime",
	"catalog_user_work_rating":              "user_ratings",
	"catalog_user_folder_item":              "user_folders",
	"catalog_user_work_state":               "user_states",
	"catalog_user_playtime":                 "user_playtime",
	"catalog_cover_vote":                    "cover_votes",
	"catalog_user_entity_follow":            "follows",
	"edit_suppressed_row":                   "hidden_rows",
	"catalog_character_alias":               "aliases",
	"catalog_label_alias":                   "aliases",
	"catalog_name_alias":                    "aliases",
	"catalog_character_intro_panel_verdict": "intro_checks",
	"catalog_character_trait_link":          "traits",
	"catalog_credit_name":                   "credit_names",
	"catalog_person":                        "persons",
	"catalog_redirect":                      "redirects",
}

// MovementKey is the editor-facing name of a recorded table.
func MovementKey(table string) string {
	if key, ok := movementKeys[table]; ok {
		return key
	}
	return table
}

// MovementTables lists every table a merge of any family can record rows of.
func MovementTables() []string {
	seen := map[string]bool{"catalog_redirect": true}
	for _, et := range []int16{model.EntityTypeWork, model.EntityTypeCharacter, model.EntityTypeLabel,
		model.EntityTypePerson, model.EntityTypeCreditName} {
		for _, r := range movementRefs(et) {
			seen[r.table] = true
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	slices.Sort(out)
	return out
}
