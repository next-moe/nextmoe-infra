package merge

import (
	"context"
	"encoding/json"
	"fmt"

	"api/internal/canon/ent"
)

// libraryTables are the user-state tables keyed by work_id that live with the
// legacy catalog until P5 and carry no FK to canon.work (10 §3), so the
// FK walk cannot see them. Their rules are the legacy merge's (merge_work.go):
// browsing history (catalog_user_work_view) stays on the old id on purpose,
// play time is game-only and FK-bound to catalog_work.
var libraryTables = []string{"public.catalog_user_folder_item", "public.catalog_user_work_state", "public.catalog_user_work_rating"}

// libraryStatements fold the loser's user rows into the survivor's: a row the
// survivor lacks moves; where a user holds both, the later intent wins; a
// folder keeps one membership with its note and cover; a removed rating
// leaves a tombstone, as any rating delete does.
func libraryStatements(src, dst int64) []struct {
	q    string
	args []any
} {
	return []struct {
		q    string
		args []any
	}{
		{`UPDATE public.catalog_user_folder_item f SET work_id = $1, updated_at = now() WHERE f.work_id = $2
			AND NOT EXISTS (SELECT 1 FROM public.catalog_user_folder_item x WHERE x.work_id = $1 AND x.folder_id = f.folder_id)`, []any{dst, src}},
		{`UPDATE public.catalog_user_folder_item d SET note = s.note, updated_at = now() FROM public.catalog_user_folder_item s
			WHERE d.work_id = $1 AND s.work_id = $2 AND s.folder_id = d.folder_id AND d.note = '' AND s.note <> ''`, []any{dst, src}},
		{`WITH gone AS (DELETE FROM public.catalog_user_folder_item WHERE work_id = $2 RETURNING folder_id, is_cover)
			UPDATE public.catalog_user_folder_item d SET is_cover = true, updated_at = now()
			FROM gone g WHERE g.is_cover AND d.folder_id = g.folder_id AND d.work_id = $1`, []any{dst, src}},
		{`UPDATE public.catalog_user_folder f SET item_count = (SELECT count(*) FROM public.catalog_user_folder_item i WHERE i.folder_id = f.id), updated_at = now()
			WHERE f.id IN (SELECT folder_id FROM public.catalog_user_folder_item WHERE work_id = $1)`, []any{dst}},
		{`UPDATE public.catalog_user_work_state d SET state = s.state, completion = s.completion, updated_at = now()
			FROM public.catalog_user_work_state s WHERE d.work_id = $1 AND s.work_id = $2 AND s.actor_uid = d.actor_uid AND s.updated_at > d.updated_at`, []any{dst, src}},
		{`UPDATE public.catalog_user_work_state f SET work_id = $1, updated_at = now() WHERE f.work_id = $2
			AND NOT EXISTS (SELECT 1 FROM public.catalog_user_work_state x WHERE x.work_id = $1 AND x.actor_uid = f.actor_uid)`, []any{dst, src}},
		{`DELETE FROM public.catalog_user_work_state WHERE work_id = $1`, []any{src}},
		{`WITH gone AS (DELETE FROM public.catalog_user_work_rating d USING public.catalog_user_work_rating s
				WHERE d.work_id = $1 AND s.work_id = $2 AND s.actor_uid = d.actor_uid AND s.updated_at > d.updated_at RETURNING d.id, d.work_id)
			INSERT INTO public.catalog_user_work_rating_tombstone (id, work_id, deleted_at) SELECT id, work_id, now() FROM gone`, []any{dst, src}},
		{`UPDATE public.catalog_user_work_rating f SET work_id = $1, changed_at = now() WHERE f.work_id = $2
			AND NOT EXISTS (SELECT 1 FROM public.catalog_user_work_rating x WHERE x.work_id = $1 AND x.actor_uid = f.actor_uid)`, []any{dst, src}},
		{`WITH gone AS (DELETE FROM public.catalog_user_work_rating WHERE work_id = $1 RETURNING id, work_id)
			INSERT INTO public.catalog_user_work_rating_tombstone (id, work_id, deleted_at) SELECT id, work_id, now() FROM gone`, []any{src}},
	}
}

// rehangLibrary snapshots the loser's user rows, applies the rules, then
// records each row as moved (it now points at the survivor) or dropped.
func rehangLibrary(ctx context.Context, tx *ent.Tx, src, dst int64) ([]Movement, error) {
	var present bool
	if err := one(ctx, tx, `SELECT to_regclass('public.catalog_user_work_state') IS NOT NULL`, nil, &present); err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	before := map[string][]json.RawMessage{}
	for _, t := range libraryTables {
		rows, err := tx.QueryContext(ctx, `SELECT to_jsonb(t) FROM `+t+` t WHERE work_id = $1`, src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", t, err)
		}
		for rows.Next() {
			var b json.RawMessage
			if err := rows.Scan(&b); err != nil {
				rows.Close()
				return nil, err
			}
			before[t] = append(before[t], b)
		}
		rows.Close()
	}
	for _, s := range libraryStatements(src, dst) {
		if _, err := tx.ExecContext(ctx, s.q, s.args...); err != nil {
			return nil, fmt.Errorf("library rehang: %w", err)
		}
	}
	var moves []Movement
	for _, t := range libraryTables {
		for _, b := range before[t] {
			var r struct {
				ID int64 `json:"id"`
			}
			_ = json.Unmarshal(b, &r)
			var n int
			if err := one(ctx, tx, `SELECT count(*) FROM `+t+` WHERE id = $1 AND work_id = $2`, []any{r.ID, dst}, &n); err != nil {
				return nil, err
			}
			action := "dropped"
			if n > 0 {
				action = "moved"
			}
			moves = append(moves, Movement{Table: t, Column: "work_id", Action: action, Row: b})
		}
	}
	return moves, nil
}
