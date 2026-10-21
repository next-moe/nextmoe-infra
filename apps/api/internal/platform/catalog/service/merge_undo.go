package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/settings/keys"

	"gorm.io/gorm"
)

var (
	ErrUndoUnavailable = errors.New("catalog merge: this merge ran before undo was recorded; only the unmerge-work tool can take it back")
	ErrUndoExpired     = errors.New("catalog merge: the undo window for this merge has passed")
	ErrUndoBlocked     = errors.New("catalog merge: the ids have moved on since this merge")
)

// Rows whose other columns a merge rewrites on the way (titles turned into
// aliases, media flagged merged_in and re-sorted, refs demoted): an undo puts
// the whole row back, not just the id it points at.
var undoRestoresWholeRow = map[string]bool{
	"catalog_cover_vote":      true,
	"catalog_work_title":      true,
	"catalog_work_cover":      true,
	"catalog_work_screenshot": true,
	"catalog_external_ref":    true,
	"catalog_name_alias":      true,
	"catalog_label_alias":     true,
	"catalog_character_alias": true,
}

// The survivor's own columns a merge may overwrite from the source
// (applySurvivorship). Each is put back only while it still holds what the
// merge wrote there.
var undoTargetColumns = map[int16][]string{
	model.EntityTypeWork:       {"display_name", "olang"},
	model.EntityTypePerson:     {"display_name", "description", "gender", "birth_y", "birth_m", "birth_d", "primary_credit_name_id"},
	model.EntityTypeCreditName: {"latin", "lang", "note"},
	model.EntityTypeLabel:      {"display_name", "latin", "lang", "note", "logo_hash"},
	model.EntityTypeCharacter:  {"display_name", "latin", "lang", "gender", "description", "instance_of", "image_hash"},
}

type UndoReport struct {
	Restored        map[string]int `json:"restored"`
	Missing         map[string]int `json:"missing"`
	Archive         ArchiveRestore `json:"archive"`
	RevertedFields  []string       `json:"reverted_fields"`
	KeptFields      []string       `json:"kept_fields"`
	RemovedInserted int            `json:"removed_inserted"`
	UnhiddenRelease int            `json:"unhidden_releases"`
}

type movementRow struct {
	Table  string `gorm:"column:table_name"`
	Column string `gorm:"column:column_name"`
	Row    string `gorm:"column:row"`
}

