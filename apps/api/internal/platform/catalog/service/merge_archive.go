package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"api/internal/platform/catalog/model"

	"gorm.io/gorm"
)

func archiveDelete(p mergePlan, table, where string, args ...any) mergeStmt {
	return mergeStmt{
		sql: fmt.Sprintf(`WITH gone AS (DELETE FROM %[1]s t WHERE %[2]s RETURNING t.*)
			INSERT INTO catalog_merge_archive (proposal_id, table_name, row)
			SELECT ?, '%[1]s', to_jsonb(g) FROM gone g`, table, where),
		args: append(append([]any{}, args...), p.proposalID),
	}
}

func archiveDeleteReturning(p mergePlan, table, where string, whereArgs []any, returning string, returningArgs ...any) mergeStmt {
	args := append(append([]any{}, whereArgs...), p.proposalID)
	return mergeStmt{
		sql: fmt.Sprintf(`WITH gone AS (DELETE FROM %[1]s t WHERE %[2]s RETURNING t.*),
			archived AS (INSERT INTO catalog_merge_archive (proposal_id, table_name, row)
			             SELECT ?, '%[1]s', to_jsonb(g) FROM gone g)
			SELECT %[3]s FROM gone`, table, where, returning),
		args:         append(args, returningArgs...),
		collectWorks: true,
	}
}

type ArchiveSkip struct {
	ArchiveID int64  `json:"archive_id"`
	Table     string `json:"table"`
	Reason    string `json:"reason"`
}

type ArchiveRestore struct {
	Restored map[string]int `json:"restored"`
	Skipped  []ArchiveSkip  `json:"skipped,omitempty"`
}

const (
	skipCollides      = "collides with a row already there"
	skipSurvivorSide  = "a survivor-side row, not the merged entity's"
	skipVoteElsewhere = "its cover is not on the rebuilt work"
)

func restoreArchive(tx *gorm.DB, p *model.CatalogMergeProposal, newID int64) (ArchiveRestore, []int64, error) {
	out := ArchiveRestore{Restored: map[string]int{}}
	var rows []struct {
		ID    int64  `gorm:"column:id"`
		Table string `gorm:"column:table_name"`
		Row   string `gorm:"column:row"`
	}
	if err := tx.Raw(`SELECT id, table_name, row::text AS row FROM catalog_merge_archive
		WHERE proposal_id = ? ORDER BY (table_name = 'catalog_cover_vote'), id`, p.ID).Scan(&rows).Error; err != nil {
		return out, nil, err
	}
	cols := map[string][]string{}
	var touched []int64
	for _, r := range rows {
		if _, ok := cols[r.Table]; !ok {
			var names []string
			if err := tx.Raw(`SELECT column_name FROM information_schema.columns
				WHERE table_schema = current_schema() AND table_name = ? AND is_generated = 'NEVER'
				ORDER BY ordinal_position`, r.Table).Scan(&names).Error; err != nil {
				return out, nil, err
			}
			cols[r.Table] = names
		}
		if r.Table == suppressedRowTable {
			ok, err := restoreSuppressedRow(tx, r.Row, p, newID)
			if err != nil {
				return out, nil, fmt.Errorf("restore archive row %d (%s): %w", r.ID, r.Table, err)
			}
			if !ok {
				out.Skipped = append(out.Skipped, ArchiveSkip{r.ID, r.Table, skipCollides})
				continue
			}
			out.Restored[r.Table]++
			continue
		}
		refs := restoreRefs(r.Table, p.EntityType)
		if len(refs) == 0 || len(cols[r.Table]) == 0 {
			out.Skipped = append(out.Skipped, ArchiveSkip{r.ID, r.Table, skipSurvivorSide})
			continue
		}
		ok, reason, err := restoreRow(tx, r.Table, cols[r.Table], refs, r.Row, p, newID)
		if err != nil {
			return out, nil, fmt.Errorf("restore archive row %d (%s): %w", r.ID, r.Table, err)
		}
		if !ok {
			out.Skipped = append(out.Skipped, ArchiveSkip{r.ID, r.Table, reason})
			continue
		}
		out.Restored[r.Table]++
		touched = append(touched, rowWorkIDs(r.Row, p.EntityType, p.SourceEntityID, newID)...)
	}
	return out, touched, nil
}

const suppressedRowTable = "edit_suppressed_row"

