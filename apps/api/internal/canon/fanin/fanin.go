// Package fanin is the trusted side of ingest (plan 05 §15): it pulls the
// pool, trusts none of it, re-derives every value from cited evidence and
// commits through the commit service. The pool never reaches canon.
package fanin

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"api/internal/canon/commit"
	"api/internal/canon/contract"
	"api/internal/canon/ent"
	"api/internal/canon/registry"
)

var errNoRows = sql.ErrNoRows

type Fanin struct {
	Name   string
	Pool   Pool
	Client *ent.Client
	Commit *commit.Service
	index  map[string]map[string]registry.Property
	// pushed is the org list last sent, so an unchanged one is not sent again.
	pushed string
}

func New(name string, pool Pool, client *ent.Client) *Fanin {
	return &Fanin{Name: name, Pool: pool, Client: client, Commit: commit.New(client), index: contract.Index()}
}

// RunOnce processes one page from the cursor and answers every row; a row's
// refusal is an answer, not an error. It returns how many rows it handled.
func (f *Fanin) RunOnce(ctx context.Context, limit int) (int, error) {
	if err := f.pushBlocklist(ctx); err != nil {
		return 0, fmt.Errorf("push blocklist: %w", err)
	}
	if err := f.pushRefs(ctx); err != nil {
		return 0, fmt.Errorf("push refs: %w", err)
	}
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
	modes, err := f.modes(ctx)
	if err != nil {
		return 0, err
	}
	r := &run{client: f.Client, pool: f.Pool, snaps: map[string]contract.Snapshot{}, recs: map[string]contract.Record{}, archived: map[string]contract.Record{}}
	answers := make([]contract.InboxRow, 0, len(page.Items))
	for _, e := range page.Items {
		if modes[e.Row.Program] == registry.ModeStopped {
			slog.Warn("fanin: the program is stopped; its rows stay in the pool", "program", e.Row.Program, "outbox", e.ID)
			break
		}
		ans, err := f.process(ctx, r, e, modes[e.Row.Program])
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

// modes is every registered program's mode; one nobody set is in review (D6).
func (f *Fanin) modes(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for _, p := range registry.Programs {
		out[p.Key] = registry.ModeReview
	}
	set, err := f.Client.ProgramState.Query().All(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range set {
		out[p.ID] = p.Mode
	}
	return out, nil
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

func (f *Fanin) process(ctx context.Context, r *run, e contract.OutboxEntry, mode string) (contract.InboxRow, error) {
	refused := func(rf *refusal) (contract.InboxRow, error) {
		return contract.InboxRow{OutboxID: e.ID, Result: "refused", Code: rf.code, Detail: rf.detail}, nil
	}
	row := e.Row
	if row.ContractVersion != contract.Version {
		return refused(refuse("CONTRACT_VERSION_UNSUPPORTED", "fanin accepts %s, not %q", contract.Version, row.ContractVersion))
	}
	if row.Kind == "lead_result" {
		return contract.InboxRow{OutboxID: e.ID, Result: "written"}, nil
	}
	prog, ok := registry.ProgramByKey(row.Program)
	if !ok {
		return refused(refuse("PROPERTY_OUT_OF_SCOPE", "program %q is not registered", row.Program))
	}
	r.program = prog
	key := f.key(e.ID)
	if prior, ok, err := f.Commit.Prior(ctx, key); err != nil {
		return contract.InboxRow{}, err
	} else if ok {
		return written(e, prior.IDs), nil
	}
	r.broken = nil
	it, rf, err := f.prepare(ctx, r, row)
	if err == nil {
		err = r.broken
	}
	if err != nil {
		return contract.InboxRow{}, err
	}
	if rf != nil {
		return refused(rf)
	}
	// Before the route: what a reviewer already wrote would be refused now as
	// a duplicate of itself.
	if ans, ok, err := f.alreadySettled(ctx, row); err != nil {
		return contract.InboxRow{}, err
	} else if ok {
		ans.OutboxID = e.ID
		return ans, nil
	}
	for _, ck := range it.checks {
		if err := archive(ctx, f.Client, ck); err != nil {
			return contract.InboxRow{}, err
		}
	}
	it.cs.Key, it.cs.Note = key, "fanin "+f.Name+" outbox "+strconv.FormatInt(e.ID, 10)

	kind, rf, err := f.route(ctx, it, mode)
	if err != nil {
		return contract.InboxRow{}, err
	}
	switch {
	case rf != nil && kind == "":
		return refused(rf)
	case kind != "":
		return f.file(ctx, kind, rf, e, it)
	}
	res, err := f.Commit.Apply(ctx, it.cs)
	var cr commit.Refused
	if errors.As(err, &cr) {
		return refused(refuse(cr[0].Code, "%s %s", cr[0].Property, cr[0].Detail))
	}
	if err != nil {
		return contract.InboxRow{}, err
	}
	return written(e, res.IDs), nil
}

// route decides between writing, refusing and the review queue. A row canon
// would refuse anyway never reaches a reviewer, so the check runs first, with
// every question answered "a different one".
func (f *Fanin) route(ctx context.Context, it *item, mode string) (string, *refusal, error) {
	probe := it.cs
	probe.Key = ""
	probe.Changes = slices.Clone(it.cs.Changes)
	for _, q := range it.questions {
		for i := range probe.Changes {
			if probe.Changes[i].Ref != q.Ref {
				continue
			}
			for _, c := range q.Candidates {
				probe.Changes[i].Distinct = append(slices.Clone(probe.Changes[i].Distinct), c.Token())
			}
		}
	}
	err := f.Commit.Check(ctx, probe)
	var cr commit.Refused
	switch {
	case errors.As(err, &cr):
		rf := refuse(cr[0].Code, "%s %s", cr[0].Property, cr[0].Detail)
		switch {
		case cr.Has("HUMAN_LOCKED"):
			return registry.TodoAgentOnHuman, rf, nil
		case cr.Has("SAME_TIER_CONFLICT"):
			return registry.TodoConflict, rf, nil
		case cr.Has("IDENTITY_UNSURE"):
			return registry.TodoIdentity, rf, nil
		}
		return "", rf, nil
	case err != nil:
		return "", nil, err
	}
	if len(it.questions) > 0 {
		return registry.TodoIdentity, refuse("IDENTITY_UNSURE", "%s", describe(it.questions)), nil
	}
	if mode != registry.ModeAuto {
		return registry.TodoFirstWrite, refuse("AWAITING_REVIEW", "the program is in review"), nil
	}
	return "", nil, nil
}

func describe(qs []Question) string {
	parts := make([]string, len(qs))
	for i, q := range qs {
		parts[i] = fmt.Sprintf("%s %s %q", q.Ref, q.Why, q.Name)
	}
	return strings.Join(parts, "; ")
}

func (f *Fanin) key(outbox int64) string {
	return "pool:" + f.Name + ":" + strconv.FormatInt(outbox, 10)
}

func written(e contract.OutboxEntry, ids map[string]int64) contract.InboxRow {
	ans := contract.InboxRow{OutboxID: e.ID, Result: "written"}
	if e.Row.Kind == "changeset" && len(e.Row.Changeset.Entities) > 0 {
		ans.IDs, ans.EntityID = ids, ids[e.Row.Changeset.Entities[0].Ref]
	} else if e.Row.Candidate != nil {
		ans.EntityID = e.Row.Candidate.Target.ID
	}
	return ans
}