// UndoMerge takes an executed merge back under the source's original id:
// the source row comes back to life, the rows the merge moved go back, the
// rows it archived are restored, the copies it inserted on the survivor are
// removed, and the redirect is revoked so the feed tells mirrors the id is
// live again. Data the two sides shared and the merge folded into one row
// (a user's rating or play state on both works, a character's roster entry)
// stays on the survivor; the report counts what could not come back.
func (s *MergeService) UndoMerge(ctx context.Context, proposalID, actorID int64, reason string) (UndoReport, error) {
	rep := UndoReport{Restored: map[string]int{}, Missing: map[string]int{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := repository.LockProposal(tx, proposalID)
		if err != nil {
			return err
		}
		if p == nil {
			return fmt.Errorf("%w: proposal %d", ErrNotFound, proposalID)
		}
		if p.Status != model.ProposalStatusExecuted {
			return fmt.Errorf("%w: proposal %d is in status %d, want executed", ErrProposalState, proposalID, p.Status)
		}
		ok, err := hasMovements(tx, p.ID)
		if err != nil {
			return err
		}
		if !ok {
			return ErrUndoUnavailable
		}
		days := keys.CatalogMergeUndoWindowDays.Get()
		if days <= 0 || p.ExecutedAt == nil || time.Since(*p.ExecutedAt) > time.Duration(days)*24*time.Hour {
			return ErrUndoExpired
		}
		et, src, dst := p.EntityType, p.SourceEntityID, p.TargetEntityID
		if err := assertUndoable(tx, et, src, dst); err != nil {
			return err
		}
		first, second := min(src, dst), max(src, dst)
		if err := repository.LockEntityRow(tx, et, first); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := repository.LockEntityRow(tx, et, second); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var moves []movementRow
		if err := tx.Raw(`SELECT table_name, column_name, row::text AS row FROM catalog_merge_movement
			WHERE proposal_id = ? ORDER BY id`, p.ID).Scan(&moves).Error; err != nil {
			return err
		}
		byKind := map[string][]movementRow{}
		groups := map[[2]string][]string{}
		for _, m := range moves {
			switch m.Column {
			case movementSource, movementTarget, movementInserted, movementHidden, movementIdentity:
				byKind[m.Column] = append(byKind[m.Column], m)
			default:
				key := [2]string{m.Table, m.Column}
				groups[key] = append(groups[key], m.Row)
			}
		}

		reverted, kept, err := revertTarget(tx, et, dst, byKind[movementSource], byKind[movementTarget])
		if err != nil {
			return fmt.Errorf("revert target: %w", err)
		}
		rep.RevertedFields, rep.KeptFields = reverted, kept
		if err := reviveSource(tx, et, src, byKind[movementSource]); err != nil {
			return fmt.Errorf("revive source: %w", err)
		}

		n, err := removeInserted(tx, byKind[movementInserted])
		if err != nil {
			return fmt.Errorf("remove inserted copies: %w", err)
		}
		rep.RemovedInserted = n

		var touched []int64
		for _, key := range slices.SortedFunc(maps.Keys(groups), func(a, b [2]string) int {
			return strings.Compare(a[0]+"."+a[1], b[0]+"."+b[1])
		}) {
			table, column := key[0], key[1]
			if table == "catalog_redirect" {
				if err := restoreFlattenedRedirects(tx, et, src, dst, groups[key]); err != nil {
					return err
				}
				continue
			}
			if err := moveBack(tx, table, column, src, dst, groups[key]); err != nil {
				return fmt.Errorf("move back %s.%s: %w", table, column, err)
			}
			for _, row := range groups[key] {
				touched = append(touched, rowWorkIDs(row, et, src, src)...)
			}
		}

		archive, archTouched, err := restoreArchive(tx, p, src)
		if err != nil {
			return err
		}
		rep.Archive = archive
		touched = append(touched, archTouched...)

		if err := restoreIdentityKeys(tx, byKind[movementIdentity]); err != nil {
			return fmt.Errorf("restore suppression keys: %w", err)
		}
		if et == model.EntityTypeWork {
			if err := refreshUserAggregates(tx, src, dst); err != nil {
				return fmt.Errorf("refresh user aggregates: %w", err)
			}
		}

		hidden := idsOf(byKind[movementHidden])
		if len(hidden) > 0 {
			res := tx.Exec(`UPDATE catalog_release SET deleted_at = NULL WHERE id IN ? AND work_id = ? AND deleted_at IS NOT NULL`, hidden, src)
			if res.Error != nil {
				return res.Error
			}
			rep.UnhiddenRelease = int(res.RowsAffected)
		}

		for key, rows := range groups {
			if key[0] == "catalog_redirect" {
				continue
			}
			back, err := countOnSource(tx, key[0], key[1], src, rows)
			if err != nil {
				return err
			}
			rep.Restored[key[0]] += back
			if gone := len(rows) - back; gone > 0 {
				rep.Missing[key[0]] += gone
			}
		}

		note := fmt.Sprintf("undo of merge proposal %d", p.ID)
		if err := repository.RevokeRedirect(tx, et, src, note); err != nil {
			return err
		}
		srcSnap, err := takeSnapshot(tx, et, src)
		if err != nil {
			return fmt.Errorf("snapshot source: %w", err)
		}
		if err := writeRevision(tx, et, src, model.RevisionActionReverted, srcSnap, nil, &actorID, note); err != nil {
			return err
		}
		dstSnap, err := takeSnapshot(tx, et, dst)
		if err != nil {
			return fmt.Errorf("snapshot target: %w", err)
		}
		if err := writeRevision(tx, et, dst, model.RevisionActionReverted, dstSnap, nil, &actorID, note); err != nil {
			return err
		}
		if et == model.EntityTypeWork {
			touched = append(touched, src, dst)
		}
		if err := repository.TouchWorks(ctx, tx, touched); err != nil {
			return fmt.Errorf("touch works: %w", err)
		}
		entry := fmt.Sprintf("undone by user %d", actorID)
		if reason = strings.TrimSpace(reason); reason != "" {
			entry += ": " + reason
		}
		return tx.Model(p).Updates(map[string]any{
			"status":    model.ProposalStatusUndone,
			"undone_by": actorID,
			"undone_at": time.Now(),
			"note":      appendNote(p.Note, entry),
		}).Error
	})
	return rep, err
}