// A moved suppression row still owns the id its archived copy carries, so the
// id-preserving restoreRow would skip every one of them as a collision.
func restoreSuppressedRow(tx *gorm.DB, row string, p *model.CatalogMergeProposal, newID int64) (bool, error) {
	res := tx.Exec(`INSERT INTO edit_suppressed_row (entity_type, entity_id, field_key, identity_key, created_at)
		SELECT t.entity_type, ?, t.field_key, t.identity_key, t.created_at
		FROM jsonb_populate_record(NULL::edit_suppressed_row, ?::jsonb) t
		WHERE t.entity_id = ?
		ON CONFLICT DO NOTHING`, newID, row, p.SourceEntityID)
	return res.RowsAffected > 0, res.Error
}

func restoreRefs(table string, entityType int16) []mergeRef {
	var out []mergeRef
	for _, r := range mergeRefs {
		if r.table != table {
			continue
		}
		if r.families == nil {
			out = append(out, r)
			continue
		}
		for _, f := range r.families {
			if f == entityType {
				out = append(out, r)
				break
			}
		}
	}
	return out
}

func restoreRow(tx *gorm.DB, table string, cols []string, refs []mergeRef, row string,
	p *model.CatalogMergeProposal, newID int64) (bool, string, error) {
	src := p.SourceEntityID
	var matchSQL []string
	var matchArgs []any
	rewrite := map[string]mergeRef{}
	for _, r := range refs {
		matchSQL = append(matchSQL, fmt.Sprintf(`(t.%q = ? AND (%s) = ?)`, r.column, r.typeSQL))
		matchArgs = append(matchArgs, src, p.EntityType)
		rewrite[r.column] = r
	}
	var loserSide bool
	if err := tx.Raw(fmt.Sprintf(`SELECT %s FROM jsonb_populate_record(NULL::%s, ?::jsonb) t`,
		strings.Join(matchSQL, " OR "), table), append(matchArgs, row)...).Scan(&loserSide).Error; err != nil {
		return false, "", err
	}
	if !loserSide {
		return false, skipSurvivorSide, nil
	}
	if table == "catalog_cover_vote" {
		var onWork bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM catalog_work_cover c
			JOIN jsonb_populate_record(NULL::catalog_cover_vote, ?::jsonb) t ON t.cover_id = c.id
			WHERE c.work_id = ?)`, row, newID).Scan(&onWork).Error; err != nil {
			return false, "", err
		}
		if !onWork {
			return false, skipVoteElsewhere, nil
		}
	}
	quoted := make([]string, len(cols))
	sel := make([]string, len(cols))
	var selArgs []any
	for i, c := range cols {
		quoted[i] = fmt.Sprintf("%q", c)
		r, ok := rewrite[c]
		if !ok {
			sel[i] = "t." + quoted[i]
			continue
		}
		sel[i] = fmt.Sprintf(`CASE WHEN t.%[1]q = ? AND (%[2]s) = ? THEN ? ELSE t.%[1]q END`, c, r.typeSQL)
		selArgs = append(selArgs, src, p.EntityType, newID)
	}
	q := fmt.Sprintf(`INSERT INTO %s (%s) SELECT %s FROM jsonb_populate_record(NULL::%s, ?::jsonb) t ON CONFLICT DO NOTHING`,
		table, strings.Join(quoted, ", "), strings.Join(sel, ", "), table)

	var inserted int64
	if err := tx.Transaction(func(sp *gorm.DB) error {
		res := sp.Exec(q, append(selArgs, row)...)
		inserted = res.RowsAffected
		return res.Error
	}); err != nil {
		return false, err.Error(), nil
	}
	if inserted == 0 {
		return false, skipCollides, nil
	}
	if table == "catalog_external_ref" {
		if err := dropAbsorbedCopy(tx, row, newID); err != nil {
			return false, "", err
		}
	}
	return true, "", nil
}

func dropAbsorbedCopy(tx *gorm.DB, row string, newID int64) error {
	return tx.Exec(`DELETE FROM catalog_external_ref r USING jsonb_populate_record(NULL::catalog_external_ref, ?::jsonb) t
		WHERE r.entity_type = t.entity_type AND r.source_id = t.source_id AND r.external_id = t.external_id
		  AND r.entity_id <> ? AND r.matched_by = ?`, row, newID, absorbMatchedBy).Error
}

func rowWorkIDs(row string, entityType int16, src, newID int64) []int64 {
	var doc map[string]any
	if json.Unmarshal([]byte(row), &doc) != nil {
		return nil
	}
	var out []int64
	for _, k := range []string{"work_id", "a_work_id", "b_work_id"} {
		v, ok := doc[k].(float64)
		if !ok {
			continue
		}
		id := int64(v)
		if entityType == model.EntityTypeWork && id == src {
			id = newID
		}
		out = append(out, id)
	}
	return out
}
