// Package merge folds a duplicate into its survivor (I3: survivor first, the
// loser's id redirects, an unmerge undoes it). What points at the loser is read
// off the schema's own foreign keys, so a table added later is never missed.
package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"api/internal/canon/ent"
	"api/internal/canon/ent/migrate"
	"api/internal/canon/ent/redirect"

	entschema "entgo.io/ent/dialect/sql/schema"
)

// Mergeable is every entity a merge may fold.
var Mergeable = map[string]bool{"work": true, "work_part": true, "release": true, "person": true, "org": true}

// polymorphic lists the tables that name an entity by (entity, id) without a
// foreign key. xref_link follows the merge; revision keeps the loser's history
// under its old id on purpose; todo names subjects by key, not id; an active
// hold or takedown refuses the merge before anything moves.
var polymorphic = []string{"xref_link"}

var (
	ErrSame = errors.New("an entity cannot merge into itself")
	ErrHeld = errors.New("the loser is held or taken down; resolve that first")
)

type ref struct {
	table, column string
	unique        bool
	// peers are the other columns of a composite unique index holding the
	// column: a loser row whose peers match a survivor row cannot move.
	peers [][]string
}

func referencing(entity string) []ref {
	var out []ref
	for _, t := range migrate.Tables {
		for _, fk := range t.ForeignKeys {
			if fk.RefTable.Name != entity || len(fk.Columns) != 1 {
				continue
			}
			c := fk.Columns[0]
			out = append(out, ref{table: t.Name, column: c.Name, unique: c.Unique || uniqueIndex(t, c), peers: compositePeers(t, c)})
		}
	}
	return out
}

func compositePeers(t *entschema.Table, c *entschema.Column) [][]string {
	var out [][]string
	for _, ix := range t.Indexes {
		if !ix.Unique || len(ix.Columns) < 2 {
			continue
		}
		var peers []string
		has := false
		for _, col := range ix.Columns {
			if col.Name == c.Name {
				has = true
			} else {
				peers = append(peers, col.Name)
			}
		}
		if has {
			out = append(out, peers)
		}
	}
	return out
}

func uniqueIndex(t *entschema.Table, c *entschema.Column) bool {
	for _, ix := range t.Indexes {
		if ix.Unique && len(ix.Columns) == 1 && ix.Columns[0].Name == c.Name {
			return true
		}
	}
	return false
}

type Movement struct {
	Table  string          `json:"table"`
	Column string          `json:"column"`
	Action string          `json:"action"`
	Row    json.RawMessage `json:"row"`
}

