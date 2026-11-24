// Package review settles todos (plan 07 §4, D29). Accepting a verified row
// commits it as the program's own write with the reviewer on it; nothing is
// re-read from the pool. A reviewer may answer identity questions and leave
// parts out, but never supplies a value: one without evidence would not be one.
package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"api/internal/canon/commit"
	"api/internal/canon/ent"
	"api/internal/canon/fanin"
	"api/internal/canon/identity"
	"api/internal/canon/registry"
)

// Reviewer is who decides: a person ("user:<id>") or a review agent
// ("review:<name>").
type Reviewer struct {
	Key   string
	Human bool
}

// Answer settles one question: the entity is an existing canon one, is a
// legacy one whose id the new entity takes over, or is none of the candidates.
type Answer struct {
	Use   int64 `json:"use,omitempty"`
	Adopt int64 `json:"adopt,omitempty"`
	New   bool  `json:"new,omitempty"`
}

type Decision struct {
	Answers map[string]Answer `json:"answers,omitempty"`
	Omit    []string          `json:"omit,omitempty"`
	Note    string            `json:"note,omitempty"`
}

var (
	ErrSettled = errors.New("the todo is already settled")
	// ErrPersonOnly: a value a person wrote is only a person's to replace.
	ErrPersonOnly = errors.New("only a person may settle this todo")
)

// Invalid is a decision that does not answer what the todo asks.
type Invalid string

func (e Invalid) Error() string { return string(e) }

func Accept(ctx context.Context, client *ent.Client, id int, by Reviewer, d Decision) (map[string]int64, error) {
	t, err := open(ctx, client, id, by)
	if err != nil {
		return nil, err
	}
	var cs commit.Changeset
	var questions []fanin.Question
	if err := convert(t.Changeset, &cs); err != nil {
		return nil, err
	}
	if err := convert(t.Questions, &questions); err != nil {
		return nil, err
	}
	if len(cs.Changes) == 0 {
		return nil, Invalid("the todo carries no verified change")
	}
	cs, used, err := resolve(ctx, client, cs, questions, by, d)
	if err != nil {
		return nil, err
	}
	cs.Actor.Reviewer = by.Key
	if t.Kind == registry.TodoAgentOnHuman {
		// Taking the agent's value over a person's is that person's own edit.
		cs.Actor = commit.Actor{Kind: commit.ActorHuman, Key: by.Key}
	}
	cs.Note = strings.TrimSpace(cs.Note + "; todo " + fmt.Sprint(t.ID) + " accepted: " + d.Note)
	res, err := commit.New(client).Apply(ctx, cs)
	if err != nil {
		return nil, err
	}
	ids := maps.Clone(res.IDs)
	maps.Copy(ids, used)
	return ids, client.Todo.UpdateOneID(t.ID).SetState("done").SetResolvedBy(by.Key).
		SetResolution(map[string]any{"result": "written", "ids": ids, "batch": res.Batch, "note": d.Note, "omitted": d.Omit}).Exec(ctx)
}

func Reject(ctx context.Context, client *ent.Client, id int, by Reviewer, note string) error {
	t, err := open(ctx, client, id, by)
	if err != nil {
		return err
	}
	if strings.TrimSpace(note) == "" {
		return Invalid("a rejection says why")
	}
	return client.Todo.UpdateOneID(t.ID).SetState("rejected").SetResolvedBy(by.Key).
		SetResolution(map[string]any{"result": "refused", "code": "REJECTED_IN_REVIEW", "detail": note}).Exec(ctx)
}

// Unsure leaves the todo open for someone else, with what this reviewer found.
func Unsure(ctx context.Context, client *ent.Client, id int, by Reviewer, note string) error {
	t, err := open(ctx, client, id, by)
	if err != nil {
		return err
	}
	notes := append(slices.Clone(t.Notes), map[string]any{"by": by.Key, "note": note, "at": time.Now().UTC().Format(time.RFC3339)})
	return client.Todo.UpdateOneID(t.ID).SetNotes(notes).Exec(ctx)
}

func open(ctx context.Context, client *ent.Client, id int, by Reviewer) (*ent.Todo, error) {
	t, err := client.Todo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.State != "open" {
		return nil, ErrSettled
	}
	if t.Kind == registry.TodoAgentOnHuman && !by.Human {
		return nil, ErrPersonOnly
	}
	return t, nil
}

