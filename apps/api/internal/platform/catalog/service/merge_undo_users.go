package service

import (
	"fmt"
	"strings"
	"time"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

const movementSurvivorUser = "__survivor_user__"

// userCollisionTable is a per-user table a merge folds: where one user has a
// row on both entities the merge keeps or blends one and deletes the other, so
// moving rows back by id cannot undo it.
type userCollisionTable struct {
	table      string
	entityCol  string
	scope      string // extra predicate on both sides, "" when none
	naturalKey []string
	clock      string
	// survivorDeleted: the merge deletes the survivor's row when the source's
	// is newer (ratings). overwrites: the merge rewrites the survivor's row in
	// place (playtime maximum, newer play state).
	survivorDeleted bool
	overwrites      bool
	// parent guards a re-insert against rows the user removed with their
	// container after the merge: a deleted folder, a deleted cover.
	parent string
}

var userCollisionTables = map[int16][]userCollisionTable{
	model.EntityTypeWork: {
		{table: "catalog_user_work_rating", entityCol: "work_id", naturalKey: []string{"actor_uid"}, clock: "changed_at", survivorDeleted: true},
		{table: "catalog_user_folder_item", entityCol: "work_id", naturalKey: []string{"folder_id"}, clock: "updated_at",
			parent: "EXISTS (SELECT 1 FROM catalog_user_folder f WHERE f.id = r.folder_id)"},
		{table: "catalog_user_playtime", entityCol: "work_id", naturalKey: []string{"actor_uid", "client_id"}, clock: "updated_at", overwrites: true},
		{table: "catalog_user_work_state", entityCol: "work_id", naturalKey: []string{"actor_uid"}, clock: "updated_at", overwrites: true},
		{table: "catalog_cover_vote", entityCol: "work_id", naturalKey: []string{"actor_uid"}, clock: "updated_at",
			parent: "EXISTS (SELECT 1 FROM catalog_work_cover c WHERE c.id = r.cover_id)"},
	},
	model.EntityTypeLabel: {
		{table: "catalog_user_entity_follow", entityCol: "entity_id", scope: fmt.Sprintf("entity_type = %d", model.EntityTypeLabel),
			naturalKey: []string{"actor_uid"}},
	},
}

func naturalMatch(a, b string, key []string) string {
	parts := make([]string, 0, len(key))
	for _, c := range key {
		parts = append(parts, fmt.Sprintf(`%s.%q = %s.%q`, a, c, b, c))
	}
	return strings.Join(parts, " AND ")
}

func scoped(t userCollisionTable, alias string) string {
	if t.scope == "" {
		return "true"
	}
	return alias + "." + t.scope
}

// captureSurvivorUserRows records, before the merge runs, the survivor's rows
// that collide with a source row: the merge may delete or overwrite them, and
// they reference the survivor, so the source-side capture never sees them.
func captureSurvivorUserRows(p mergePlan) []mergeStmt {
	var out []mergeStmt
	for _, t := range userCollisionTables[p.entityType] {
		out = append(out, mergeStmt{fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', '%[2]s', to_jsonb(d) FROM %[1]s d
			WHERE d.%[3]q = ? AND %[4]s
			  AND EXISTS (SELECT 1 FROM %[1]s s WHERE s.%[3]q = ? AND %[5]s AND %[6]s)`,
			t.table, movementSurvivorUser, t.entityCol, scoped(t, "d"), scoped(t, "s"), naturalMatch("s", "d", t.naturalKey)),
			[]any{p.proposalID, p.dst, p.src}, false})
	}
	return out
}

// restoreUserCollisions runs after the moved rows are back on the source and
// re-splits only the pairs the merge folded, and only while the row each one
// was folded into still exists: a row the user deleted after the merge, a row
// in a folder they deleted, or any row of an account erased in between (its
// purge removed both halves) stays gone.
func restoreUserCollisions(tx *gorm.DB, et int16, proposalID, src, dst int64, executedAt time.Time) error {
	for _, t := range userCollisionTables[et] {
		cols, err := tableColumns(tx, t.table)
		if err != nil {
			return err
		}
		insertCols := make([]string, 0, len(cols))
		values := make([]string, 0, len(cols))
		for _, c := range cols {
			insertCols = append(insertCols, fmt.Sprintf("%q", c))
			if t.clock != "" && c == t.clock {
				values = append(values, "now()")
			} else {
				values = append(values, fmt.Sprintf("r.%q", c))
			}
		}
		pair := func(row, other string) string {
			return fmt.Sprintf(`FROM catalog_merge_movement mr, jsonb_populate_record(NULL::%[1]s, mr.row) r,
			     catalog_merge_movement mo, jsonb_populate_record(NULL::%[1]s, mo.row) o
			WHERE mr.proposal_id = ? AND mr.table_name = '%[1]s' AND mr.column_name = '%[2]s'
			  AND mo.proposal_id = ? AND mo.table_name = '%[1]s' AND mo.column_name = '%[3]s'
			  AND %[4]s`, t.table, row, other, naturalMatch("r", "o", t.naturalKey))
		}
		gone := func(entity int64) string {
			return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.id = r.id)
			  AND NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.%[2]q = %[3]d AND %[4]s AND %[5]s)`,
				t.table, t.entityCol, entity, scoped(t, "x"), naturalMatch("x", "r", t.naturalKey))
		}
		alive := fmt.Sprintf(`EXISTS (SELECT 1 FROM %s k WHERE k.id = o.id)`, t.table)
		parent := "true"
		if t.parent != "" {
			parent = t.parent
		}
		reinsert := func(row, other string, entity int64) error {
			return tx.Exec(fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s `, t.table, strings.Join(insertCols, ", "), strings.Join(values, ", "))+
				pair(row, other)+" AND "+gone(entity)+" AND "+alive+" AND "+parent,
				proposalID, proposalID).Error
		}
		// A source row the merge deleted because the user also had one on the
		// survivor; the survivor's row is what it folded into.
		if err := reinsert(t.entityCol, movementSurvivorUser, src); err != nil {
			return fmt.Errorf("%s: restore folded source rows: %w", t.table, err)
		}
		// A survivor row the merge deleted because the source's was newer; the
		// moved source row, now back on the source, is what it folded into.
		if t.survivorDeleted {
			if err := reinsert(movementSurvivorUser, t.entityCol, dst); err != nil {
				return fmt.Errorf("%s: restore folded survivor rows: %w", t.table, err)
			}
		}
		if t.overwrites {
			set := make([]string, 0, len(cols))
			for _, c := range cols {
				if c == "id" || c == t.entityCol || c == t.clock {
					continue
				}
				set = append(set, fmt.Sprintf(`%[1]q = r.%[1]q`, c))
			}
			set = append(set, fmt.Sprintf(`%q = now()`, t.clock))
			if err := tx.Exec(fmt.Sprintf(`UPDATE %[1]s x SET %[2]s
				FROM catalog_merge_movement m, jsonb_populate_record(NULL::%[1]s, m.row) r
				WHERE m.proposal_id = ? AND m.table_name = '%[1]s' AND m.column_name = '%[3]s'
				  AND x.id = r.id AND x.%[4]q = ? AND x.updated_at <= ?`,
				t.table, strings.Join(set, ", "), movementSurvivorUser, t.entityCol),
				proposalID, dst, executedAt.Add(time.Minute)).Error; err != nil {
				return fmt.Errorf("%s: restore overwritten survivor rows: %w", t.table, err)
			}
		}
	}
	if et != model.EntityTypeWork {
		return nil
	}
	return tx.Exec(`DELETE FROM catalog_user_work_rating_tombstone t
		USING catalog_merge_movement m
		WHERE m.proposal_id = ? AND m.table_name = 'catalog_user_work_rating'
		  AND (m.row->>'id')::bigint = t.id
		  AND t.deleted_at BETWEEN ?::timestamptz - interval '30 minutes' AND ?::timestamptz + interval '1 minute'
		  AND EXISTS (SELECT 1 FROM catalog_user_work_rating r WHERE r.id = t.id)`,
		proposalID, executedAt, executedAt).Error
}
