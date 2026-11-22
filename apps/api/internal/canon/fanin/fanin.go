// Package fanin is the trusted side of ingest (plan 05 §15): it pulls the
// pool, trusts none of it, re-derives every value from cited evidence and
// commits through the commit service. The pool never reaches canon.
package fanin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/ent/bookrelease"
	"api/internal/canon/ent/orgname"
	"api/internal/canon/ent/personalias"
	"api/internal/canon/registry"
)

var errNoRows = sql.ErrNoRows

type Fanin struct {
	Name   string
	Pool   Pool
	Client *ent.Client
	Commit *commit.Service
	index  map[string]map[string]registry.Property
}

func New(name string, pool Pool, client *ent.Client) *Fanin {
	return &Fanin{Name: name, Pool: pool, Client: client, Commit: commit.New(client), index: contract.Index()}
}

// RunOnce processes one page from the cursor and answers every row; a row's
// refusal is an answer, not an error. It returns how many rows it handled.
func (f *Fanin) RunOnce(ctx context.Context, limit int) (int, error) {
	if err := f.answerSettled(ctx); err != nil {
		return 0, fmt.Errorf("answer settled todos: %w", err)
	}
	after, err := f.cursor(ctx)
	if err != nil {
		return 0, err
	}
	page, err := f.Pool.Outbox(ctx, after, limit)
	if err != nil {
		return 0, err
	}
	r := &run{pool: f.Pool, snaps: map[string]contract.Snapshot{}, recs: map[string]contract.Record{}}
	answers := make([]contract.InboxRow, 0, len(page.Items))
	for _, e := range page.Items {
		ans, err := f.process(ctx, r, e)
		if err != nil {
			return 0, fmt.Errorf("outbox %d: %w", e.ID, err)
		}
		answers = append(answers, ans)
		after = max(after, e.ID)
	}
	if err := f.Pool.Answer(ctx, answers); err != nil {
		return 0, err
	}
	return len(answers), f.Client.FaninCursor.Create().SetID(f.Name).SetAfter(after).
		OnConflictColumns("id").UpdateNewValues().Exec(ctx)
}

func (f *Fanin) cursor(ctx context.Context) (int64, error) {
	c, err := f.Client.FaninCursor.Get(ctx, f.Name)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return c.After, nil
}