func assertUndoable(tx *gorm.DB, et int16, src, dst int64) error {
	if err := assertEntityAlive(tx, et, dst); err != nil {
		return fmt.Errorf("%w: the survivor %d is no longer live (merged on or removed)", ErrUndoBlocked, dst)
	}
	var current *int64
	if err := tx.Raw(`SELECT current_id FROM catalog_redirect
		WHERE entity_type = ? AND old_id = ? AND revoked_at IS NULL`, et, src).Scan(&current).Error; err != nil {
		return err
	}
	if current == nil || *current != dst {
		return fmt.Errorf("%w: %d no longer redirects to %d", ErrUndoBlocked, src, dst)
	}
	return nil
}

func reviveSource(tx *gorm.DB, et int16, src int64, rows []movementRow) error {
	if len(rows) != 1 {
		return fmt.Errorf("expected one recorded source row, found %d", len(rows))
	}
	table, _ := entityTableName(et)
	if et == model.EntityTypeCreditName {
		return tx.Exec(`INSERT INTO catalog_credit_name SELECT * FROM jsonb_populate_record(NULL::catalog_credit_name, ?::jsonb)
			ON CONFLICT (id) DO NOTHING`, rows[0].Row).Error
	}
	set := "deleted_at = NULL"
	if et == model.EntityTypeWork {
		set += ", status = r.status, site = r.site, product_work_id = r.product_work_id, updated_at = now()"
	}
	return tx.Exec(fmt.Sprintf(`UPDATE %[1]s t SET %[2]s
		FROM jsonb_populate_record(NULL::%[1]s, ?::jsonb) r WHERE t.id = ?`, table, set), rows[0].Row, src).Error
}

// revertTarget puts back each survivor column the merge overwrote, as long as
// it still holds the source's value — what applySurvivorship wrote. A work's
// claim follows the same rule: if the merge handed the source's claim to the
// survivor, the survivor gives it back.
func revertTarget(tx *gorm.DB, et int16, dst int64, srcRows, dstRows []movementRow) ([]string, []string, error) {
	if len(srcRows) != 1 || len(dstRows) != 1 {
		return nil, nil, nil
	}
	table, _ := entityTableName(et)
	var before, source, now map[string]any
	if err := json.Unmarshal([]byte(dstRows[0].Row), &before); err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(srcRows[0].Row), &source); err != nil {
		return nil, nil, err
	}
	var current string
	if err := tx.Raw(fmt.Sprintf(`SELECT to_jsonb(t)::text FROM %s t WHERE t.id = ?`, table), dst).Scan(&current).Error; err != nil {
		return nil, nil, err
	}
	if err := json.Unmarshal([]byte(current), &now); err != nil {
		return nil, nil, err
	}
	same := func(a, b any) bool {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return string(x) == string(y)
	}
	columns := slices.Clone(undoTargetColumns[et])
	if et == model.EntityTypeWork && before["site"] == nil && source["site"] != nil && same(now["site"], source["site"]) {
		columns = append(columns, "site", "product_work_id", "status")
	}
	var reverted, kept []string
	updates := map[string]any{}
	for _, col := range columns {
		if same(before[col], now[col]) {
			continue
		}
		if !same(now[col], source[col]) && !slices.Contains([]string{"product_work_id", "status"}, col) {
			kept = append(kept, col)
			continue
		}
		updates[col] = before[col]
		reverted = append(reverted, col)
	}
	if len(updates) == 0 {
		return reverted, kept, nil
	}
	if prov, ok := before["field_provenance"]; ok {
		cur, _ := now["field_provenance"].(map[string]any)
		old, _ := prov.(map[string]any)
		merged := map[string]any{}
		maps.Copy(merged, cur)
		for _, col := range reverted {
			if v, ok := old[col]; ok {
				merged[col] = v
			} else {
				delete(merged, col)
			}
		}
		raw, err := json.Marshal(merged)
		if err != nil {
			return nil, nil, err
		}
		updates["field_provenance"] = string(raw)
	}
	if _, ok := now["updated_at"]; ok {
		updates["updated_at"] = gorm.Expr("now()")
	}
	return reverted, kept, tx.Table(table).Where("id = ?", dst).Updates(updates).Error
}

