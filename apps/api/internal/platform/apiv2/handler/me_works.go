package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listMyWorksInput struct {
	WorkIDs string `query:"work_ids" maxLength:"4096" doc:"Comma-separated work ids, max 100. Batch read, no pagination."`
}

type listMyWorksOutput struct {
	Body repr.List[repr.UserWork]
}

func registerMeWorks(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "listMyWorks", Method: http.MethodGet, Path: "/v2/me/works",
		Summary: "My folders, playtime and play state for these works",
		Description: "What the bearer has recorded about up to 100 works, in one request: the folders holding each work, its playtime and its play state. " +
			"One item per distinct work id, in the order asked. A work the bearer has recorded nothing about still gets an item, with empty folder_ids and null playtime and work_state, and so does an id that names no work. " +
			"The values are the ones /v2/me/folders/holdings, /v2/me/playtimes and /v2/me/work-states answer; this face saves a client from asking all three. " +
			"Cover votes are not included: they need catalog:edit, and /v2/me/cover-votes lists them. " +
			"work_ids is required; this is a batch read with no pagination. " +
			"Requires a user access token with folder:read (folder:write also grants reads).",
		Tags:               []string{"me"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listMyWorks(cat))
}

func listMyWorks(cat *Catalog) func(context.Context, *listMyWorksInput) (*listMyWorksOutput, error) {
	return func(ctx context.Context, in *listMyWorksInput) (*listMyWorksOutput, error) {
		if in == nil {
			in = &listMyWorksInput{}
		}
		page, err := cat.ListMyWorks(ctx, splitWorkIDs(in.WorkIDs))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &listMyWorksOutput{Body: page}, nil
	}
}

func (c *Catalog) ListMyWorks(ctx context.Context, workIDs []string) (repr.List[repr.UserWork], error) {
	var empty repr.List[repr.UserWork]
	if c == nil || c.Folders == nil || c.Playtime == nil || c.WorkStates == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "my works are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	if len(workIDs) == 0 {
		return empty, workIDsRequired()
	}
	ids, err := parseWorkIDs(workIDs)
	if err != nil {
		return empty, err
	}
	holdings, err := c.Folders.Holdings(ctx, uid, ids)
	if err != nil {
		return empty, folderErr(err)
	}
	playtimes, err := c.Playtime.ListMineFor(ctx, uid, ids)
	if err != nil {
		return empty, err
	}
	states, err := c.WorkStates.ListMineFor(ctx, uid, ids)
	if err != nil {
		return empty, err
	}

	folders := make(map[int64][]string, len(holdings))
	for _, h := range holdings {
		folders[h.WorkID] = append(folders[h.WorkID], repr.ID(h.FolderID))
	}
	playtime := make(map[int64]*repr.UserPlaytime, len(playtimes))
	for _, p := range playtimes {
		playtime[p.WorkID] = &repr.UserPlaytime{Object: "playtime", WorkID: repr.ID(p.WorkID), Minutes: p.Minutes}
	}
	state := make(map[int64]*repr.UserWorkState, len(states))
	for _, s := range states {
		v := userWorkStateRepr(s)
		state[s.WorkID] = &v
	}
	items := make([]repr.UserWork, 0, len(ids))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		held := folders[id]
		if held == nil {
			held = []string{}
		}
		items = append(items, repr.UserWork{
			Object: "user_work", WorkID: repr.ID(id), FolderIDs: held,
			Playtime: playtime[id], WorkState: state[id],
		})
	}
	return finishList(items, nil, int64(len(items)), collect.Query{Batch: true}, nil), nil
}

func workIDsRequired() error {
	p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids is required.")
	p.Errors = []problem.FieldError{{
		Parameter: "work_ids", Reason: problem.ReasonRequired,
		Detail: "send 1 to 100 comma-separated work ids",
	}}
	return p
}

func parseWorkIDs(workIDs []string) ([]int64, error) {
	if len(workIDs) > collect.MaxBatchItems {
		return nil, problem.New(problem.CodeTooManyIDs, "", "", "work_ids named more than 100 items.")
	}
	ids := make([]int64, 0, len(workIDs))
	for _, s := range workIDs {
		id, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "work_ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return nil, p
		}
		ids = append(ids, id)
	}
	return ids, nil
}