func (f *Fanin) process(ctx context.Context, r *run, e contract.OutboxEntry) (contract.InboxRow, error) {
	ans := contract.InboxRow{OutboxID: e.ID}
	refused := func(rf *refusal) (contract.InboxRow, error) {
		ans.Result, ans.Code, ans.Detail = "refused", rf.code, rf.detail
		if subject, ok := todoSubject(rf, e.Row); ok {
			if err := f.fileTodo(ctx, rf, subject, e); err != nil {
				return ans, err
			}
			ans.Result = "todo"
		}
		return ans, nil
	}
	row := e.Row
	if row.ContractVersion != contract.Version {
		return refused(refuse("CONTRACT_VERSION_UNSUPPORTED", "fanin accepts %s, not %q", contract.Version, row.ContractVersion))
	}
	prog, ok := registry.ProgramByKey(row.Program)
	if !ok && row.Kind != "lead_result" {
		return refused(refuse("PROPERTY_OUT_OF_SCOPE", "program %q is not registered", row.Program))
	}
	r.program = prog
	cs := commit.Changeset{
		Actor: commit.Actor{Kind: commit.ActorAgent, Key: "program:" + row.Program},
		Note:  "fanin " + f.Name + " outbox " + strconv.FormatInt(e.ID, 10),
		Key:   "pool:" + f.Name + ":" + strconv.FormatInt(e.ID, 10),
	}
	var checks []checked
	switch row.Kind {
	case "lead_result":
		ans.Result = "written"
		return ans, nil
	case "candidate":
		c := row.Candidate
		if c == nil || c.Target.ID == 0 {
			return refused(refuse("PROPERTY_UNKNOWN", "a candidate needs an existing target id"))
		}
		v, ck, rf := f.property(ctx, r, c.Target.Entity, c.Property, *c)
		if rf != nil {
			return refused(rf)
		}
		checks = append(checks, ck)
		cs.Changes = []commit.Change{{Entity: c.Target.Entity, ID: c.Target.ID, Values: map[string]commit.Value{c.Property: v}}}
		ans.EntityID = c.Target.ID
	case "changeset":
		if row.Changeset == nil || len(row.Changeset.Entities) == 0 {
			return refused(refuse("PROPERTY_UNKNOWN", "an empty changeset"))
		}
		for _, ne := range row.Changeset.Entities {
			ch := commit.Change{Ref: ne.Ref, Entity: ne.Entity, Values: map[string]commit.Value{}}
			for key, c := range ne.Properties {
				v, ck, rf := f.property(ctx, r, ne.Entity, key, c)
				if rf != nil {
					rf.detail = ne.Ref + "." + key + ": " + rf.detail
					return refused(rf)
				}
				checks = append(checks, ck)
				ch.Values[key] = v
			}
			if rf, holder := f.identity(ctx, row.Changeset.LeadID, ch); rf != nil {
				ans.EntityID = holder
				return refused(rf)
			}
			cs.Changes = append(cs.Changes, ch)
		}
	default:
		return refused(refuse("PROPERTY_UNKNOWN", "unknown row kind %q", row.Kind))
	}

	for _, ck := range checks {
		if err := archive(ctx, f.Client, ck); err != nil {
			return ans, err
		}
	}
	res, err := f.Commit.Apply(ctx, cs)
	var cr commit.Refused
	if errors.As(err, &cr) {
		return refused(refuse(cr[0].Code, "%s %s", cr[0].Property, cr[0].Detail))
	}
	if err != nil {
		return ans, err
	}
	ans.Result = "written"
	if row.Kind == "changeset" {
		ans.EntityID = res.IDs[row.Changeset.Entities[0].Ref]
		ans.IDs = res.IDs
	}
	return ans, nil
}

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

// identity applies plan 06 before anything is created: a known ISBN or JAN
// means the release exists; a new organization is always a human's call; a
// person name already in canon cannot be told apart by fanin alone.
func (f *Fanin) identity(ctx context.Context, lead string, ch commit.Change) (*refusal, int64) {
	switch ch.Entity {
	case "book_release":
		for _, key := range []string{"isbn13", "jan"} {
			v, ok := ch.Values[key]
			if !ok {
				continue
			}
			code := string(v.Raw[1 : len(v.Raw)-1])
			pred := bookrelease.Isbn13(code)
			if key == "jan" {
				pred = bookrelease.Jan(code)
			}
			if br, err := f.Client.BookRelease.Query().Where(pred).Only(ctx); err == nil {
				return refuse("UNIQUE_CONFLICT", "%s %s is release %d", key, code, br.ReleaseID), br.ReleaseID
			}
		}
	case "org", "org_name":
		if ch.Entity == "org" {
			rf := refuse("IDENTITY_UNSURE", "new organizations are seeded by a person (plan 06 §5)")
			rf.subject = "identity:org:" + lead
			return rf, 0
		}
		if v, ok := ch.Values["text"]; ok {
			if n, err := f.Client.OrgName.Query().Where(orgname.Text(unquote(v))).First(ctx); err == nil {
				rf := refuse("IDENTITY_UNSURE", "org %d already has this name", n.OrgID)
				rf.subject = "identity:org_name:" + unquote(v)
				return rf, n.OrgID
			}
		}
	case "person_alias":
		if v, ok := ch.Values["name"]; ok {
			if a, err := f.Client.PersonAlias.Query().Where(personalias.Name(unquote(v))).First(ctx); err == nil {
				rf := refuse("IDENTITY_UNSURE", "alias %d already has this name; cite it if it is the same person", a.ID)
				rf.subject = "identity:person_alias:" + unquote(v)
				return rf, a.ID
			}
		}
	}
	return nil, 0
}

func unquote(v commit.Value) string {
	s := string(v.Raw)
	if len(s) >= 2 && s[0] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
