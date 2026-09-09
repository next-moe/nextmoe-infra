package handler

import (
	"context"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"
)

func (c *Catalog) ListPlaytimes(ctx context.Context, q collect.Query, workIDs []string) (repr.List[repr.UserPlaytime], error) {
	if c == nil || c.Playtime == nil {
		return repr.List[repr.UserPlaytime]{}, problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.List[repr.UserPlaytime]{}, err
	}
	if len(workIDs) > 0 {
		if len(workIDs) > collect.MaxBatchItems {
			return repr.List[repr.UserPlaytime]{}, problem.New(problem.CodeTooManyIDs, "", "", "work_ids named more than 100 items.")
		}
		items := make([]repr.UserPlaytime, 0, len(workIDs))
		var missing []string
		for _, s := range workIDs {
			id, ok := repr.ParseID(s)
			if !ok {
				p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids values must be decimal catalog ids.")
				p.Errors = []problem.FieldError{{Parameter: "work_ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
				return repr.List[repr.UserPlaytime]{}, p
			}
			row, gerr := c.Playtime.GetMine(ctx, uid, id)
			if gerr != nil {
				return repr.List[repr.UserPlaytime]{}, gerr
			}
			if row == nil {
				missing = append(missing, s)
				continue
			}
			items = append(items, repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(row.WorkID), Minutes: row.Minutes})
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	// The cursor was a bare time.RFC3339, which truncates to whole seconds:
	// two rows updated within the same second made `updated_at > cursor`
	// re-return the boundary row forever, and a limit=1 page crawl hung the
	// live suite at the 600s package timeout. Nano precision plus a work_id
	// tiebreak; a bare-time cursor from before this change still parses.
	since, sinceWorkID := time.Time{}, int64(0)
	if q.Cursor != "" {
		ts := q.Cursor
		if i := strings.LastIndexByte(ts, '|'); i >= 0 {
			id, ok := repr.ParseID(ts[i+1:])
			if !ok {
				return repr.List[repr.UserPlaytime]{}, collectInvalidCursor()
			}
			ts, sinceWorkID = ts[:i], id
		}
		t, perr := time.Parse(time.RFC3339, ts)
		if perr != nil {
			return repr.List[repr.UserPlaytime]{}, collectInvalidCursor()
		}
		since = t
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.Playtime.ListMine(ctx, uid, since, sinceWorkID, limit+1)
	if lerr != nil {
		return repr.List[repr.UserPlaytime]{}, lerr
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		s := last.UpdatedAt.UTC().Format(time.RFC3339Nano) + "|" + repr.ID(last.WorkID)
		next = &s
	}
	items := make([]repr.UserPlaytime, 0, len(rows))
	for _, r := range rows {
		items = append(items, repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(r.WorkID), Minutes: r.Minutes})
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Playtime.CountMine(ctx, uid); lerr != nil {
			return repr.List[repr.UserPlaytime]{}, lerr
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetPlaytime(ctx context.Context, workID int64) (repr.UserPlaytime, error) {
	if c == nil || c.Playtime == nil {
		return repr.UserPlaytime{}, problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserPlaytime{}, err
	}
	row, gerr := c.Playtime.GetMine(ctx, uid, workID)
	if gerr != nil {
		return repr.UserPlaytime{}, gerr
	}
	if row == nil {
		return repr.UserPlaytime{}, problem.New(problem.CodeNotFound, "", "", "No playtime on this work.")
	}
	return repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(row.WorkID), Minutes: row.Minutes}, nil
}

func (c *Catalog) PutPlaytime(ctx context.Context, workID int64, minutes int) (repr.UserPlaytime, error) {
	if c == nil || c.Playtime == nil {
		return repr.UserPlaytime{}, problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, client, err := requireUser(ctx)
	if err != nil {
		return repr.UserPlaytime{}, err
	}
	rec, gerr := c.Playtime.Report(ctx, catsvc.PlaytimeReport{
		ActorUID: uid, WorkID: workID, ClientID: client, Minutes: minutes,
	})
	if gerr != nil {
		return repr.UserPlaytime{}, playtimeErr(gerr)
	}
	return repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(rec.WorkID), Minutes: rec.Minutes}, nil
}

func (c *Catalog) DeletePlaytime(ctx context.Context, workID int64) error {
	if c == nil || c.Playtime == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "playtimes are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return playtimeErr(c.Playtime.DeleteMine(ctx, uid, workID))
}

func (c *Catalog) BatchPlaytimes(ctx context.Context, items []struct {
	WorkID  string
	Minutes int
}) (repr.List[repr.PlaytimeBatchItem], error) {
	out := make([]repr.PlaytimeBatchItem, 0, len(items))
	// maxItems on the request schema refuses this first with a 422; the
	// guard stays because the schema and this function are two different
	// authorities and only one of them is the one that writes rows.
	if len(items) > collect.MaxBatchItems {
		return repr.List[repr.PlaytimeBatchItem]{}, problem.New(problem.CodeTooManyIDs, "", "", "batch playtimes named more than 100 items.")
	}
	for i, it := range items {
		id, ok := repr.ParseID(it.WorkID)
		if !ok {
			p := problem.New(problem.CodeValidationFailed, "", "", "work_id must be a decimal catalog id.")
			p.Errors = []problem.FieldError{{Pointer: fmt.Sprintf("/items/%d/work_id", i), Reason: problem.ReasonInvalidFormat, Detail: it.WorkID}}
			out = append(out, repr.PlaytimeBatchItem{Status: 422, Problem: p})
			continue
		}
		rec, err := c.PutPlaytime(ctx, id, it.Minutes)
		if err != nil {
			p, ok := err.(*problem.Problem)
			if !ok {
				p = problem.New(problem.CodeInternalError, "", "", err.Error())
			}
			out = append(out, repr.PlaytimeBatchItem{Status: p.Status, Problem: p})
			continue
		}
		obj, wid, min := "playtime", rec.WorkID, rec.Minutes
		out = append(out, repr.PlaytimeBatchItem{Status: 200, Object: &obj, WorkID: &wid, Minutes: &min})
	}
	return repr.NewList(out, nil), nil
}

func playtimeErr(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case err == catsvc.ErrPlaytimeMinutesRange:
		p := problem.New(problem.CodeValidationFailed, "", "", "minutes is out of range.")
		p.Errors = []problem.FieldError{{Pointer: "/minutes", Reason: problem.ReasonOutOfRange, Detail: err.Error()}}
		return p
	case err == catsvc.ErrPlaytimeWorkUnavailable:
		return problem.New(problem.CodeNotFound, "", "", "work is not available for playtime.")
	case err == catsvc.ErrPlaytimeActorRequired, err == catsvc.ErrPlaytimeClientRequired:
		return problem.New(problem.CodeUserIdentityRequired, "", "", err.Error())
	}
	return err
}

func splitWorkIDs(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
