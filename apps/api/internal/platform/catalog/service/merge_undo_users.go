package service

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

const movementSurvivorUser = "__survivor_user__"

// userCollisionTable is a per-user table a work merge folds: where one user has
// a row on both works the merge keeps or blends one and deletes the other, so
// moving rows back by id cannot restore it. naturalKey is the unique key minus
// work_id; clock is the column mirrors sync on.
type userCollisionTable struct {
	table      string
	naturalKey []string
	clock      string
}

var userCollisionTables = []userCollisionTable{
	{"catalog_user_work_rating", []string{"actor_uid"}, "changed_at"},
	{"catalog_user_folder_item", []string{"folder_id"}, "updated_at"},
	{"catalog_user_playtime", []string{"actor_uid", "client_id"}, "updated_at"},
	{"catalog_user_work_state", []string{"actor_uid"}, "updated_at"},
	{"catalog_cover_vote", []string{"actor_uid"}, "updated_at"},
}

func naturalMatch(alias, other string, key []string) string {
	parts := make([]string, 0, len(key))
	for _, c := range key {
		parts = append(parts, fmt.Sprintf(`%s.%q = %s.%q`, alias, c, other, c))
	}
	return strings.Join(parts, " AND ")
}

// captureSurvivorUserRows records, before a work merge runs, the survivor's
// rows that the merge will fold with a source row: the merge may delete or
// overwrite them, and they reference the survivor, so the source-side capture
// never sees them.
func captureSurvivorUserRows(p mergePlan) []mergeStmt {
	out := make([]mergeStmt, 0, len(userCollisionTables))
	for _, t := range userCollisionTables {
		out = append(out, mergeStmt{fmt.Sprintf(`INSERT INTO catalog_merge_movement (proposal_id, table_name, column_name, row)
			SELECT ?, '%[1]s', '%[2]s', to_jsonb(d) FROM %[1]s d
			WHERE d.work_id = ? AND EXISTS (SELECT 1 FROM %[1]s s WHERE s.work_id = ? AND %[3]s)`,
			t.table, movementSurvivorUser, naturalMatch("s", "d", t.naturalKey)),
			[]any{p.proposalID, p.dst, p.src}, false})
	}
	return out
}

// restoreUserCollisions runs after moveBack: rows the merge moved are already
// on the source. What is left is what the fold destroyed — a source row
// deleted because the user also had one on the survivor, a survivor row
// deleted because the source one was newer, and a survivor row the merge
// overwrote (playtime maximum, newer play state). Each comes back from its
// recorded copy, under its own id where that id is free. A survivor row the
// user changed after the merge keeps the user's newer state.
func restoreUserCollisions(tx *gorm.DB, proposalID, src, dst int64, executedAt time.Time) (int, error) {
	restored := 0
	for _, t := range userCollisionTables {
		cols, err := tableColumns(tx, t.table)
		if err != nil {
			return restored, err
		}
		quoted := make([]string, 0, len(cols))
		for _, c := range cols {
			quoted = append(quoted, fmt.Sprintf("%q", c))
		}
		colList := strings.Join(quoted, ", ")
		selected := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == t.clock {
				selected = append(selected, "now()")
				continue
			}
			selected = append(selected, fmt.Sprintf("r.%q", c))
		}
		reinsert := func(marker string, work int64) (int64, error) {
			res := tx.Exec(fmt.Sprintf(`INSERT INTO %[1]s (%[2]s)
				SELECT %[3]s FROM catalog_merge_movement m,
				     jsonb_populate_record(NULL::%[1]s, m.row) r
				WHERE m.proposal_id = ? AND m.table_name = '%[1]s' AND m.column_name = ?
				  AND r.work_id = ?
				  AND NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.work_id = r.work_id AND %[4]s)
				  AND NOT EXISTS (SELECT 1 FROM %[1]s x WHERE x.id = r.id)`,
				t.table, colList, strings.Join(selected, ", "), naturalMatch("x", "r", t.naturalKey)),
				proposalID, marker, work)
			return res.RowsAffected, res.Error
		}
		n, err := reinsert("work_id", src)
		if err != nil {
			return restored, fmt.Errorf("%s: restore source rows: %w", t.table, err)
		}
		restored += int(n)
		n, err = reinsert(movementSurvivorUser, dst)
		if err != nil {
			return restored, fmt.Errorf("%s: restore survivor rows: %w", t.table, err)
		}
		restored += int(n)

		set := make([]string, 0, len(cols))
		for _, c := range cols {
			if c == "id" || c == "work_id" || c == t.clock {
				continue
			}
			set = append(set, fmt.Sprintf(`%[1]q = r.%[1]q`, c))
		}
		set = append(set, fmt.Sprintf(`%q = now()`, t.clock))
		res := tx.Exec(fmt.Sprintf(`UPDATE %[1]s x SET %[2]s
			FROM catalog_merge_movement m, jsonb_populate_record(NULL::%[1]s, m.row) r
			WHERE m.proposal_id = ? AND m.table_name = '%[1]s' AND m.column_name = '%[3]s'
			  AND x.id = r.id AND x.work_id = ? AND x.updated_at <= ?`,
			t.table, strings.Join(set, ", "), movementSurvivorUser),
			proposalID, dst, executedAt.Add(time.Minute))
		if res.Error != nil {
			return restored, fmt.Errorf("%s: restore overwritten survivor rows: %w", t.table, res.Error)
		}
		restored += int(res.RowsAffected)
	}
	err := tx.Exec(`DELETE FROM catalog_user_work_rating_tombstone t
		WHERE t.work_id IN (?, ?) AND EXISTS (SELECT 1 FROM catalog_user_work_rating r WHERE r.id = t.id)`, src, dst).Error
	return restored, err
}
