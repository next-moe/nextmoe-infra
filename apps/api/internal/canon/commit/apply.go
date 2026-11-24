package commit

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"api/internal/canon/ent"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
	"api/internal/canon/verify"
)

type applier struct {
	s       *Service
	tx      *ent.Tx
	cs      Changeset
	batch   string
	ids     map[string]int64
	created map[string]Change
}

func (a *applier) apply(ctx context.Context, i int, ch Change) error {
	b, ok := builders[ch.Entity]
	props := a.s.index[ch.Entity]
	if !ok || props == nil {
		return Refused{{Change: i, Code: "PROPERTY_UNKNOWN", Detail: "unknown entity " + ch.Entity}}
	}
	agent := a.cs.Actor.Kind == ActorAgent
	if agent && ch.ID != 0 {
		if held, err := isHeld(ctx, a.tx, ch.Entity, ch.ID); err != nil {
			return err
		} else if held {
			return Refused{{Change: i, Code: "ENTITY_HELD", Detail: fmt.Sprintf("%s %d is held", ch.Entity, ch.ID)}}
		}
	}
	row, err := a.lock(ctx, i, ch)
	if err != nil {
		return err
	}
	state, _ := row["field_state"].(map[string]any)
	if state == nil {
		state = map[string]any{}
	}
	version := intOf(row["version"])
	if ch.ExpectVersion != 0 && ch.ExpectVersion != version {
		return Refused{{Change: i, Code: "VERSION_CONFLICT", Detail: fmt.Sprintf("%s %d is at version %d, not %d", ch.Entity, ch.ID, version, ch.ExpectVersion)}}
	}

	var m ent.Mutation
	if ch.ID == 0 {
		m = b.create(a.tx.Client())
		if ch.AdoptID != 0 {
			if err := a.adopt(ctx, i, ch, m); err != nil {
				return err
			}
		}
	} else {
		m = b.update(a.tx.Client(), ch.ID)
	}
	var refused Refused
	changes := map[string]any{}
	for _, key := range sortedKeys(ch.Values) {
		v := ch.Values[key]
		p, ok := props[key]
		if !ok {
			refused = append(refused, Refusal{Change: i, Property: key, Code: "PROPERTY_UNKNOWN", Detail: "not in the registry"})
			continue
		}
		if code := inScope(a.cs.Actor, p, v); code != "" {
			refused = append(refused, Refusal{Change: i, Property: key, Code: code})
			continue
		}
		val, r, err := a.value(ctx, i, ch, key, p, v)
		if err != nil {
			return err
		}
		if r != nil {
			refused = append(refused, *r)
			continue
		}
		prior, _ := state[key].(map[string]any)
		same := ch.ID != 0 && sameValue(row[key], val)
		if same && agent && (prior == nil || prior["by"] == string(ActorHuman) || rankOf(prior) <= registry.Rank(v.Tier)) {
			continue
		}
		if code := outranked(a.cs.Actor, v, prior, same); code != "" {
			refused = append(refused, Refusal{Change: i, Property: key, Code: code, Detail: priorDetail(prior, row[key])})
			continue
		}
		if err := m.SetField(key, val); err != nil {
			refused = append(refused, Refusal{Change: i, Property: key, Code: "VALUE_NOT_ENTAILED", Detail: err.Error()})
			continue
		}
		c := map[string]any{"value": val, "tier": v.Tier, "source": v.Source, "evidence": v.Evidence}
		if ch.ID != 0 {
			c["prior"], c["prior_state"] = row[key], state[key]
		}
		changes[key] = c
		state[key] = a.stamp(v)
	}
	if len(refused) > 0 {
		return refused
	}
	if ch.ID != 0 && len(changes) == 0 {
		return a.authority(ctx, ch, ch.ID)
	}
	if err := m.SetField("field_state", state); err != nil {
		return err
	}
	if ch.ID != 0 {
		if err := m.SetField("version", version+1); err != nil {
			return err
		}
	}
	saved, err := a.tx.Client().Mutate(ctx, m)
	if err != nil {
		return uniqueRefusal(err, i)
	}
	id := entityID(saved)
	if ch.Ref != "" {
		a.ids[ch.Ref] = id
	}
	action := "update"
	if ch.ID == 0 {
		action = "create"
		a.created[entityKey(ch.Entity, id)] = ch
	}
	if err := a.judge(ctx, i, ch, id, saved, changes); err != nil {
		return err
	}
	if err := a.authority(ctx, ch, id); err != nil {
		return err
	}
	root, rootID := rootOf(ch.Entity, id, saved)
	return a.tx.Revision.Create().SetEntity(ch.Entity).SetEntityID(id).SetAction(action).
		SetActorKind(string(a.cs.Actor.Kind)).SetActorKey(a.cs.Actor.Key).SetReviewedBy(a.cs.Actor.Reviewer).SetBatch(a.batch).
		SetNote(a.cs.Note).SetChanges(changes).SetRootEntity(root).SetRootID(rootID).Exec(ctx)
}

