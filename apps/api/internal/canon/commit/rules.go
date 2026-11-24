package commit

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"time"

	"api/internal/canon/ent"
	"api/internal/canon/registry"
)

// inScope is what an actor may write at all, whatever is there now.
func inScope(a Actor, p registry.Property, v Value) string {
	if a.Kind != ActorAgent {
		return ""
	}
	if !p.Agent {
		return "PROPERTY_OUT_OF_SCOPE"
	}
	if structural := p.Shape == registry.ShapeRef && !p.Named; !structural && !containsTier(p.Tiers, v.Tier) {
		return "TIER_NOT_ALLOWED"
	}
	return ""
}

// outranked is the write priority (plan 05 §10): a person's value is not an
// agent's to change, a stronger tier is not a weaker one's, and one tier's
// sources that disagree wait for a decision unless a reviewer made it.
func outranked(a Actor, v Value, prior map[string]any, same bool) string {
	if a.Kind != ActorAgent || prior == nil {
		return ""
	}
	if prior["by"] == string(ActorHuman) {
		return "HUMAN_LOCKED"
	}
	switch was := rankOf(prior); {
	case was < registry.Rank(v.Tier):
		return "STRONGER_VALUE_EXISTS"
	case was == registry.Rank(v.Tier) && !same && a.Reviewer == "":
		if src, _ := prior["source"].(string); src != "" && v.Source != "" && src != v.Source {
			return "SAME_TIER_CONFLICT"
		}
	}
	return ""
}

// rankOf is the rank of a stamped value; one with no tier ranks last, so any
// evidence beats a seed.
func rankOf(prior map[string]any) int {
	was, _ := prior["assurance"].(string)
	return registry.Rank(registry.Tier(was))
}

func priorDetail(prior map[string]any, value any) string {
	if prior == nil {
		return ""
	}
	return fmt.Sprintf("currently %v (%v %v)", value, prior["by"], prior["assurance"])
}

func (a *applier) stamp(v Value) map[string]any {
	st := map[string]any{"by": string(a.cs.Actor.Kind), "key": a.cs.Actor.Key, "at": a.s.now().UTC().Format(time.RFC3339)}
	if v.Tier != "" {
		st["assurance"] = string(v.Tier)
	}
	if v.Source != "" {
		st["source"] = v.Source
	}
	if a.cs.Actor.Reviewer != "" {
		st["reviewed_by"] = a.cs.Actor.Reviewer
	}
	return st
}

// sameValue compares a column as Postgres renders it in JSON with a validated
// value; numeric columns come back as numbers where money is a string.
func sameValue(cur, val any) bool {
	if cur == nil {
		return false
	}
	var norm any
	b, _ := json.Marshal(val)
	_ = json.Unmarshal(b, &norm)
	if reflect.DeepEqual(cur, norm) {
		return true
	}
	x, err1 := strconv.ParseFloat(fmt.Sprint(cur), 64)
	y, err2 := strconv.ParseFloat(fmt.Sprint(norm), 64)
	return err1 == nil && err2 == nil && x == y
}

func rowOf(ctx context.Context, tx *ent.Tx, entity string, id int64) (map[string]any, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT to_jsonb(t) FROM canon.`+entity+` t WHERE id = $1 FOR UPDATE`, id)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, false, rows.Err()
	}
	var b []byte
	if err := rows.Scan(&b); err != nil {
		return nil, false, err
	}
	var row map[string]any
	return row, true, json.Unmarshal(b, &row)
}

func redirected(ctx context.Context, tx *ent.Tx, entity string, id int64) (int64, bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT new_id FROM canon.redirect WHERE entity = $1 AND old_id = $2
		AND revoked_at IS NULL ORDER BY id DESC LIMIT 1`, entity, id)
	if err != nil {
		return 0, false, err
	}
	defer rows.Close()
	var to int64
	if !rows.Next() {
		return 0, false, rows.Err()
	}
	return to, true, rows.Scan(&to)
}

func holder(ctx context.Context, tx *ent.Tx, entity, column string, val any, self int64) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM canon.`+entity+` WHERE `+column+` = $1 AND id <> $2 LIMIT 1`, val, self)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var id int64
	if rows.Next() {
		if err := rows.Scan(&id); err != nil {
			return 0, err
		}
	}
	return id, rows.Err()
}

func isHeld(ctx context.Context, tx *ent.Tx, entity string, id int64) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT 1 FROM canon.hold WHERE entity = $1 AND entity_id = $2 AND released_at IS NULL LIMIT 1`, entity, id)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func containsTier(ts []registry.Tier, t registry.Tier) bool {
	for _, x := range ts {
		if x == t {
			return true
		}
	}
	return false
}

func intOf(v any) int {
	f, _ := v.(float64)
	return int(f)
}

// roots names the parent field a child row shows up under; an entity absent
// here is public on its own.
var roots = map[string][]struct{ entity, field string }{
	"work_title":      {{"work", "WorkID"}},
	"work_part":       {{"work", "WorkID"}},
	"book_work":       {{"work", "WorkID"}},
	"release_content": {{"work", "WorkID"}},
	"book_release":    {{"release", "ReleaseID"}},
	"person_alias":    {{"person", "PersonID"}},
	"org_name":        {{"org", "OrgID"}},
	"credit":          {{"work", "WorkID"}, {"work_part", "PartID"}, {"release", "ReleaseID"}},
}

func rootOf(entity string, id int64, saved ent.Value) (string, int64) {
	v := reflect.Indirect(reflect.ValueOf(saved))
	for _, r := range roots[entity] {
		f := v.FieldByName(r.field)
		if !f.IsValid() {
			continue
		}
		if f.Kind() == reflect.Pointer {
			if f.IsNil() {
				continue
			}
			f = f.Elem()
		}
		if f.CanInt() && f.Int() != 0 {
			return r.entity, f.Int()
		}
	}
	return entity, id
}

func entityID(v ent.Value) int64 {
	f := reflect.Indirect(reflect.ValueOf(v)).FieldByName("ID")
	if f.CanInt() {
		return f.Int()
	}
	return 0
}
