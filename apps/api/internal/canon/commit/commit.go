// Package commit is canon's single write path (plan 07 §1): every change,
// from fanin, the editor or a migration, is validated against the registry
// and applied in one transaction, all of a changeset or none of it.
package commit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/registry"
	"api/internal/canon/verify"

	"github.com/jackc/pgx/v5/pgconn"
)

type ActorKind string

const (
	ActorAgent  ActorKind = "agent"
	ActorHuman  ActorKind = "human"
	ActorSystem ActorKind = "system"
)

type Actor struct {
	Kind ActorKind
	Key  string
}

type EvidenceRef struct {
	Snapshot string `json:"snapshot,omitempty"`
	LineFrom int    `json:"line_from,omitempty"`
	LineTo   int    `json:"line_to,omitempty"`
	Record   string `json:"record,omitempty"`
	Path     string `json:"path,omitempty"`
}

type Value struct {
	Raw      json.RawMessage
	Tier     registry.Tier
	Evidence []EvidenceRef
}

// Change creates an entity when ID is zero; Ref names it for later changes
// of the same changeset, which point at it as "ref:<Ref>".
type Change struct {
	Ref    string
	Entity string
	ID     int64
	Values map[string]Value
}

type Changeset struct {
	Actor   Actor
	Note    string
	Changes []Change
	// Key, when set, makes Apply idempotent: a second Apply with the same key
	// writes nothing and returns the first result.
	Key string
}

type Result struct {
	IDs      map[string]int64
	Replayed bool
}

type Refusal struct {
	Change   int
	Property string
	Code     string
	Detail   string
}

type Refused []Refusal

func (r Refused) Error() string {
	parts := make([]string, len(r))
	for i, x := range r {
		parts[i] = fmt.Sprintf("change %d %s: %s %s", x.Change, x.Property, x.Code, x.Detail)
	}
	return strings.Join(parts, "; ")
}

type Service struct {
	client *ent.Client
	index  map[string]map[string]registry.Property
	now    func() time.Time
}

func New(client *ent.Client) *Service {
	return &Service{client: client, index: contract.Index(), now: time.Now}
}

