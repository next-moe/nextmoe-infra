package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type listFolderFollowsInput struct {
	CollectionInput
	FolderIDs string `query:"folder_ids" maxLength:"4096" doc:"Comma-separated folder ids, max 100. Batch read, no pagination: which of these the bearer follows."`
}
type listFolderFollowsOutput struct {
	Body repr.List[repr.FolderFollow]
}
type folderFollowInput struct {
	FolderID string `path:"folder_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}
type folderFollowOutput struct {
	Body repr.FolderFollow
}
type reportFolderInput struct {
	Body struct {
		FolderID string `json:"folder_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"The public folder being reported."`
		Reason   string `json:"reason" enum:"abuse,spam,illegal,rating_mislabel,copyright,other" example:"spam" doc:"Why the folder is reported."`
		Note     string `json:"note,omitempty" maxLength:"500" doc:"The reporter's own words for the moderator. Must not be used as a discriminant."`
		WorkID   string `json:"work_id,omitempty" maxLength:"20" pattern:"^[0-9]+$" doc:"Report the note the owner wrote on this work instead of the folder's name and description. The work must be in the folder and carry a note."`
	}
}

func registerMeFolderFollows(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity)
	huma.Register(api, huma.Operation{
		OperationID: "listMyFolderFollows", Method: http.MethodGet, Path: "/v2/me/followed-folders",
		Summary:     "List my followed folders",
		Description: "The public and unlisted folders the bearer follows, newest follow first, each with the folder as GET /v2/folders/{id} answers it. new_item_count says how many works each gained since the bearer last opened it (POST /v2/me/followed-folders/{folder_id}/seen). A followed folder its owner has since made private drops out of this list (and out of folder_ids=) but the follow is kept, so it comes back if the folder is made public or unlisted again. The list is private to its owner; only the count is public (follower_count on the folder). Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyFolderFollows(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyFolderFollow", Method: http.MethodGet, Path: "/v2/me/followed-folders/{folder_id}",
		Summary:     "Get my follow on one folder",
		Description: "404 when the bearer does not follow this folder, or it has gone private. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyFolderFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyFolderFollow", Method: http.MethodPut, Path: "/v2/me/followed-folders/{folder_id}",
		Summary:     "Follow a folder",
		Description: "No request body. Idempotent: following an already-followed folder answers the stored row and writes nothing. An unlisted folder can be followed by anyone who has its id. 404 when the folder is private or does not exist; 422 for the bearer's own folder. At most 1000 followed folders. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, putMyFolderFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyFolderFollow", Method: http.MethodDelete, Path: "/v2/me/followed-folders/{folder_id}",
		Summary:     "Unfollow a folder",
		Description: "204 with no body, also when the bearer was not following, and also for a folder that has since gone private. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyFolderFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "reportFolder", Method: http.MethodPost, Path: "/v2/me/folder-reports",
		Summary:     "Report a public folder",
		Description: "Files the bearer's report with the platform's moderation inbox: the folder's name and description as subject (nextmoe, user_folder), or with work_id one item's note as (nextmoe, user_folder_note). The text as it stands now travels with the report. A moderator acts through /v2/moderation/folders. Unlisted folders can be reported like public ones. 404 when the folder is private or does not exist; 422 when work_id names a work the folder does not hold or that carries no note. 204 with no body. 429 when this account has filed too many reports; 503 when the moderation inbox cannot be reached. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: append(writeErrs, http.StatusTooManyRequests), DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, reportFolder(cat))
}

func reportFolder(cat *Catalog) func(context.Context, *reportFolderInput) (*struct{}, error) {
	return func(ctx context.Context, in *reportFolderInput) (*struct{}, error) {
		if in == nil {
			in = &reportFolderInput{}
		}
		id, ok := repr.ParseID(in.Body.FolderID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.Body.FolderID))
		}
		var workID int64
		if in.Body.WorkID != "" {
			if workID, ok = repr.ParseID(in.Body.WorkID); !ok {
				return nil, catalogErr(ctx, problemInvalidID(in.Body.WorkID))
			}
		}
		if err := cat.ReportFolder(ctx, id, in.Body.Reason, strings.TrimSpace(in.Body.Note), workID); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func (c *Catalog) folderFollowViews(ctx context.Context, rows []model.CatalogUserFolderFollow) ([]repr.FolderFollow, error) {
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.FolderID
	}
	folders, err := c.Folders.ReadableFoldersByID(ctx, ids)
	if err != nil {
		return nil, err
	}
	views, err := c.folderViews(ctx, folders)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]repr.UserFolder, len(views))
	for _, v := range views {
		byID[v.ID] = v
	}
	var fresh map[int64]int
	if len(rows) > 0 {
		if fresh, err = c.Folders.FollowNewItems(ctx, rows[0].ActorUID, ids); err != nil {
			return nil, err
		}
	}
	out := make([]repr.FolderFollow, 0, len(rows))
	for _, r := range rows {
		f, ok := byID[repr.ID(r.FolderID)]
		if !ok {
			continue
		}
		out = append(out, repr.FolderFollow{
			Object: "folder_follow", FolderID: f.ID, Folder: f, NewItemCount: fresh[r.FolderID],
			SeenAt:    r.SeenAt.UTC().Format(time.RFC3339Nano),
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return out, nil
}

func (c *Catalog) ListFolderFollows(ctx context.Context, q collect.Query, folderIDs []string) (repr.List[repr.FolderFollow], error) {
	var empty repr.List[repr.FolderFollow]
	if c == nil || c.Folders == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	if len(folderIDs) > 0 {
		ids, perr := parseBatchIDs(folderIDs, "folder_ids")
		if perr != nil {
			return empty, perr
		}
		rows, gerr := c.Folders.ListFollowsFor(ctx, uid, ids)
		if gerr != nil {
			return empty, folderErr(gerr)
		}
		views, verr := c.folderFollowViews(ctx, rows)
		if verr != nil {
			return empty, verr
		}
		byFolder := make(map[string]repr.FolderFollow, len(views))
		for _, v := range views {
			byFolder[v.FolderID] = v
		}
		items := make([]repr.FolderFollow, 0, len(ids))
		var missing []string
		for i, id := range ids {
			v, ok := byFolder[repr.ID(id)]
			if !ok {
				missing = append(missing, folderIDs[i])
				continue
			}
			items = append(items, v)
		}
		return finishList(items, nil, int64(len(items)), collect.Query{Batch: true, IncludeTotal: q.IncludeTotal}, missing), nil
	}
	var before int64
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return empty, collectInvalidCursor()
		}
		before = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, lerr := c.Folders.ListFollows(ctx, uid, before, limit+1)
	if lerr != nil {
		return empty, folderErr(lerr)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items, verr := c.folderFollowViews(ctx, rows)
	if verr != nil {
		return empty, verr
	}
	var total int64
	if q.IncludeTotal {
		if total, lerr = c.Folders.CountFollows(ctx, uid); lerr != nil {
			return empty, folderErr(lerr)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func (c *Catalog) GetFolderFollow(ctx context.Context, folderID int64) (repr.FolderFollow, error) {
	if c == nil || c.Folders == nil {
		return repr.FolderFollow{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.FolderFollow{}, err
	}
	rows, gerr := c.Folders.ListFollowsFor(ctx, uid, []int64{folderID})
	if gerr != nil {
		return repr.FolderFollow{}, folderErr(gerr)
	}
	views, verr := c.folderFollowViews(ctx, rows)
	if verr != nil {
		return repr.FolderFollow{}, verr
	}
	if len(views) == 0 {
		return repr.FolderFollow{}, problem.New(problem.CodeNotFound, "", "", "Not following this folder.")
	}
	return views[0], nil
}

func (c *Catalog) PutFolderFollow(ctx context.Context, folderID int64) (repr.FolderFollow, error) {
	if c == nil || c.Folders == nil {
		return repr.FolderFollow{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, clientID, err := requireUser(ctx)
	if err != nil {
		return repr.FolderFollow{}, err
	}
	row, ferr := c.Folders.Follow(ctx, uid, folderID, clientID, siteFrom(ctx))
	if ferr != nil {
		return repr.FolderFollow{}, folderErr(ferr)
	}
	views, verr := c.folderFollowViews(ctx, []model.CatalogUserFolderFollow{*row})
	if verr != nil {
		return repr.FolderFollow{}, verr
	}
	if len(views) == 0 {
		return repr.FolderFollow{}, folderErr(catsvc.ErrFolderNotFound)
	}
	return views[0], nil
}

func (c *Catalog) DeleteFolderFollow(ctx context.Context, folderID int64) error {
	if c == nil || c.Folders == nil {
		return problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return err
	}
	return folderErr(c.Folders.Unfollow(ctx, uid, folderID))
}

func listMyFolderFollows(cat *Catalog) func(context.Context, *listFolderFollowsInput) (*listFolderFollowsOutput, error) {
	return func(ctx context.Context, in *listFolderFollowsInput) (*listFolderFollowsOutput, error) {
		if in == nil {
			in = &listFolderFollowsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderFollowSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListFolderFollows(ctx, q, splitWorkIDs(in.FolderIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFolderFollowsOutput{Body: page}, nil
	}
}

func folderFollowID(ctx context.Context, in *folderFollowInput) (int64, error) {
	if in == nil {
		in = &folderFollowInput{}
	}
	id, ok := repr.ParseID(in.FolderID)
	if !ok {
		return 0, catalogErr(ctx, problemInvalidID(in.FolderID))
	}
	return id, nil
}

func getMyFolderFollow(cat *Catalog) func(context.Context, *folderFollowInput) (*folderFollowOutput, error) {
	return func(ctx context.Context, in *folderFollowInput) (*folderFollowOutput, error) {
		id, err := folderFollowID(ctx, in)
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetFolderFollow(ctx, id)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &folderFollowOutput{Body: rec}, nil
	}
}

func putMyFolderFollow(cat *Catalog) func(context.Context, *folderFollowInput) (*folderFollowOutput, error) {
	return func(ctx context.Context, in *folderFollowInput) (*folderFollowOutput, error) {
		id, err := folderFollowID(ctx, in)
		if err != nil {
			return nil, err
		}
		rec, perr := cat.PutFolderFollow(ctx, id)
		if perr != nil {
			return nil, catalogErr(ctx, perr)
		}
		return &folderFollowOutput{Body: rec}, nil
	}
}

func deleteMyFolderFollow(cat *Catalog) func(context.Context, *folderFollowInput) (*struct{}, error) {
	return func(ctx context.Context, in *folderFollowInput) (*struct{}, error) {
		id, err := folderFollowID(ctx, in)
		if err != nil {
			return nil, err
		}
		if derr := cat.DeleteFolderFollow(ctx, id); derr != nil {
			return nil, catalogErr(ctx, derr)
		}
		return &struct{}{}, nil
	}
}