// resolve rewrites the changeset by the answers. used is what the refs it no
// longer creates turned out to be, which the pool learns from.
func resolve(ctx context.Context, client *ent.Client, cs commit.Changeset, questions []fanin.Question, by Reviewer, d Decision) (commit.Changeset, map[string]int64, error) {
	used := map[string]int64{}
	if len(d.Omit) > 0 {
		for _, r := range d.Omit {
			if !slices.ContainsFunc(cs.Changes, func(ch commit.Change) bool { return ch.Ref == r }) {
				return cs, nil, Invalid("omit names " + r + ", which the changeset does not have")
			}
		}
		cs = cs.Omitting(d.Omit...)
		if len(cs.Changes) == 0 {
			return cs, nil, Invalid("nothing is left to write")
		}
	}
	asked := map[string]bool{}
	for _, q := range questions {
		asked[q.Ref] = true
		i := slices.IndexFunc(cs.Changes, func(ch commit.Change) bool { return ch.Ref == q.Ref })
		if i < 0 {
			continue
		}
		a, ok := d.Answers[q.Ref]
		if !ok {
			return cs, nil, Invalid("question " + q.Ref + " is not answered")
		}
		var err error
		switch {
		case q.Cites != 0:
			cs, err = settleCited(cs, i, a)
		case a.Use != 0:
			before := cs
			cs = cs.Citing(q.Ref, a.Use)
			used[q.Ref] = a.Use
			err = usePerson(ctx, client, before, q, a.Use, &cs, used)
		case a.Adopt != 0:
			err = adopt(ctx, client, &cs, q, a.Adopt, by)
		case a.New:
			if q.Entity == "org" && !by.Human {
				return cs, nil, Invalid("a new organization is a person's call")
			}
			for _, c := range q.Candidates {
				cs.Changes[i].Distinct = append(slices.Clone(cs.Changes[i].Distinct), c.Token())
			}
		default:
			err = Invalid("the answer to " + q.Ref + " is neither use, adopt nor new")
		}
		if err != nil {
			return cs, nil, err
		}
	}
	for ref := range d.Answers {
		if !asked[ref] {
			return cs, nil, Invalid("answers name " + ref + ", which nothing asks about")
		}
	}
	return cs, used, nil
}

func settleCited(cs commit.Changeset, i int, a Answer) (commit.Changeset, error) {
	if a.Use == 0 {
		return cs, Invalid("a cited name is confirmed with use, or left out with omit")
	}
	ch := cs.Changes[i]
	values := maps.Clone(ch.Values)
	v := values["alias_id"]
	v.Raw = json.RawMessage(fmt.Sprint(a.Use))
	values["alias_id"] = v
	ch.Values = values
	cs.Changes = slices.Clone(cs.Changes)
	cs.Changes[i] = ch
	return cs, nil
}

// usePerson records the authority the name was printed under on the alias it
// turned out to be, and tells the pool which person that is.
func usePerson(ctx context.Context, client *ent.Client, before commit.Changeset, q fanin.Question, alias int64, cs *commit.Changeset, used map[string]int64) error {
	if q.Entity != "person_alias" {
		return nil
	}
	a, err := client.PersonAlias.Get(ctx, alias)
	if ent.IsNotFound(err) {
		return Invalid(fmt.Sprintf("person alias %d does not exist", alias))
	}
	if err != nil {
		return err
	}
	for _, ch := range before.Changes {
		if ch.Ref != q.Ref {
			continue
		}
		if ch.Authority != "" {
			cs.Changes = append(slices.Clone(cs.Changes), commit.Change{Entity: "person_alias", ID: alias, Authority: ch.Authority})
		}
		var person string
		if json.Unmarshal(ch.Values["person_id"].Raw, &person) == nil && strings.HasPrefix(person, "ref:") {
			used[strings.TrimPrefix(person, "ref:")] = a.PersonID
		}
	}
	return nil
}

// adopt takes over a legacy id. A legacy credit name keeps its person too:
// under the id canon already has, or taken over with it (D19, D30).
func adopt(ctx context.Context, client *ent.Client, cs *commit.Changeset, q fanin.Question, id int64, by Reviewer) error {
	if !slices.ContainsFunc(q.Candidates, func(c identity.Candidate) bool { return c.Origin == identity.OriginLegacy && c.ID == id }) && !by.Human {
		return Invalid(fmt.Sprintf("legacy %d is not a candidate of %s", id, q.Ref))
	}
	cs.Changes = slices.Clone(cs.Changes)
	personRef := ""
	for i, ch := range cs.Changes {
		if ch.Ref != q.Ref {
			continue
		}
		cs.Changes[i].AdoptID = id
		for _, c := range q.Candidates {
			if c.ID != id || c.Origin != identity.OriginLegacy {
				cs.Changes[i].Distinct = append(slices.Clone(cs.Changes[i].Distinct), c.Token())
			}
		}
		var p string
		if json.Unmarshal(ch.Values["person_id"].Raw, &p) == nil {
			personRef = strings.TrimPrefix(p, "ref:")
		}
	}
	if q.Entity != "person_alias" || personRef == "" {
		return nil
	}
	person, err := identity.LegacyPerson(ctx, client, id)
	if err != nil || person == 0 {
		return err
	}
	if _, err := client.Person.Get(ctx, person); err == nil {
		*cs = cs.Citing(personRef, person)
		return nil
	} else if !ent.IsNotFound(err) {
		return err
	}
	for i, ch := range cs.Changes {
		if ch.Ref == personRef {
			cs.Changes[i].AdoptID = person
		}
	}
	return nil
}

func convert(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}