func removeInserted(tx *gorm.DB, rows []movementRow) (int, error) {
	byTable := map[string][]int64{}
	for _, m := range rows {
		var doc struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal([]byte(m.Row), &doc); err != nil {
			return 0, err
		}
		byTable[m.Table] = append(byTable[m.Table], doc.ID)
	}
	total := 0
	for table, ids := range byTable {
		res := tx.Exec(fmt.Sprintf(`DELETE FROM %s WHERE id IN ?`, table), ids)
		if res.Error != nil {
			return 0, res.Error
		}
		total += int(res.RowsAffected)
	}
	return total, nil
}

func idsOf(rows []movementRow) []int64 {
	var ids []int64
	for _, m := range rows {
		var doc struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal([]byte(m.Row), &doc) == nil && doc.ID > 0 {
			ids = append(ids, doc.ID)
		}
	}
	return ids
}

// moveBack finds each recorded row where the merge left it — same primary
// key, with the moved column now holding the survivor's id — and points it at
// the source again. A row the merge deleted, or that changed its key since,
// is not found and is counted as missing by countOnSource.
func moveBack(tx *gorm.DB, table, column string, src, dst int64, rows []string) error {
	pk, err := primaryKey(tx, table)
	if err != nil {
		return err
	}
	cols, err := tableColumns(tx, table)
	if err != nil {
		return err
	}
	set := []string{fmt.Sprintf(`%q = ?`, column)}
	setArgs := []any{src}
	for _, c := range cols {
		if c == column || slices.Contains(pk, c) {
			continue
		}
		switch {
		case c == "updated_at" || c == "changed_at":
			set = append(set, fmt.Sprintf(`%q = now()`, c))
		case undoRestoresWholeRow[table]:
			set = append(set, fmt.Sprintf(`%[1]q = r.%[1]q`, c))
		}
	}
	match, matchArgs := keyMatch(pk, column, dst)
	payload := "[" + strings.Join(rows, ",") + "]"
	q := fmt.Sprintf(`UPDATE %[1]s t SET %[2]s
		FROM jsonb_populate_recordset(NULL::%[1]s, ?::jsonb) r
		WHERE %[3]s AND t.%[4]q = ?`, table, strings.Join(set, ", "), match, column)
	args := append(append(append(setArgs, payload), matchArgs...), dst)
	err = tx.Transaction(func(sp *gorm.DB) error { return sp.Exec(q, args...).Error })
	if err == nil {
		return nil
	}
	for _, row := range rows {
		one := fmt.Sprintf(`UPDATE %[1]s t SET %[2]s
			FROM jsonb_populate_record(NULL::%[1]s, ?::jsonb) r
			WHERE %[3]s AND t.%[4]q = ?`, table, strings.Join(set, ", "), match, column)
		oneArgs := append(append(append(slices.Clone(setArgs), row), matchArgs...), dst)
		_ = tx.Transaction(func(sp *gorm.DB) error { return sp.Exec(one, oneArgs...).Error })
	}
	return nil
}

func keyMatch(pk []string, column string, moved int64) (string, []any) {
	parts := make([]string, 0, len(pk))
	var args []any
	for _, c := range pk {
		if c == column {
			parts = append(parts, fmt.Sprintf(`t.%q = ?`, c))
			args = append(args, moved)
			continue
		}
		parts = append(parts, fmt.Sprintf(`t.%[1]q = r.%[1]q`, c))
	}
	return strings.Join(parts, " AND "), args
}

