package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"

	"github.com/danielgtaylor/huma/v2"
)

type copyFolderInput struct {
	Body struct {
		FolderID string `json:"folder_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"The folder to copy: a public or unlisted one, or one of the bearer's own."`
		Name     string `json:"name,omitempty" maxLength:"100" example:"我的入坑清单" doc:"Name of the copy. Absent or blank: the source's name. Must not be used as a discriminant."`
	}
}
type folderProgressInput struct {
	FolderIDs string `query:"folder_ids" maxLength:"1024" doc:"Comma-separated folder ids, 1 to 50."`
}
type folderProgressOutput struct {
	Body repr.List[repr.FolderProgress]
}
type folderFollowSeenInput struct {
	FolderID string `path:"folder_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	Body     *struct {
		At string `json:"at,omitempty" format:"date-time" maxLength:"40" doc:"Mark seen up to this moment, RFC 3339. Absent means now. The mark moves forward only and never past now. A body is optional."`
	}
}
type folderFollowUpdatesOutput struct {
	Body repr.FolderFollowUpdates
}

// Registered before registerMeFolders and registerMeFolderFollows: Fiber
// matches in registration order, so /v2/me/folders/progress and
// /v2/me/followed-folders/updates must come before the {id} routes.
func registerMeFolderExtras(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity)
	huma.Register(api, huma.Operation{
		OperationID: "copyFolder", Method: http.MethodPost, Path: "/v2/me/folders/copies",
		Summary:     "Copy a folder into my folders",
		Description: "Creates a private folder of the bearer's own holding every live work of the source in the source's order, with the source owner's notes, tags and cover pick. The description starts with 源自「<source name>」 followed by the source's own description, so the copier decides whether to keep the credit by editing it; copied_from_folder_id carries the source's id whatever the description becomes. Copying the same source again makes another copy. 201 with the new folder. 404 when the source is private and not the bearer's; 422 at the folder cap (" + strconv.Itoa(model.FoldersPerUserMax) + "). A name sent here first passes the platform's word-list check. Requires a user access token with folder:write.",
		Tags:        me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, copyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyFolderProgress", Method: http.MethodGet, Path: "/v2/me/folders/progress",
		Summary:     "My progress through folders",
		Description: "For each folder: how many of its live works the bearer keeps in another folder of their own (collected), and how many carry each of the bearer's play states, for a \"you have played 12 of 50\" line on someone else's list. Works themselves are not listed; GET /v2/me/works?work_ids= answers per work. Folders the bearer cannot read (private and not theirs, or gone) are in missing. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyFolderProgress(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyFolderFollowUpdates", Method: http.MethodGet, Path: "/v2/me/followed-folders/updates",
		Summary:     "Count my followed folders with new works",
		Description: "The red dot: how many followed folders gained a work since the bearer last marked them seen. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyFolderFollowUpdates(cat))
	huma.Register(api, huma.Operation{
		OperationID: "markMyFolderFollowSeen", Method: http.MethodPost, Path: "/v2/me/followed-folders/{folder_id}/seen",
		Summary:     "Mark a followed folder seen",
		Description: "Call it when the bearer opens the folder. Its new_item_count returns to 0 and it leaves the updates count until it gains another work. The mark is account-wide, the same on every site. Answers the follow. 404 when the bearer does not follow the folder or it has gone private. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: writeErrs, SkipValidateParams: true,
	}, markMyFolderFollowSeen(cat))
}

func copyFolder(cat *Catalog) func(context.Context, *copyFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *copyFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &copyFolderInput{}
		}
		id, ok := repr.ParseID(in.Body.FolderID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.Body.FolderID))
		}
		rec, err := cat.CopyFolder(ctx, id, strings.TrimSpace(in.Body.Name))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderOutput{Body: rec}, nil
	}
}

func (c *Catalog) CopyFolder(ctx context.Context, sourceID int64, name string) (repr.UserFolder, error) {
	if c == nil || c.Folders == nil {
		return repr.UserFolder{}, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return repr.UserFolder{}, err
	}
	if err := c.checkFolderText(ctx, uid, folderText{name, "/name"}); err != nil {
		return repr.UserFolder{}, err
	}
	row, cerr := c.Folders.Copy(ctx, uid, sourceID, name)
	if cerr != nil {
		return repr.UserFolder{}, folderErr(cerr)
	}
	return c.folderViewOf(ctx, *row)
}

func listMyFolderProgress(cat *Catalog) func(context.Context, *folderProgressInput) (*folderProgressOutput, error) {
	return func(ctx context.Context, in *folderProgressInput) (*folderProgressOutput, error) {
		if in == nil {
			in = &folderProgressInput{}
		}
		page, err := cat.FolderProgress(ctx, splitWorkIDs(in.FolderIDs))
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderProgressOutput{Body: page}, nil
	}
}

func (c *Catalog) FolderProgress(ctx context.Context, raw []string) (repr.List[repr.FolderProgress], error) {
	var empty repr.List[repr.FolderProgress]
	if c == nil || c.Folders == nil {
		return empty, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound.")
	}
	uid, _, err := requireUser(ctx)
	if err != nil {
		return empty, err
	}
	if len(raw) == 0 || len(raw) > model.FolderProgressBatchMax {
		p := problem.New(problem.CodeInvalidParameter, "", "", "folder_ids takes 1 to "+strconv.Itoa(model.FolderProgressBatchMax)+" ids.")
		p.Errors = []problem.FieldError{{Parameter: "folder_ids", Reason: problem.ReasonInvalidFormat, Detail: "1 to " + strconv.Itoa(model.FolderProgressBatchMax) + " ids"}}
		return empty, p
	}
	ids, perr := parseBatchIDs(raw, "folder_ids")
	if perr != nil {
		return empty, perr
	}
	rows, gerr := c.Folders.Progress(ctx, uid, ids)
	if gerr != nil {
		return empty, folderErr(gerr)
	}
	byID := make(map[int64]repr.FolderProgress, len(rows))
	for _, r := range rows {
		byID[r.FolderID] = repr.FolderProgress{
			Object: "folder_progress", FolderID: repr.ID(r.FolderID), ItemCount: r.ItemCount, Collected: r.Collected,
			Wish: r.Wish, Doing: r.Doing, Done: r.Done, OnHold: r.OnHold, Dropped: r.Dropped,
		}
	}
	items := make([]repr.FolderProgress, 0, len(ids))
	var missing []string
	for i, id := range ids {
		v, ok := byID[id]
		if !ok {
			missing = append(missing, raw[i])
			continue
		}
		items = append(items, v)
	}
	return finishList(items, nil, int64(len(items)), collect.Query{Batch: true}, missing), nil
}

func getMyFolderFollowUpdates(cat *Catalog) func(context.Context, *struct{}) (*folderFollowUpdatesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*folderFollowUpdatesOutput, error) {
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		uid, _, err := requireUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		n, cerr := cat.Folders.CountUpdatedFollows(ctx, uid)
		if cerr != nil {
			return nil, catalogErr(ctx, folderErr(cerr))
		}
		return &folderFollowUpdatesOutput{Body: repr.FolderFollowUpdates{Object: "folder_follow_updates", UpdatedCount: n}}, nil
	}
}

func markMyFolderFollowSeen(cat *Catalog) func(context.Context, *folderFollowSeenInput) (*folderFollowOutput, error) {
	return func(ctx context.Context, in *folderFollowSeenInput) (*folderFollowOutput, error) {
		if in == nil {
			in = &folderFollowSeenInput{}
		}
		id, ok := repr.ParseID(in.FolderID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.FolderID))
		}
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		uid, _, err := requireUser(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		var at *time.Time
		if in.Body != nil && in.Body.At != "" {
			t, perr := time.Parse(time.RFC3339Nano, in.Body.At)
			if perr != nil {
				p := problem.New(problem.CodeValidationFailed, "", "", "at must be an RFC 3339 timestamp.")
				p.Errors = []problem.FieldError{{Pointer: "/at", Reason: problem.ReasonInvalidFormat, Detail: in.Body.At}}
				return nil, catalogErr(ctx, p)
			}
			at = &t
		}
		row, merr := cat.Folders.MarkFollowSeen(ctx, uid, id, at)
		if merr != nil {
			return nil, catalogErr(ctx, folderErr(merr))
		}
		views, verr := cat.folderFollowViews(ctx, []model.CatalogUserFolderFollow{*row})
		if verr != nil {
			return nil, catalogErr(ctx, verr)
		}
		if len(views) == 0 {
			return nil, catalogErr(ctx, problem.New(problem.CodeNotFound, "", "", "Not following this folder."))
		}
		return &folderFollowOutput{Body: views[0]}, nil
	}
}
