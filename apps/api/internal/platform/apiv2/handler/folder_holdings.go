package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listFolderHoldingsInput struct {
	WorkIDs string `query:"work_ids" maxLength:"4096" doc:"Comma-separated work ids, max 100. Batch read, no pagination."`
}

type listFolderHoldingsOutput struct {
	Body repr.List[repr.FolderHolding]
}

// Registered before /v2/me/folders/{id}: fiber matches routes in registration
// order, so the parameterised sibling would otherwise swallow this path and
// answer 400 for a folder id of "holdings".
func registerFolderHoldings(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "listMyFolderHoldings", Method: http.MethodGet, Path: "/v2/me/folders/holdings",
		Summary: "Which of my folders hold these works",
		Description: "Membership for up to 100 works in one request: for each work the bearer keeps in at least one folder, the ids of those folders. " +
			"A work the bearer holds nowhere is left out rather than answered with an empty array, and an id that names no work is simply held nowhere. " +
			"Folders of every visibility are searched — the bearer owns them all. " +
			"work_ids is required; this is a batch read with no pagination. " +
			"Requires a user access token with folder:read (folder:write also grants reads).",
		Tags:               []string{"me"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listMyFolderHoldings(cat))
}

func listMyFolderHoldings(cat *Catalog) func(context.Context, *listFolderHoldingsInput) (*listFolderHoldingsOutput, error) {
	return func(ctx context.Context, in *listFolderHoldingsInput) (*listFolderHoldingsOutput, error) {
		if in == nil {
			in = &listFolderHoldingsInput{}
		}
		page, err := cat.ListFolderHoldings(ctx, splitWorkIDs(in.WorkIDs))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &listFolderHoldingsOutput{Body: page}, nil
	}
}

func (c *Catalog) ListFolderHoldings(ctx context.Context, workIDs []string) (repr.List[repr.FolderHolding], error) {
	var empty repr.List[repr.FolderHolding]
	if c == nil || c.Folders == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	if len(workIDs) == 0 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids is required.")
		p.Errors = []problem.FieldError{{
			Parameter: "work_ids", Reason: problem.ReasonRequired,
			Detail: "send 1 to 100 comma-separated work ids",
		}}
		return empty, p
	}
	if len(workIDs) > collect.MaxBatchItems {
		return empty, problem.New(problem.CodeTooManyIDs, "", "", "work_ids named more than 100 items.")
	}
	ids := make([]int64, 0, len(workIDs))
	for _, s := range workIDs {
		id, ok := repr.ParseID(s)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "work_ids values must be decimal catalog ids.")
			p.Errors = []problem.FieldError{{Parameter: "work_ids", Reason: problem.ReasonInvalidFormat, Detail: s}}
			return empty, p
		}
		ids = append(ids, id)
	}
	rows, lerr := c.Folders.Holdings(ctx, uid, ids)
	if lerr != nil {
		return empty, folderErr(lerr)
	}
	held := make(map[int64][]string, len(rows))
	for _, r := range rows {
		held[r.WorkID] = append(held[r.WorkID], repr.ID(r.FolderID))
	}
	items := make([]repr.FolderHolding, 0, len(held))
	seen := make(map[int64]bool, len(ids))
	for _, id := range ids {
		folders := held[id]
		if len(folders) == 0 || seen[id] {
			continue
		}
		seen[id] = true
		items = append(items, repr.FolderHolding{
			Object: "folder_holding", WorkID: repr.ID(id), FolderIDs: folders,
		})
	}
	return finishList(items, nil, int64(len(items)), collect.Query{Batch: true}, nil), nil
}
