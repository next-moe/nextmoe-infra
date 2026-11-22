package merge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/ent/redirect"
)

var ErrUndone = errors.New("merge already undone")

// Unmerge puts a merge back from its record: the loser row returns with its
// id, every moved row points at it again, every dropped row is reinserted, and
// the redirect is revoked. Rows written to the survivor since stay there.
func Unmerge(ctx context.Context, client *ent.Client, mergeID int, by string) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rec, err := tx.MergeRecord.Get(ctx, mergeID)
	if err != nil {
		return err
	}
	if rec.UndoneAt != nil {
		return ErrUndone
	}
	row, _ := json.Marshal(rec.LoserRow)
	if _, err := tx.ExecContext(ctx, `INSERT INTO canon.`+rec.Entity+` SELECT * FROM jsonb_populate_record(NULL::canon.`+rec.Entity+`, $1)`, row); err != nil {
		return fmt.Errorf("restore loser: %w", err)
	}
	for i := len(rec.Movements) - 1; i >= 0; i-- {
		m := rec.Movements[i]
		table, _ := m["table"].(string)
		column, _ := m["column"].(string)
		action, _ := m["action"].(string)
		snap, _ := json.Marshal(m["row"])
		var r struct {
			ID int64 `json:"id"`
		}
		_ = json.Unmarshal(snap, &r)
		var q string
		var args []any
		switch action {
		case "moved":
			to := rec.LoserID
			q, args = `UPDATE canon.`+table+` SET `+column+` = $1 WHERE id = $2 AND `+column+` = $3`, []any{to, r.ID, rec.SurvivorID}
		case "dropped":
			q, args = `INSERT INTO canon.`+table+` SELECT * FROM jsonb_populate_record(NULL::canon.`+table+`, $1) ON CONFLICT DO NOTHING`, []any{snap}
		default:
			continue
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("%s %s: %w", action, table, err)
		}
	}
	now := time.Now()
	if err := tx.Redirect.Update().Where(redirect.MergeID(mergeID), redirect.RevokedAtIsNil()).SetRevokedAt(now).Exec(ctx); err != nil {
		return err
	}
	if err := tx.MergeRecord.UpdateOneID(mergeID).SetUndoneAt(now).SetUndoneBy(by).Exec(ctx); err != nil {
		return err
	}
	for _, x := range []int64{rec.LoserID, rec.SurvivorID} {
		if err := tx.Revision.Create().SetEntity(rec.Entity).SetEntityID(x).SetAction("unmerge").SetActorKind("human").
			SetActorKey(by).SetChanges(map[string]any{"merge_id": mergeID}).Exec(ctx); err != nil {
			return err
		}
	}
	return tx.Commit()
}