// lock reads the row as it is now; a missing one names where it was merged to.
func (a *applier) lock(ctx context.Context, i int, ch Change) (map[string]any, error) {
	if ch.ID == 0 {
		return nil, nil
	}
	row, found, err := rowOf(ctx, a.tx, ch.Entity, ch.ID)
	if err != nil || found {
		return row, err
	}
	detail := fmt.Sprintf("%s %d does not exist", ch.Entity, ch.ID)
	if to, ok, err := redirected(ctx, a.tx, ch.Entity, ch.ID); err != nil {
		return nil, err
	} else if ok {
		detail = fmt.Sprintf("%s %d was merged into %d", ch.Entity, ch.ID, to)
	}
	return nil, Refused{{Change: i, Code: "ENTITY_MERGED", Detail: detail}}
}

// value validates one property and resolves a ref of this changeset.
func (a *applier) value(ctx context.Context, i int, ch Change, key string, p registry.Property, v Value) (any, *Refusal, error) {
	refuse := func(code, detail string) (any, *Refusal, error) {
		return nil, &Refusal{Change: i, Property: key, Code: code, Detail: detail}, nil
	}
	val, verr := verify.Value(p, v.Raw)
	if verr != nil {
		return refuse(verr.Code, verr.Detail)
	}
	if ref, isRef := val.(string); isRef && p.Shape == registry.ShapeRef {
		id, known := a.ids[strings.TrimPrefix(ref, "ref:")]
		if !known {
			return refuse("VALUE_NOT_ENTAILED", ref+" is not created earlier in this changeset")
		}
		val = id
	}
	if id, isID := val.(int64); isID && p.Shape == registry.ShapeRef && a.cs.Actor.Kind == ActorAgent {
		if held, err := isHeld(ctx, a.tx, p.Ref, id); err != nil {
			return nil, nil, err
		} else if held {
			return refuse("ENTITY_HELD", fmt.Sprintf("%s %d is held", p.Ref, id))
		}
	}
	if p.Unique == registry.UniqueGlobal {
		if holder, err := holder(ctx, a.tx, ch.Entity, key, val, ch.ID); err != nil {
			return nil, nil, err
		} else if holder != 0 {
			return refuse("UNIQUE_CONFLICT", fmt.Sprintf("%s row %d already holds %v", ch.Entity, holder, val))
		}
	}
	return val, nil, nil
}

