package service

import (
	"fmt"
	"slices"

	"gorm.io/gorm"
)

const (
	movementSource   = "__source__"
	movementTarget   = "__target__"
	movementInserted = "__inserted__"
	movementHidden   = "__hidden__"
	movementIdentity = "__identity__"
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
		stmts = append(stmts, mergeStmt{fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', '%[2]s', to_jsonb(t) FROM %[1]s t WHERE t.%[2]q = ? AND (%[3]s) = ?`, r.table, r.column, r.typeSQL),
			[]any{p.proposalID, p.src, p.entityType}, false})
	}
	_, err := execAll(tx, stmts)
	return err
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