func countOnSource(tx *gorm.DB, table, column string, src int64, rows []string) (int, error) {
	pk, err := primaryKey(tx, table)
	if err != nil {
		return 0, err
	}
	match, args := keyMatch(pk, column, src)
	var n int64
	err = tx.Raw(fmt.Sprintf(`SELECT count(*) FROM jsonb_populate_recordset(NULL::%[1]s, ?::jsonb) r
		WHERE EXISTS (SELECT 1 FROM %[1]s t WHERE %[2]s AND t.%[3]q = ?)`, table, match, column),
		append(append([]any{"[" + strings.Join(rows, ",") + "]"}, args...), src)...).Scan(&n).Error
	return int(n), err
}

func restoreIdentityKeys(tx *gorm.DB, rows []movementRow) error {
	if len(rows) == 0 {
		return nil
	}
	parts := make([]string, 0, len(rows))
	for _, m := range rows {
		parts = append(parts, m.Row)
	}
	return tx.Exec(`UPDATE edit_suppressed_row t SET identity_key = r.identity_key
		FROM jsonb_populate_recordset(NULL::edit_suppressed_row, ?::jsonb) r
		WHERE t.id = r.id AND t.identity_key <> r.identity_key
		  AND NOT EXISTS (SELECT 1 FROM edit_suppressed_row x
		                   WHERE x.entity_type = t.entity_type AND x.entity_id = t.entity_id
		                     AND x.field_key = t.field_key AND x.identity_key = r.identity_key)`,
		"["+strings.Join(parts, ",")+"]").Error
}

// A folder's item_count and the site's own rating aggregate both summarize
// rows the undo just moved between the two works.
func refreshUserAggregates(tx *gorm.DB, src, dst int64) error {
	scope := []int64{src, dst}
	upsert, prune := ratingAggregateSQL(true)
	stmts := []mergeStmt{
		{`UPDATE catalog_user_folder f
		    SET item_count = (SELECT count(*) FROM catalog_user_folder_item i WHERE i.folder_id = f.id),
		        updated_at = now()
		    WHERE f.id IN (SELECT folder_id FROM catalog_user_folder_item WHERE work_id IN ?)`, []any{scope}, false},
		{upsert, []any{scope, scope}, false},
		{prune, []any{scope}, false},
	}
	_, err := execAll(tx, stmts)
	return err
}

func restoreFlattenedRedirects(tx *gorm.DB, et int16, src, dst int64, rows []string) error {
	var olds []int64
	for _, row := range rows {
		var doc struct {
			OldID int64 `json:"old_id"`
		}
		if err := json.Unmarshal([]byte(row), &doc); err != nil {
			return err
		}
		olds = append(olds, doc.OldID)
	}
	if len(olds) == 0 {
		return nil
	}
	return tx.Exec(`UPDATE catalog_redirect SET current_id = ?, merged_at = now()
		WHERE entity_type = ? AND old_id IN ? AND current_id = ? AND revoked_at IS NULL`, src, et, olds, dst).Error
}

// UndoDeadlines is UndoDeadline for a page of requests in one query.
func (s *MergeService) UndoDeadlines(ctx context.Context, ps []model.CatalogMergeProposal) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	days := keys.CatalogMergeUndoWindowDays.Get()
	var ids []int64
	for _, p := range ps {
		if p.Status == model.ProposalStatusExecuted && p.ExecutedAt != nil && days > 0 {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var recorded []int64
	if err := s.db.WithContext(ctx).Raw(`SELECT DISTINCT proposal_id FROM catalog_merge_movement
		WHERE proposal_id IN ? AND column_name = ?`, ids, movementSource).Scan(&recorded).Error; err != nil {
		return nil, err
	}
	for _, p := range ps {
		if slices.Contains(recorded, p.ID) {
			out[p.ID] = p.ExecutedAt.Add(time.Duration(days) * 24 * time.Hour)
		}
	}
	return out, nil
}

// MovedCounts is how many rows of each table pointed at the source when the
// merge ran.
func (s *MergeService) MovedCounts(ctx context.Context, proposalID int64) (map[string]int64, error) {
	var rows []struct {
		Table string `gorm:"column:table_name"`
		N     int64  `gorm:"column:n"`
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT table_name, count(*) AS n FROM catalog_merge_movement
		WHERE proposal_id = ? AND column_name NOT LIKE '\_\_%' GROUP BY table_name`, proposalID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.Table] = r.N
	}
	return out, nil
}