func (a *applier) adopt(ctx context.Context, i int, ch Change, m ent.Mutation) error {
	refuse := func(detail string) error {
		return Refused{{Change: i, Code: "PROPERTY_OUT_OF_SCOPE", Detail: detail}}
	}
	if a.cs.Actor.Kind == ActorAgent && a.cs.Actor.Reviewer == "" {
		return refuse("taking over a legacy id is a reviewer's call")
	}
	setter, ok := m.(interface{ SetID(int64) })
	if !ok {
		return refuse(ch.Entity + " has no public id to take over")
	}
	var medium string
	_ = json.Unmarshal(ch.Values["medium"].Raw, &medium)
	why, err := identity.Adoptable(ctx, a.tx, ch.Entity, medium, ch.AdoptID)
	if err != nil {
		return err
	}
	if why != "" {
		return Refused{{Change: i, Code: "IDENTITY_UNSURE", Detail: why}}
	}
	if to, ok, err := redirected(ctx, a.tx, ch.Entity, ch.AdoptID); err != nil {
		return err
	} else if ok {
		return Refused{{Change: i, Code: "ENTITY_MERGED", Detail: fmt.Sprintf("%s %d was merged into %d", ch.Entity, ch.AdoptID, to)}}
	}
	if _, found, err := rowOf(ctx, a.tx, ch.Entity, ch.AdoptID); err != nil {
		return err
	} else if found {
		return Refused{{Change: i, Code: "UNIQUE_CONFLICT", Detail: fmt.Sprintf("%s %d is already in canon; cite it", ch.Entity, ch.AdoptID)}}
	}
	setter.SetID(ch.AdoptID)
	return nil
}

// judged is the property an entity's identity is judged by.
var judged = map[string]struct{ property, root string }{
	"work_title":   {"text", "work"},
	"person_alias": {"name", "person_alias"},
	"org_name":     {"text", "org"},
}

// judge re-runs the candidate search inside the transaction (06 §6 I1) and
// refuses what nobody has told apart, then brings the keys up to date (I3).
// A system write is a seed or a migration whose author already judged it.
func (a *applier) judge(ctx context.Context, i int, ch Change, id int64, saved ent.Value, changes map[string]any) error {
	j, ok := judged[ch.Entity]
	if !ok {
		return nil
	}
	c, renamed := changes[j.property].(map[string]any)
	if !renamed {
		return nil
	}
	rootID := id
	if j.root != ch.Entity {
		_, rootID = rootOf(ch.Entity, id, saved)
	}
	medium := ""
	if j.root == "work" {
		w, err := a.tx.Work.Get(ctx, rootID)
		if err != nil {
			return err
		}
		medium = w.Medium
	}
	name := fmt.Sprint(c["value"])
	_, keys := identity.Keys(j.root, name)
	if err := identity.Lock(ctx, a.tx, j.root, medium, keys); err != nil {
		return err
	}
	if a.cs.Actor.Kind != ActorSystem {
		found, err := identity.Find(ctx, a.tx, j.root, medium, name, rootID)
		if err != nil {
			return err
		}
		distinct := append(slices.Clone(ch.Distinct), a.created[entityKey(j.root, rootID)].Distinct...)
		found = slices.DeleteFunc(found, func(c identity.Candidate) bool {
			_, mine := a.created[entityKey(j.root, c.ID)]
			return mine || slices.Contains(distinct, c.Token())
		})
		if len(found) > 0 {
			return Refused{{Change: i, Property: j.property, Code: "IDENTITY_UNSURE", Candidates: found,
				Detail: fmt.Sprintf("%s %q may already be %s", j.root, name, describe(found))}}
		}
	}
	return identity.Rekey(ctx, a.tx, j.root, rootID)
}

func describe(cs []identity.Candidate) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = fmt.Sprintf("%s %q", c.Token(), c.Label)
	}
	return strings.Join(parts, ", ")
}

func (a *applier) authority(ctx context.Context, ch Change, id int64) error {
	if ch.Authority == "" {
		return nil
	}
	return identity.AddAuthority(ctx, a.tx, ch.Entity, id, ch.Authority)
}

func sortedKeys(m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func entityKey(entity string, id int64) string { return fmt.Sprintf("%s:%d", entity, id) }