func Merge(ctx context.Context, client *ent.Client, entity string, loser, survivor int64, by, reason string) (int, error) {
	if !Mergeable[entity] {
		return 0, fmt.Errorf("%s is not mergeable", entity)
	}
	if loser == survivor {
		return 0, ErrSame
	}
	tx, err := client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var loserRow json.RawMessage
	if err := one(ctx, tx, `SELECT to_jsonb(t) FROM canon.`+entity+` t WHERE id = $1 FOR UPDATE`, []any{loser}, &loserRow); err != nil {
		return 0, fmt.Errorf("loser %s %d: %w", entity, loser, err)
	}
	var survivorOK int
	if err := one(ctx, tx, `SELECT 1 FROM canon.`+entity+` WHERE id = $1 FOR UPDATE`, []any{survivor}, &survivorOK); err != nil {
		return 0, fmt.Errorf("survivor %s %d: %w", entity, survivor, err)
	}
	var held int
	if err := one(ctx, tx, `SELECT count(*) FROM canon.hold WHERE entity = $1 AND entity_id = $2 AND released_at IS NULL`, []any{entity, loser}, &held); err != nil {
		return 0, err
	}
	if held > 0 {
		return 0, ErrHeld
	}

	var moves []Movement
	for _, r := range referencing(entity) {
		rows, err := tx.QueryContext(ctx, `SELECT to_jsonb(t) FROM canon.`+r.table+` t WHERE `+r.column+` = $1`, loser)
		if err != nil {
			return 0, err
		}
		var snaps []json.RawMessage
		for rows.Next() {
			var b json.RawMessage
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				return 0, err
			}
			snaps = append(snaps, b)
		}
		rows.Close()
		if len(snaps) == 0 {
			continue
		}
		action := "moved"
		if r.unique {
			var taken int
			if err := one(ctx, tx, `SELECT count(*) FROM canon.`+r.table+` WHERE `+r.column+` = $1`, []any{survivor}, &taken); err != nil {
				return 0, err
			}
			if taken > 0 {
				action = "dropped"
			}
		}
		for _, peers := range r.peers {
			cond := ""
			for _, p := range peers {
				cond += " AND s." + p + " = l." + p
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM canon.`+r.table+` l WHERE l.`+r.column+` = $1
				AND EXISTS (SELECT 1 FROM canon.`+r.table+` s WHERE s.`+r.column+` = $2`+cond+`)`, loser, survivor); err != nil {
				return 0, fmt.Errorf("%s.%s peers: %w", r.table, r.column, err)
			}
		}
		var err2 error
		if action == "dropped" {
			_, err2 = tx.ExecContext(ctx, `DELETE FROM canon.`+r.table+` WHERE `+r.column+` = $1`, loser)
		} else {
			_, err2 = tx.ExecContext(ctx, `UPDATE canon.`+r.table+` SET `+r.column+` = $2 WHERE `+r.column+` = $1`, loser, survivor)
		}
		if err2 != nil {
			return 0, fmt.Errorf("%s.%s: %w", r.table, r.column, err2)
		}
		for _, b := range snaps {
			a := action
			if len(r.peers) > 0 {
				var row struct {
					ID int64 `json:"id"`
				}
				_ = json.Unmarshal(b, &row)
				var n int
				if err := one(ctx, tx, `SELECT count(*) FROM canon.`+r.table+` WHERE id = $1`, []any{row.ID}, &n); err != nil {
					return 0, err
				}
				if n == 0 {
					a = "dropped"
				}
			}
			moves = append(moves, Movement{Table: "canon." + r.table, Column: r.column, Action: a, Row: b})
		}
	}
	for _, t := range polymorphic {
		ids, err := ids(ctx, tx, `UPDATE canon.`+t+` SET entity_id = $3 WHERE entity = $1 AND entity_id = $2 RETURNING id`, entity, loser, survivor)
		if err != nil {
			return 0, err
		}
		for _, id := range ids {
			moves = append(moves, Movement{Table: "canon." + t, Column: "entity_id", Action: "moved", Row: json.RawMessage(fmt.Sprintf(`{"id":%d}`, id))})
		}
	}
	flattened, err := ids(ctx, tx, `UPDATE canon.redirect SET new_id = $3 WHERE entity = $1 AND new_id = $2 AND revoked_at IS NULL RETURNING id`, entity, loser, survivor)
	if err != nil {
		return 0, err
	}
	for _, id := range flattened {
		moves = append(moves, Movement{Table: "canon.redirect", Column: "new_id", Action: "moved", Row: json.RawMessage(fmt.Sprintf(`{"id":%d}`, id))})
	}
	if entity == "work" {
		lib, err := rehangLibrary(ctx, tx, loser, survivor)
		if err != nil {
			return 0, err
		}
		moves = append(moves, lib...)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM canon.`+entity+` WHERE id = $1`, loser); err != nil {
		return 0, fmt.Errorf("delete loser: %w", err)
	}

	var row map[string]any
	_ = json.Unmarshal(loserRow, &row)
	var mv []map[string]any
	b, _ := json.Marshal(moves)
	_ = json.Unmarshal(b, &mv)
	rec, err := tx.MergeRecord.Create().SetEntity(entity).SetLoserID(loser).SetSurvivorID(survivor).SetBy(by).SetReason(reason).
		SetLoserRow(row).SetMovements(mv).Save(ctx)
	if err != nil {
		return 0, err
	}
	if err := tx.Redirect.Create().SetEntity(entity).SetOldID(loser).SetNewID(survivor).SetMergeID(rec.ID).Exec(ctx); err != nil {
		return 0, err
	}
	for _, x := range []struct {
		id     int64
		action string
	}{{loser, "merged_into"}, {survivor, "absorbed"}} {
		if err := tx.Revision.Create().SetEntity(entity).SetEntityID(x.id).SetRootEntity(entity).SetRootID(x.id).SetAction(x.action).SetActorKind("human").
			SetActorKey(by).SetNote(reason).SetChanges(map[string]any{"merge_id": rec.ID, "loser": loser, "survivor": survivor}).Exec(ctx); err != nil {
			return 0, err
		}
	}
	return rec.ID, tx.Commit()
}

// Resolve follows a redirect; ok is false when id was never merged.
func Resolve(ctx context.Context, client *ent.Client, entity string, id int64) (int64, bool, error) {
	r, err := client.Redirect.Query().Where(redirect.Entity(entity), redirect.OldID(id), redirect.RevokedAtIsNil()).
		Order(ent.Desc(redirect.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return r.NewID, true, nil
}

func ids(ctx context.Context, tx *ent.Tx, q string, args ...any) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func one(ctx context.Context, tx *ent.Tx, q string, args []any, dst any) error {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return errors.New("not found")
	}
	return rows.Scan(dst)
}
