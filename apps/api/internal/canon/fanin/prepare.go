package fanin

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/credit"
	"api/internal/canon/ent/personalias"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
)

// item is a pool row after fanin re-derived everything in it.
type item struct {
	cs        commit.Changeset
	checks    []checked
	questions []Question
}

// Question is one identity a program may not settle (R7.5, 06 §4): a new
// entity something else may already be, or a name cited as a known person's
// without the proof the rule asks for.
type Question struct {
	Ref        string               `json:"ref"`
	Entity     string               `json:"entity"`
	Name       string               `json:"name"`
	Why        string               `json:"why"`
	Cites      int64                `json:"cites,omitempty"`
	Candidates []identity.Candidate `json:"candidates"`
}

func (f *Fanin) prepare(ctx context.Context, r *run, row contract.OutboxRow) (*item, *refusal, error) {
	it := &item{cs: commit.Changeset{Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:" + row.Program}}}
	switch row.Kind {
	case "candidate":
		c := row.Candidate
		if c == nil || c.Target.ID == 0 {
			return nil, refuse("PROPERTY_UNKNOWN", "a candidate needs an existing target id"), nil
		}
		r.row = nil
		v, ck, rf := f.property(ctx, r, c.Target.Entity, c.Property, *c)
		if rf != nil {
			return nil, rf, nil
		}
		it.checks = append(it.checks, ck)
		it.cs.Changes = []commit.Change{{Entity: c.Target.Entity, ID: c.Target.ID, Values: map[string]commit.Value{c.Property: v}}}
		return it, nil, nil
	case "changeset":
		if row.Changeset == nil || len(row.Changeset.Entities) == 0 {
			return nil, refuse("PROPERTY_UNKNOWN", "an empty changeset"), nil
		}
	default:
		return nil, refuse("PROPERTY_UNKNOWN", "unknown row kind %q", row.Kind), nil
	}
	r.row = row.Changeset
	for _, ne := range row.Changeset.Entities {
		ch := commit.Change{Ref: ne.Ref, Entity: ne.Entity, Values: map[string]commit.Value{}}
		for key, c := range ne.Properties {
			v, ck, rf := f.property(ctx, r, ne.Entity, key, c)
			if rf != nil {
				rf.detail = ne.Ref + "." + key + ": " + rf.detail
				return nil, rf, nil
			}
			it.checks = append(it.checks, ck)
			ch.Values[key] = v
		}
		if ne.Entity == "credit" {
			if rf := together(ch); rf != nil {
				rf.detail = ne.Ref + ": " + rf.detail
				return nil, rf, nil
			}
		}
		if ne.Entity == "person_alias" {
			ch.Authority = r.authority(ch.Values["name"])
		}
		it.cs.Changes = append(it.cs.Changes, ch)
	}
	return it, nil, f.identify(ctx, r, it)
}

// property checks one value; a program-scoped one is the program's own.
func (f *Fanin) property(ctx context.Context, r *run, entity, key string, c contract.Candidate) (commit.Value, checked, *refusal) {
	props, ok := f.index[entity]
	if !ok {
		return commit.Value{}, checked{}, refuse("PROPERTY_UNKNOWN", "unknown entity %s", entity)
	}
	p, ok := props[key]
	if !ok {
		return commit.Value{}, checked{}, refuse("PROPERTY_UNKNOWN", "%s.%s", entity, key)
	}
	if !p.Agent {
		return commit.Value{}, checked{}, refuse("PROPERTY_OUT_OF_SCOPE", "%s.%s", entity, key)
	}
	if p.ScopedByProgram {
		var v string
		if json.Unmarshal(c.Value, &v) != nil || v != r.program.Medium {
			return commit.Value{}, checked{}, refuse("PROPERTY_OUT_OF_SCOPE", "%s.%s must be the program's %q", entity, key, r.program.Medium)
		}
		return commit.Value{Raw: c.Value, Tier: registry.TierOfficial}, checked{}, nil
	}
	ck, rf := r.check(ctx, p, c)
	return ck.value, ck, rf
}

// together wants a credited name and its role on one line, the next one, or
// one creator cell (plan 05 §6): apart, every name on a page pairs with every role.
func together(ch commit.Change) *refusal {
	name, role := ch.Values["alias_id"], ch.Values["role"]
	if _, ok := ch.Values["alias_id"]; !ok {
		name = ch.Values["org_id"]
	}
	for _, a := range name.Evidence {
		for _, b := range role.Evidence {
			if a.Record != "" && a.Record == b.Record && parentPath(a.Path) == parentPath(b.Path) {
				return nil
			}
			if a.Snapshot != "" && a.Snapshot == b.Snapshot && a.LineFrom <= b.LineTo+1 && b.LineFrom <= a.LineTo+1 {
				return nil
			}
		}
	}
	return refuse("VALUE_NOT_ENTAILED", "the name and the role are not cited side by side")
}

func parentPath(p string) string {
	if i := strings.LastIndexByte(p, '.'); i >= 0 {
		return p[:i]
	}
	return ""
}

// authority is the registry authority printed beside a name in the record
// fanin itself resolved, prefixed by its source.
func (r *run) authority(name commit.Value) string {
	for _, e := range name.Evidence {
		if e.Record == "" || !strings.HasSuffix(e.Path, ".name") {
			continue
		}
		rec, ok := r.archived[e.Record]
		if !ok {
			continue
		}
		if a, ok := lookup(rec.Fields, parentPath(e.Path)+".authority"); ok && a != "" {
			return rec.Source + ":" + a
		}
	}
	return ""
}

// identify resolves what a rule may decide (one authority is one person) and
// lists the rest as questions.
func (f *Fanin) identify(ctx context.Context, r *run, it *item) error {
	cs, err := CiteByAuthority(ctx, f.Client, it.cs)
	if err != nil {
		return err
	}
	it.cs = cs
	for _, ch := range it.cs.Changes {
		var q *Question
		var err error
		switch ch.Entity {
		case "work_title":
			q, err = f.asksAbout(ctx, r, it, ch, "work", "work_id", "text")
		case "person_alias":
			q, err = f.asksAbout(ctx, r, it, ch, "person_alias", "", "name")
		case "org_name":
			q, err = f.asksAbout(ctx, r, it, ch, "org", "org_id", "text")
		case "credit":
			q, err = f.asksAboutCited(ctx, r, it, ch)
		}
		if err != nil {
			return err
		}
		if q != nil {
			it.questions = append(it.questions, *q)
		}
	}
	return nil
}

func (f *Fanin) asksAbout(ctx context.Context, r *run, it *item, ch commit.Change, entity, parent, property string) (*Question, error) {
	name := str(ch.Values[property])
	ref := ch.Ref
	self := int64(0)
	if parent != "" {
		to := str(ch.Values[parent])
		if to == "" {
			self = num(ch.Values[parent])
		}
		ref = strings.TrimPrefix(to, "ref:")
	}
	found, err := identity.Find(ctx, f.Client, entity, r.program.Medium, name, self)
	if err != nil {
		return nil, err
	}
	// A new organization is a person's call even when nothing resembles it (06 §5).
	if len(found) == 0 && entity != "org" {
		return nil, nil
	}
	if self != 0 && len(found) == 0 {
		return nil, nil
	}
	return &Question{Ref: ref, Entity: entity, Name: name, Why: "a new " + entity + " of this name", Candidates: found}, nil
}

// asksAboutCited wants R7.5's proof for a credit naming a known alias by id:
// the same registry authority, or the same person already credited on the work.
func (f *Fanin) asksAboutCited(ctx context.Context, r *run, it *item, ch commit.Change) (*Question, error) {
	alias := num(ch.Values["alias_id"])
	if alias == 0 {
		return nil, nil
	}
	a, err := f.Client.PersonAlias.Get(ctx, alias)
	if err != nil {
		return nil, nil //nolint:nilerr // a missing alias is refused by the commit, with where it went
	}
	if auth := r.authority(ch.Values["alias_id"]); auth != "" {
		known, err := identity.ByAuthority(ctx, f.Client, auth)
		if err != nil {
			return nil, err
		}
		if known == alias {
			return nil, nil
		}
		if known != 0 {
			if k, err := f.Client.PersonAlias.Get(ctx, known); err == nil && k.PersonID == a.PersonID {
				return nil, nil
			}
		}
	}
	if work := num(ch.Values["work_id"]); work != 0 {
		n, err := f.Client.Credit.Query().Where(credit.WorkID(work), credit.HasAliasWith(personalias.PersonID(a.PersonID))).Count(ctx)
		if err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, nil
		}
	}
	return &Question{Ref: ch.Ref, Entity: "credit", Name: a.Name, Cites: alias, Why: "cites a known person by name only",
		Candidates: []identity.Candidate{{Origin: identity.OriginCanon, ID: alias, Label: a.Name}}}, nil
}

// CiteByAuthority makes a new name under a known authority that person's: the
// same alias when it is the same name, else a further alias of theirs. The
// review runs it again, as a todo accepted before it may have made the name.
func CiteByAuthority(ctx context.Context, client *ent.Client, cs commit.Changeset) (commit.Changeset, error) {
	for i := 0; i < len(cs.Changes); i++ {
		ch := cs.Changes[i]
		if ch.Entity != "person_alias" || ch.Authority == "" || ch.ID != 0 {
			continue
		}
		known, err := identity.ByAuthority(ctx, client, ch.Authority)
		if err != nil {
			return cs, err
		}
		if known == 0 {
			continue
		}
		a, err := client.PersonAlias.Get(ctx, known)
		if err != nil {
			return cs, err
		}
		if identity.NameKey(a.Name) == identity.NameKey(str(ch.Values["name"])) {
			cs = cs.Citing(ch.Ref, a.ID)
			i = -1
		} else if p := str(ch.Values["person_id"]); p != "" {
			cs = cs.Citing(strings.TrimPrefix(p, "ref:"), a.PersonID)
			i = -1
		}
	}
	return cs, nil
}

// QuestionOf is the question a refused create asks, before its candidates.
func QuestionOf(ch commit.Change) Question {
	q := Question{Ref: ch.Ref, Entity: ch.Entity, Name: str(ch.Values["name"]), Why: "a new " + ch.Entity + " of this name"}
	for parent, entity := range map[string]string{"work_title": "work", "org_name": "org"} {
		if ch.Entity == parent {
			q.Ref, q.Entity, q.Name = strings.TrimPrefix(str(ch.Values[entity+"_id"]), "ref:"), entity, str(ch.Values["text"])
			q.Why = "a new " + entity + " of this name"
		}
	}
	return q
}

func str(v commit.Value) string {
	var s string
	_ = json.Unmarshal(v.Raw, &s)
	return s
}

func num(v commit.Value) int64 {
	n, _ := strconv.ParseInt(string(v.Raw), 10, 64)
	return n
}