func (s *Service) Apply(ctx context.Context, cs Changeset) (Result, error) {
	res := Result{IDs: map[string]int64{}}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	if cs.Key != "" {
		prior, err := tx.CommitKey.Get(ctx, cs.Key)
		if err == nil {
			return Result{IDs: prior.Ids, Replayed: true}, nil
		}
		if !ent.IsNotFound(err) {
			return res, err
		}
	}
	for i, ch := range cs.Changes {
		if err := s.apply(ctx, tx, cs, i, ch, res.IDs); err != nil {
			return Result{}, err
		}
	}
	if cs.Key != "" {
		if err := tx.CommitKey.Create().SetID(cs.Key).SetIds(res.IDs).Exec(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return Result{}, uniqueRefusal(err, -1)
	}
	return res, nil
}

func (s *Service) apply(ctx context.Context, tx *ent.Tx, cs Changeset, i int, ch Change, ids map[string]int64) error {
	b, ok := builders[ch.Entity]
	props := s.index[ch.Entity]
	if !ok || props == nil {
		return Refused{{Change: i, Code: "PROPERTY_UNKNOWN", Detail: "unknown entity " + ch.Entity}}
	}
	if cs.Actor.Kind == ActorAgent && ch.ID != 0 {
		if held, err := isHeld(ctx, tx, ch.Entity, ch.ID); err != nil {
			return err
		} else if held {
			return Refused{{Change: i, Code: "ENTITY_HELD", Detail: fmt.Sprintf("%s %d is held", ch.Entity, ch.ID)}}
		}
	}
	var state map[string]any
	version := 0
	if ch.ID != 0 && b.stamped {
		var raw []byte
		rows, err := tx.QueryContext(ctx, `SELECT field_state, version FROM canon.`+ch.Entity+` WHERE id = $1 FOR UPDATE`, ch.ID)
		if err != nil {
			return err
		}
		found := rows.Next()
		if found {
			err = rows.Scan(&raw, &version)
		}
		rows.Close()
		if err != nil {
			return err
		}
		if !found {
			detail := fmt.Sprintf("%s %d does not exist", ch.Entity, ch.ID)
			if r, err := tx.QueryContext(ctx, `SELECT new_id FROM canon.redirect WHERE entity = $1 AND old_id = $2
				AND revoked_at IS NULL ORDER BY id DESC LIMIT 1`, ch.Entity, ch.ID); err == nil {
				var to int64
				if r.Next() && r.Scan(&to) == nil {
					detail = fmt.Sprintf("%s %d was merged into %d", ch.Entity, ch.ID, to)
				}
				r.Close()
			}
			return Refused{{Change: i, Code: "ENTITY_MERGED", Detail: detail}}
		}
		if err := json.Unmarshal(raw, &state); err != nil {
			return err
		}
	}
	if state == nil {
		state = map[string]any{}
	}

	var m ent.Mutation
	if ch.ID == 0 {
		m = b.create(tx.Client())
	} else {
		m = b.update(tx.Client(), ch.ID)
	}
	var refused Refused
	changes := map[string]any{}
	keys := make([]string, 0, len(ch.Values))
	for k := range ch.Values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, key := range keys {
		v := ch.Values[key]
		p, ok := props[key]
		if !ok {
			refused = append(refused, Refusal{i, key, "PROPERTY_UNKNOWN", "not in the registry"})
			continue
		}
		if r := s.permitted(cs.Actor, p, v, state[key]); r != "" {
			refused = append(refused, Refusal{i, key, r, ""})
			continue
		}
		val, verr := verify.Value(p, v.Raw)
		if verr != nil {
			refused = append(refused, Refusal{i, key, verr.Code, verr.Detail})
			continue
		}
		if ref, isRef := val.(string); isRef && p.Shape == registry.ShapeRef {
			id, known := ids[strings.TrimPrefix(ref, "ref:")]
			if !known {
				refused = append(refused, Refusal{i, key, "VALUE_NOT_ENTAILED", ref + " is not created earlier in this changeset"})
				continue
			}
			val = id
		}
		if id, isID := val.(int64); isID && p.Shape == registry.ShapeRef && cs.Actor.Kind == ActorAgent {
			if held, err := isHeld(ctx, tx, p.Ref, id); err != nil {
				return err
			} else if held {
				refused = append(refused, Refusal{i, key, "ENTITY_HELD", fmt.Sprintf("%s %d is held", p.Ref, id)})
				continue
			}
		}
		if p.Unique == registry.UniqueGlobal {
			if holder, err := s.holder(ctx, tx, ch.Entity, key, val, ch.ID); err != nil {
				return err
			} else if holder != 0 {
				refused = append(refused, Refusal{i, key, "UNIQUE_CONFLICT", fmt.Sprintf("%s row %d already holds %v", ch.Entity, holder, val)})
				continue
			}
		}
		if err := m.SetField(key, val); err != nil {
			refused = append(refused, Refusal{i, key, "VALUE_NOT_ENTAILED", err.Error()})
			continue
		}
		changes[key] = map[string]any{"value": val, "tier": v.Tier, "evidence": v.Evidence}
		state[key] = s.stamp(cs.Actor, v.Tier)
	}
	if len(refused) > 0 {
		return refused
	}
	if b.stamped {
		if err := m.SetField("field_state", state); err != nil {
			return err
		}
		if ch.ID != 0 {
			if err := m.SetField("version", version+1); err != nil {
				return err
			}
		}
	}
	saved, err := tx.Client().Mutate(ctx, m)
	if err != nil {
		return uniqueRefusal(err, i)
	}
	id := entityID(saved)
	if ch.Ref != "" {
		ids[ch.Ref] = id
	}
	action := "update"
	if ch.ID == 0 {
		action = "create"
	}
	return tx.Revision.Create().SetEntity(ch.Entity).SetEntityID(id).SetAction(action).
		SetActorKind(string(cs.Actor.Kind)).SetActorKey(cs.Actor.Key).SetNote(cs.Note).
		SetChanges(changes).Exec(ctx)
}

func (s *Service) permitted(a Actor, p registry.Property, v Value, prior any) string {
	if a.Kind != ActorAgent {
		return ""
	}
	if !p.Agent {
		return "PROPERTY_OUT_OF_SCOPE"
	}
	if p.Shape != registry.ShapeRef && !containsTier(p.Tiers, v.Tier) {
		return "TIER_NOT_ALLOWED"
	}
	if st, ok := prior.(map[string]any); ok {
		if st["by"] == string(ActorHuman) {
			return "HUMAN_LOCKED"
		}
		if was, _ := st["assurance"].(string); was != "" && registry.Rank(registry.Tier(was)) < registry.Rank(v.Tier) {
			return "STRONGER_VALUE_EXISTS"
		}
	}
	return ""
}

func (s *Service) stamp(a Actor, tier registry.Tier) map[string]any {
	st := map[string]any{"by": string(a.Kind), "key": a.Key, "at": s.now().UTC().Format(time.RFC3339)}
	if tier != "" {
		st["assurance"] = string(tier)
	}
	return st
}

func (s *Service) holder(ctx context.Context, tx *ent.Tx, entity, column string, val any, self int64) (int64, error) {
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

func entityID(v ent.Value) int64 {
	f := reflect.Indirect(reflect.ValueOf(v)).FieldByName("ID")
	if f.CanInt() {
		return f.Int()
	}
	return 0
}

// uniqueRefusal turns a unique-index race (two commits between the pre-check
// and the insert) into the same refusal the pre-check gives.
func uniqueRefusal(err error, change int) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return Refused{{Change: change, Code: "UNIQUE_CONFLICT", Detail: pg.ConstraintName}}
	}
	return err
}
