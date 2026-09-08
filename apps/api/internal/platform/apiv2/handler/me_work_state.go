package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"
)

func workStateFromWords(state, completion string) (int16, *int16) {
	var st int16
	switch state {
	case "wish":
		st = model.WorkStateWish
	case "doing":
		st = model.WorkStateDoing
	case "done":
		st = model.WorkStateDone
	case "on_hold":
		st = model.WorkStateOnHold
	case "dropped":
		st = model.WorkStateDropped
	}
	if completion == "" {
		return st, nil
	}
	var c int16
	switch completion {
	case "one_route":
		c = model.WorkCompletionOneRoute
	case "main":
		c = model.WorkCompletionMain
	case "all":
		c = model.WorkCompletionAll
	}
	return st, &c
}

func workStateToWords(state int16, completion *int16) (string, *string) {
	var st string
	switch state {
	case model.WorkStateWish:
		st = "wish"
	case model.WorkStateDoing:
		st = "doing"
	case model.WorkStateDone:
		st = "done"
	case model.WorkStateOnHold:
		st = "on_hold"
	case model.WorkStateDropped:
		st = "dropped"
	}
	if completion == nil {
		return st, nil
	}
	var c string
	switch *completion {
	case model.WorkCompletionOneRoute:
		c = "one_route"
	case model.WorkCompletionMain:
		c = "main"
	case model.WorkCompletionAll:
		c = "all"
	}
	return st, &c
}

