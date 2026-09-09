package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catalogPerm "api/internal/platform/catalog/perm"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type moderationFolderInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}

type purgeFoldersInput struct {
	UID string `path:"uid" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"The account whose folders to remove."`
}

type listUserFoldersInput struct {
	CollectionInput
	UID string `path:"uid" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"The account whose folders to list."`
}

type purgeFoldersOutput struct {
	Body repr.FolderPurgeReceipt
}

type patchModerationFolderInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	Body struct {
		Name        *string `json:"name,omitempty" maxLength:"100" doc:"New display name. Must not be used as a discriminant. An empty string is accepted here and nowhere else: the abuse a moderator removes is usually the name, and picking a replacement is the owner's to do."`
		Description *string `json:"description,omitempty" maxLength:"500" doc:"New note; an empty string clears it. Must not be used as a discriminant."`
		Visibility  *string `json:"visibility,omitempty" enum:"private,public" doc:"private takes the folder off /v2/folders without deleting anything."`
	}
}

// Folders hold user-authored text on a public face, so the standing to act on
// them is the same "can reach a verdict" union the rest of /v2/moderation uses.
// What a moderator may touch stops at that text: not the works the folder
// holds, and not is_default, which is the owner's own navigation.
func registerModerationFolders(api huma.API, cat *Catalog) {
	tags := []string{"moderation"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusServiceUnavailable, http.StatusUnprocessableEntity)

	huma.Register(api, huma.Operation{
		OperationID: "patchModerationFolder", Method: http.MethodPatch, Path: "/v2/moderation/folders/{id}",
		Summary:     "Moderate a folder's text",
		Description: "Rewrite or blank the name and description of any user's folder, or force it private. Does not touch the folder's items. Requires a user access token whose holder moderates.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, patchModerationFolder(cat))

	huma.Register(api, huma.Operation{
		OperationID: "deleteModerationFolder", Method: http.MethodDelete, Path: "/v2/moderation/folders/{id}",
		Summary:     "Delete any folder",
		Description: "Deletes the folder and its items. Unlike the owner's own delete this accepts the default folder: is_default is set by the folder's owner, so honouring it here would let anyone make a folder undeletable. 204 with no body. Requires a user access token whose holder moderates.",
		Tags:        tags, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteModerationFolder(cat))

	huma.Register(api, huma.Operation{
		OperationID: "listUserFolders", Method: http.MethodGet, Path: "/v2/moderation/users/{uid}/folders",
		Summary:     "List every folder an account holds",
		Description: "The read preview of the purge on this same path: what the DELETE would remove. Every folder the account owns, private ones included, id-ascending and keyset-paginated like /v2/me/folders. Items are not listed — the folder's item_count is what a confirmation needs. An account holding none is 200 with an empty list, not 404. Requires a user access token whose holder moderates.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listUserFolders(cat))

	huma.Register(api, huma.Operation{
		OperationID: "purgeUserFolders", Method: http.MethodDelete, Path: "/v2/moderation/users/{uid}/folders",
		Summary:     "Remove every folder an account holds",
		Description: "What an account deletion reaches for: all of one account's folders, their memberships and the import provenance naming them, in one transaction. Answers a receipt with the counts; an account holding none is 200 with zeros, not 404. Requires a user access token whose holder moderates.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, purgeUserFolders(cat))
}

// The account-deletion path on a product site has to reach the canonical store
// or the purge leaves the person's collections behind on a face they no longer
// have any way to open.
func purgeUserFolders(cat *Catalog) func(context.Context, *purgeFoldersInput) (*purgeFoldersOutput, error) {
	return func(ctx context.Context, in *purgeFoldersInput) (*purgeFoldersOutput, error) {
		if in == nil {
			in = &purgeFoldersInput{}
		}
		if err := requireFolderModerator(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		uid, ok := repr.ParseID(in.UID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.UID))
		}
		folders, items, err := cat.Folders.PurgeOwner(ctx, uid)
		if err != nil {
			return nil, catalogErr(ctx, folderErr(err))
		}
		return &purgeFoldersOutput{Body: repr.FolderPurgeReceipt{
			Object: "folder_purge", OwnerUID: repr.ID(uid),
			FoldersDeleted: folders, ItemsDeleted: items,
		}}, nil
	}
}

func listUserFolders(cat *Catalog) func(context.Context, *listUserFoldersInput) (*listFoldersOutput, error) {
	return func(ctx context.Context, in *listUserFoldersInput) (*listFoldersOutput, error) {
		if in == nil {
			in = &listUserFoldersInput{}
		}
		// Parsed before the gate, like every other collection lane: a malformed
		// cursor or an oversized limit discloses nothing, and answering 403 to it
		// would make the same request 400 or 403 depending on who asked.
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderSpec())
		if err != nil {
			return nil, err
		}
		if err := requireFolderModerator(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		uid, ok := repr.ParseID(in.UID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.UID))
		}
		page, lerr := cat.ListUserFolders(ctx, uid, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFoldersOutput{Body: page}, nil
	}
}

func (c *Catalog) ListUserFolders(ctx context.Context, ownerUID int64, q collect.Query) (repr.List[repr.UserFolder], error) {
	var empty repr.List[repr.UserFolder]
	sinceID := int64(0)
	if q.Cursor != "" {
		id, ok := repr.ParseID(q.Cursor)
		if !ok {
			return empty, collectInvalidCursor()
		}
		sinceID = id
	}
	limit := q.Limit
	if limit <= 0 {
		limit = collect.DefaultLimit
	}
	rows, err := c.Folders.ListMine(ctx, ownerUID, sinceID, limit+1)
	if err != nil {
		return empty, folderErr(err)
	}
	var next *string
	if len(rows) > limit {
		rows = rows[:limit]
		s := repr.ID(rows[len(rows)-1].ID)
		next = &s
	}
	items := make([]repr.UserFolder, 0, len(rows))
	for _, r := range rows {
		items = append(items, folderView(r))
	}
	var total int64
	if q.IncludeTotal {
		if total, err = c.Folders.CountMine(ctx, ownerUID); err != nil {
			return empty, folderErr(err)
		}
	}
	return finishList(items, next, total, q, nil), nil
}

func requireFolderModerator(ctx context.Context) error {
	if _, _, err := requireUser(ctx); err != nil {
		return err
	}
	if !catalogPerm.Moderates(rolesFrom(ctx)) {
		return problem.New(problem.CodePermissionRequired, "", "", "this operation requires moderation standing.")
	}
	return nil
}

func patchModerationFolder(cat *Catalog) func(context.Context, *patchModerationFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *patchModerationFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &patchModerationFolderInput{}
		}
		if err := requireFolderModerator(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		p := catsvc.FolderPatch{Name: in.Body.Name, Description: in.Body.Description}
		if in.Body.Visibility != nil {
			vis, ok := folderVisibilityValue(*in.Body.Visibility)
			if !ok {
				return nil, catalogErr(ctx, folderErr(catsvc.ErrFolderBadVisibility))
			}
			p.Visibility = &vis
		}
		row, err := cat.Folders.ModerationPatch(ctx, id, p)
		if err != nil {
			return nil, catalogErr(ctx, folderErr(err))
		}
		return &folderOutput{Body: folderView(*row)}, nil
	}
}

func deleteModerationFolder(cat *Catalog) func(context.Context, *moderationFolderInput) (*struct{}, error) {
	return func(ctx context.Context, in *moderationFolderInput) (*struct{}, error) {
		if in == nil {
			in = &moderationFolderInput{}
		}
		if err := requireFolderModerator(ctx); err != nil {
			return nil, catalogErr(ctx, err)
		}
		if cat == nil || cat.Folders == nil {
			return nil, catalogErr(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "folders are not bound."))
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		if err := cat.Folders.ModerationDelete(ctx, id); err != nil {
			return nil, catalogErr(ctx, folderErr(err))
		}
		return &struct{}{}, nil
	}
}
