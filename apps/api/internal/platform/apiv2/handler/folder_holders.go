package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

const folderHoldersDefaultLimit = 100

type listFolderHoldersInput struct {
	WorkID string `query:"work_id" maxLength:"20" doc:"Catalog work id. Required."`
	Cursor string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit  string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 100. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
}

type listFolderHoldersOutput struct {
	Body repr.List[repr.FolderHolder]
}

// Registered before /v2/folders/{id} for the same reason the holdings face is
// registered before /v2/me/folders/{id}: fiber matches in registration order.
func registerFolderHolders(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "listFolderHolders", Method: http.MethodGet, Path: "/v2/folders/holders",
		Summary: "Who holds this work in a folder",
		Description: "The accounts that keep one work in a favorite folder, owner_uid-ascending, one page at a time. " +
			"Folders of every visibility count: this face exists so a service can fan a notification out to the people who follow a work, " +
			"and a private folder is still a person waiting to hear about it. It answers uids and nothing else — no folder ids, names, " +
			"visibility or counts — so it cannot be walked into a \"who favourited what\" index. A work nobody holds is an empty list, not a 404. " +
			"Requires an application key with the folder_holders:read scope on top of catalog:read; a user access token is refused. " +
			"The scope is granted by an operator, not self-service, because the answer is somebody else's private collection.",
		Tags:               []string{"folders"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listFolderHolders(cat))
}

func listFolderHolders(cat *Catalog) func(context.Context, *listFolderHoldersInput) (*listFolderHoldersOutput, error) {
	return func(ctx context.Context, in *listFolderHoldersInput) (*listFolderHoldersOutput, error) {
		if in == nil {
			in = &listFolderHoldersInput{}
		}
		limit := in.Limit
		if strings.TrimSpace(limit) == "" {
			limit = strconv.Itoa(folderHoldersDefaultLimit)
		}
		q, perr := collect.Parse(collect.Raw{Cursor: in.Cursor, Limit: limit}, collect.FolderHolderSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		workID, err := requiredIDParam("work_id", in.WorkID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListFolderHolders(ctx, workID, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFolderHoldersOutput{Body: page}, nil
	}
}

func (c *Catalog) ListFolderHolders(ctx context.Context, workID int64, q collect.Query) (repr.List[repr.FolderHolder], error) {
	var empty repr.List[repr.FolderHolder]
	if c == nil || c.Folders == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	if _, perr := requireAppClient(ctx); perr != nil {
		return empty, perr
	}
	sinceUID := int64(0)
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return empty, collectInvalidCursor()
		}
		sinceUID = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = folderHoldersDefaultLimit
	}
	uids, err := c.Folders.HoldersOfWork(ctx, workID, sinceUID, limit+1)
	if err != nil {
		return empty, folderErr(err)
	}
	var next *string
	if len(uids) > limit {
		uids = uids[:limit]
		s := repr.ID(uids[len(uids)-1])
		next = &s
	}
	items := make([]repr.FolderHolder, 0, len(uids))
	for _, uid := range uids {
		items = append(items, repr.FolderHolder{Object: "folder_holder", OwnerUID: repr.ID(uid)})
	}
	return finishList(items, next, 0, q, nil), nil
}
