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
	"strconv"
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
	// Evidence is what the reviewer found outside the todo; it is kept with
	// the resolution, and no value is ever read off it.
	Evidence []Found `json:"evidence,omitempty"`
}

type Found struct {
	URL   string `json:"url"`
	Quote string `json:"quote"`
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
	before := cs
	if cs, err = fanin.CiteByAuthority(ctx, client, cs); err != nil {
		return nil, err
	}
	cs, used, err := resolve(ctx, client, cs, questions, by, d)
	if err != nil {
		return nil, err
	}
	maps.Copy(used, citedSince(before, cs))
	if err := learnPersons(ctx, client, before, used); err != nil {
		return nil, err
	}
	cs.Actor.Reviewer = by.Key
	if t.Kind == registry.TodoAgentOnHuman {
		// Taking the agent's value over a person's is that person's own edit.
		cs.Actor = commit.Actor{Kind: commit.ActorHuman, Key: by.Key}
	}
	cs.Note = strings.TrimSpace(cs.Note + "; todo " + fmt.Sprint(t.ID) + " accepted: " + d.Note)
	res, err := commit.New(client).Apply(ctx, cs)
	var refused commit.Refused
	if errors.As(err, &refused) && refused.Has("IDENTITY_UNSURE") {
		return nil, ask(ctx, client, t, cs, questions, refused)
	}
	if err != nil {
		return nil, err
	}
	ids := maps.Clone(res.IDs)
	maps.Copy(ids, used)
	return ids, client.Todo.UpdateOneID(t.ID).SetState("done").SetResolvedBy(by.Key).
		SetResolution(map[string]any{"result": "written", "ids": ids, "batch": res.Batch, "note": d.Note, "omitted": d.Omit, "evidence": d.Evidence}).Exec(ctx)
}

// ask adds what turned up since the todo was filed to its questions: a todo
// accepted before this one may have made the very entity this one creates.
func ask(ctx context.Context, client *ent.Client, t *ent.Todo, cs commit.Changeset, questions []fanin.Question, refused commit.Refused) error {
	for _, r := range refused {
		if r.Code != "IDENTITY_UNSURE" || r.Change >= len(cs.Changes) {
			continue
		}
		ch := cs.Changes[r.Change]
		q := fanin.QuestionOf(ch)
		if i := slices.IndexFunc(questions, func(x fanin.Question) bool { return x.Ref == q.Ref }); i >= 0 {
			questions[i].Candidates = append(questions[i].Candidates, r.Candidates...)
		} else {
			q.Candidates = r.Candidates
			questions = append(questions, q)
		}
	}
	var stored []map[string]any
	if err := convert(questions, &stored); err != nil {
		return err
	}
	if err := client.Todo.UpdateOneID(t.ID).SetKind(registry.TodoIdentity).SetQuestions(stored).
		SetDetail("IDENTITY_UNSURE " + refused.Error()).Exec(ctx); err != nil {
		return err
	}
	return Invalid("something of this name was made since: " + refused.Error())
}

// citedSince is what the refs an authority settled on the way turned out to be.
func citedSince(before, after commit.Changeset) map[string]int64 {
	made := map[string]bool{}
	for _, ch := range after.Changes {
		made[ch.Ref] = true
	}
	out := map[string]int64{}
	for _, ch := range before.Changes {
		for key, v := range ch.Values {
			ref := refOf(v)
			if ref == "" || made[ref] {
				continue
			}
			for _, now := range after.Changes {
				if now.Ref != ch.Ref {
					continue
				}
				if id, err := strconv.ParseInt(string(now.Values[key].Raw), 10, 64); err == nil {
					out[ref] = id
				}
			}
		}
	}
	return out
}

func refOf(v commit.Value) string {
	var s string
	if json.Unmarshal(v.Raw, &s) != nil {
		return ""
	}
	ref, _ := strings.CutPrefix(s, "ref:")
	if ref == s {
		return ""
	}
	return ref
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
		var unknown []string
		if cs, unknown = cs.Omitting(d.Omit...); len(unknown) > 0 {
			return cs, nil, Invalid("omit names " + strings.Join(unknown, ", ") + ", which the changeset does not have")
		}
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
			err = usePerson(ctx, client, before, q, a.Use, &cs)
		case a.Adopt != 0:
			// Taken over by a todo accepted before this one: the same entity,
			// now canon's, under the same id.
			if taken, terr := inCanon(ctx, client, q.Entity, a.Adopt); terr != nil {
				return cs, nil, terr
			} else if taken {
				before := cs
				cs = cs.Citing(q.Ref, a.Adopt)
				used[q.Ref] = a.Adopt
				err = usePerson(ctx, client, before, q, a.Adopt, &cs)
				break
			}
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
// turned out to be.
func usePerson(ctx context.Context, client *ent.Client, before commit.Changeset, q fanin.Question, alias int64, cs *commit.Changeset) error {
	if q.Entity != "person_alias" {
		return nil
	}
	if _, err := client.PersonAlias.Get(ctx, alias); ent.IsNotFound(err) {
		return Invalid(fmt.Sprintf("person alias %d does not exist", alias))
	} else if err != nil {
		return err
	}
	for _, ch := range before.Changes {
		if ch.Ref == q.Ref && ch.Authority != "" {
			cs.Changes = append(slices.Clone(cs.Changes), commit.Change{Entity: "person_alias", ID: alias, Authority: ch.Authority})
		}
	}
	return nil
}

// learnPersons adds the person behind every alias that was cited instead of
// created, so the pool learns both.
func learnPersons(ctx context.Context, client *ent.Client, before commit.Changeset, used map[string]int64) error {
	for _, ch := range before.Changes {
		alias, ok := used[ch.Ref]
		if !ok || ch.Entity != "person_alias" {
			continue
		}
		person := refOf(ch.Values["person_id"])
		if person == "" {
			continue
		}
		a, err := client.PersonAlias.Get(ctx, alias)
		if err != nil {
			return err
		}
		used[person] = a.PersonID
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

func inCanon(ctx context.Context, client *ent.Client, entity string, id int64) (bool, error) {
	table, ok := map[string]string{"work": "work", "person_alias": "person_alias", "org": "org"}[entity]
	if !ok {
		return false, nil
	}
	found, err := lines(ctx, client, `SELECT 'x' FROM canon.`+table+` WHERE id = $1`, id)
	return len(found) > 0, err
}

func convert(from, to any) error {
	b, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, to)
}