func userWorkStateRepr(r catsvc.WorkStateRecord) repr.UserWorkState {
	st, comp := workStateToWords(r.State, r.Completion)
	return repr.UserWorkState{
		Object: "work_state", WorkID: repr.ID(r.WorkID), State: st, Completion: comp,
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (c *Catalog) ListWorkStates(ctx context.Context, q collect.Query, workIDs []string) (repr.List[repr.UserWorkState], error) {
	if c == nil || c.WorkStates == nil {
		return repr.List[repr.UserWorkState]{}, problem.New(problem.CodeServiceUnavailable, "", "", "work states are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.UserWorkState]{}, err
	}
	if len(workIDs) > 0 {
		if len(workIDs) > collect.MaxBatchItems {
			return repr.List[repr.UserWorkState]{}, problem.New(problem.CodeTooManyIDs, "", "", "work_ids named more than 100 items.")
		}
		items := make([]repr.UserWorkState, 0, len(workIDs))
		var missing []string
		for _, s := range workIDs {
			id, ok := repr.ParseID(s)
			if !ok {
				p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids values must be decimal catalog ids.")
				p.Errors = []problem.FieldError{{Parameter: "work_ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
				return repr.List[repr.UserWorkState]{}, p
			}
			row, gerr := c.WorkStates.GetMine(ctx, uid, id)
			if gerr != nil {
				return repr.List[repr.UserWorkState]{}, gerr
			}
			if row == nil {
				missing = append(missing, s)
				continue
			}
			items = append(items, userWorkStateRepr(*row))
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	// Nano precision plus a work_id tiebreak — a bare RFC3339 cursor truncates to whole seconds and re-returns the boundary row forever; me_playtime.go hit exactly that.
	since, sinceWorkID := time.Time{}, int64(0)
	if q.Cursor != "" {
		ts := q.Cursor
		if i := strings.LastIndexByte(ts, '|'); i >= 0 {
			id, ok := repr.ParseID(ts[i+1:])
			if !ok {
				return repr.List[repr.UserWorkState]{}, collectInvalidCursor()
			}
			ts, sinceWorkID = ts[:i], id
		}
		t, perr := time.Parse(time.RFC3339, ts)
		if perr != nil {
			return repr.List[repr.UserWorkState]{}, collectInvalidCursor()
		}
		since = t
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.WorkStates.ListMine(ctx, uid, since, sinceWorkID, limit+1)
	if lerr != nil {
		return repr.List[repr.UserWorkState]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		s := last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + repr.ID(last.WorkID)
		next = &s
	}
	items := make([]repr.UserWorkState, 0, len(rows))
	for _, r := range rows {
		items = append(items, userWorkStateRepr(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.WorkStates.CountMine(ctx, uid); lerr != nil {
			return repr.List[repr.UserWorkState]{}, lerr
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetWorkState(ctx context.Context, workID int64) (repr.UserWorkState, error) {
	if c == nil || c.WorkStates == nil {
		return repr.UserWorkState{}, problem.New(problem.CodeServiceUnavailable, "", "", "work states are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserWorkState{}, err
	}
	row, gerr := c.WorkStates.GetMine(ctx, uid, workID)
	if gerr != nil {
		return repr.UserWorkState{}, gerr
	}
	if row == nil {
		return repr.UserWorkState{}, problem.New(problem.CodeNotFound, "", "", "No state on this work.")
	}
	return userWorkStateRepr(*row), nil
}

func (c *Catalog) PutWorkState(ctx context.Context, workID int64, state string, completion string) (repr.UserWorkState, error) {
	if c == nil || c.WorkStates == nil {
		return repr.UserWorkState{}, problem.New(problem.CodeServiceUnavailable, "", "", "work states are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserWorkState{}, err
	}
	st, comp := workStateFromWords(state, completion)
	rec, gerr := c.WorkStates.Set(ctx, uid, workID, st, comp)
	if gerr != nil {
		return repr.UserWorkState{}, workStateErr(gerr)
	}
	return userWorkStateRepr(*rec), nil
}

func (c *Catalog) DeleteWorkState(ctx context.Context, workID int64) error {
	if c == nil || c.WorkStates == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "work states are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return workStateErr(c.WorkStates.DeleteMine(ctx, uid, workID))
}

func (c *Catalog) BatchWorkStates(ctx context.Context, items []workStateBatchEntry) (repr.List[repr.WorkStateBatchItem], error) {
	out := make([]repr.WorkStateBatchItem, 0, len(items))
	// maxItems on the request schema refuses this first with a 422; the
	// guard stays because the schema and this function are two different
	// authorities and only one of them is the one that writes rows.
	if len(items) > collect.MaxBatchItems {
		return repr.List[repr.WorkStateBatchItem]{}, problem.New(problem.CodeTooManyIDs, "", "", "batch work states named more than 100 items.")
	}
	for i, it := range items {
		id, ok := repr.ParseID(it.WorkID)
		if !ok {
			p := problem.New(problem.CodeValidationFailed, "", "", "work_id must be a decimal catalog id.")
			p.Errors = []problem.FieldError{{Pointer: fmt.Sprintf("/items/%d/work_id", i), Reason: problem.ReasonInvalidFormat, Detail: it.WorkID}}
			out = append(out, repr.WorkStateBatchItem{Status: 422, Problem: p})
			continue
		}
		rec, err := c.PutWorkState(ctx, id, it.State, it.Completion)
		if err != nil {
			p, ok := err.(*problem.Problem)
			if !ok {
				p = problem.New(problem.CodeInternalError, "", "", err.Error())
			}
			out = append(out, repr.WorkStateBatchItem{Status: p.Status, Problem: p})
			continue
		}
		obj, wid, st := "work_state", rec.WorkID, rec.State
		out = append(out, repr.WorkStateBatchItem{Status: 200, Object: &obj, WorkID: &wid, State: &st, Completion: rec.Completion})
	}
	return repr.NewList(out, nil), nil
}

func workStateErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case err == catsvc.ErrWorkStateCompletionOnWish:
		p := problem.New(problem.CodeValidationFailed, "", "", "completion cannot accompany wish.")
		p.Errors = []problem.FieldError{{Pointer: "/completion", Reason: problem.ReasonNotAllowedValue, Detail: err.Error()}}
		return p
	case err == catsvc.ErrWorkStateBadCompletion:
		p := problem.New(problem.CodeValidationFailed, "", "", "completion is not in the vocabulary.")
		p.Errors = []problem.FieldError{{Pointer: "/completion", Reason: problem.ReasonUnknownValue, Detail: err.Error()}}
		return p
	case err == catsvc.ErrWorkStateBadState:
		p := problem.New(problem.CodeValidationFailed, "", "", "state is not in the vocabulary.")
		p.Errors = []problem.FieldError{{Pointer: "/state", Reason: problem.ReasonUnknownValue, Detail: err.Error()}}
		return p
	case err == catsvc.ErrWorkStateWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for states.")
	case err == catsvc.ErrWorkStateActorRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	}
	return err
}
