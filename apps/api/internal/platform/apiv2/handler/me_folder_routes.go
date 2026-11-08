package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listFoldersInput struct {
	CollectionInput
	ContainsWorkID string `query:"contains_work_id" maxLength:"20" doc:"Keep only the folders that already hold this catalog work. This is the add-to-folder picker's question; without it a client had to read every folder it owns and probe each one."`
}
type listFoldersOutput struct {
	Body repr.List[repr.UserFolder]
}
type createFolderInput struct {
	Body struct {
		Name        string   `json:"name" minLength:"1" maxLength:"100" example:"想玩" doc:"Display name. Must not be used as a discriminant."`
		Description string   `json:"description,omitempty" maxLength:"500" example:"还没开始的作品" doc:"Owner's own note. Must not be used as a discriminant."`
		Visibility  string   `json:"visibility,omitempty" enum:"private,unlisted,public" example:"private" doc:"Defaults to private. unlisted opens from its link but stays out of every listing."`
		IsDefault   bool     `json:"is_default,omitempty" example:"false" doc:"true makes this the user's default folder and clears the flag on the previous holder."`
		Tags        []string `json:"tags,omitempty" maxItems:"5" example:"[\"催泪\",\"入坑向\"]" doc:"Up to 5 tags of at most 16 characters each, used to find folders by tag. Trimmed, a leading # dropped, duplicates (ignoring case) dropped. Must not be used as a discriminant."`
	}
}
type folderOutput struct {
	Body repr.UserFolder
}
type getFolderInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}
type patchFolderInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	Body struct {
		Name        *string   `json:"name,omitempty" minLength:"1" maxLength:"100" example:"已通关" doc:"New display name. Must not be used as a discriminant."`
		Description *string   `json:"description,omitempty" maxLength:"500" example:"全路线通关的作品" doc:"New note; an empty string clears it. Must not be used as a discriminant."`
		Visibility  *string   `json:"visibility,omitempty" enum:"private,unlisted,public" example:"public" doc:"New visibility."`
		IsDefault   *bool     `json:"is_default,omitempty" example:"true" doc:"Only true is accepted: it moves the default flag to this folder. Sending false is 422 — set the flag on another folder instead."`
		Tags        *[]string `json:"tags,omitempty" maxItems:"5" example:"[\"催泪\"]" doc:"Replaces every tag; an empty array clears them. Same rules as on create. Must not be used as a discriminant."`
		CoverWorkID *string   `json:"cover_work_id,omitempty" maxLength:"20" pattern:"^[0-9]*$" example:"1" doc:"Pick a work this folder holds as its cover (422 otherwise); an empty string clears the pick. Removing that work from the folder clears it too."`
	}
}
type listFolderItemsInput struct {
	CollectionInput
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}
type listFolderItemsOutput struct {
	Body repr.List[repr.UserFolderItem]
}
type putFolderItemInput struct {
	ID     string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
}
type folderItemOutput struct {
	Body repr.UserFolderItem
}
type patchFolderItemInput struct {
	ID     string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	WorkID string `path:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
	Body   struct {
		Note         *string `json:"note,omitempty" maxLength:"500" example:"入坑首选，共通线就很好哭" doc:"New note on this work; an empty string clears it. Trimmed. Must not be used as a discriminant."`
		AfterWorkID  *string `json:"after_work_id,omitempty" maxLength:"20" pattern:"^[0-9]+$" doc:"Move this work to directly after that one in the folder's display order (sort=position)."`
		BeforeWorkID *string `json:"before_work_id,omitempty" maxLength:"20" pattern:"^[0-9]+$" doc:"Move this work to directly before that one. Send after_work_id or before_work_id, not both: to move to the top name the current first work, to the end the current last."`
	}
}

// Named, not anonymous: huma names an anonymous `Items []struct{...}` element
// "Item" after the field, and two batch routes with that shape panic the
// registry with "duplicate name: Item" at startup.
type folderBatchEntry struct {
	WorkID string `json:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" example:"1" doc:"Catalog work id."`
}

type batchFolderItemsInput struct {
	ID   string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
	Body struct {
		Items []folderBatchEntry `json:"items" maxItems:"100" doc:"At most 100 items."`
	}
}
type batchFolderItemsOutput struct {
	Status int
	Body   repr.List[repr.FolderItemBatchItem]
}

func registerMeFolders(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	writeErrs := append(errs, http.StatusUnprocessableEntity)

	huma.Register(api, huma.Operation{
		OperationID: "listMyFolders", Method: http.MethodGet, Path: "/v2/me/folders",
		Summary: "List my folders", Description: "The bearer user's favorite folders, id-ascending. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyFolders(cat))
	huma.Register(api, huma.Operation{
		OperationID: "createMyFolder", Method: http.MethodPost, Path: "/v2/me/folders",
		Summary: "Create a folder", Description: "name and description first pass the platform's word-list check (refused: 422 at /name or /description, nothing written; check unavailable: written anyway); a public folder's text then goes to the platform's content scan. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyFolder", Method: http.MethodGet, Path: "/v2/me/folders/{id}",
		Summary: "Get one folder", Description: "404 when the bearer owns no folder with this id. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyFolder", Method: http.MethodPatch, Path: "/v2/me/folders/{id}",
		Summary: "Update a folder", Description: "Partial update: name, description, visibility, is_default. A changed name or description first passes the platform's word-list check (refused: 422 at /name or /description, nothing written; check unavailable: written anyway). While the folder is public, its text goes to the platform's content scan after every change, and making it public scans its text and every item note. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyFolder", Method: http.MethodDelete, Path: "/v2/me/folders/{id}",
		Summary: "Delete a folder", Description: "Deletes the folder and its items. The default folder is refused with 422; move the flag first. 204 with no body. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyFolderItems", Method: http.MethodGet, Path: "/v2/me/folders/{id}/items",
		Summary: "List a folder's items", Description: "sort=updated (default) is keyset-paginated by updated_at; the final next_cursor can be stored and replayed later as an incremental-sync watermark (deletions are not replayed). sort=position is the owner's display order: new works are appended at the end and PATCH moves them. A cursor belongs to the sort it was issued for. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyFolderItems(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyFolderItem", Method: http.MethodGet, Path: "/v2/me/folders/{id}/items/{work_id}",
		Summary: "Get one folder item", Description: "One membership with its note and position, e.g. to prefill the note when the work is already in the folder. 404 when the bearer owns no folder with this id or the work is not in it. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyFolderItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyFolderItem", Method: http.MethodPut, Path: "/v2/me/folders/{id}/items/{work_id}",
		Summary: "Add a work to a folder", Description: "Naturally idempotent: re-adding an existing membership answers the stored row and touches nothing. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, putMyFolderItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyFolderItem", Method: http.MethodPatch, Path: "/v2/me/folders/{id}/items/{work_id}",
		Summary: "Update a folder item", Description: "Partial update of one membership: note, and its place in the display order (after_work_id or before_work_id). Both move updated_at, so the change reaches incremental sync; an unchanged note writes nothing. On a public folder a changed note goes to the platform's content scan. 404 when the work is not in the folder. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyFolderItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyFolderItem", Method: http.MethodDelete, Path: "/v2/me/folders/{id}/items/{work_id}",
		Summary: "Remove a work from a folder", Description: "204 with no body, also when the work was not in the folder. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyFolderItem(cat))
	huma.Register(api, huma.Operation{
		OperationID: "batchMyFolderItems", Method: http.MethodPost, Path: "/v2/me/folders/{id}/items",
		Summary: "Batch add works to a folder", Description: "207 Multi-Status. Each item is a folder_item or a problem. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: 207, SkipValidateParams: true,
	}, batchMyFolderItems(cat))
}

func listMyFolders(cat *Catalog) func(context.Context, *listFoldersInput) (*listFoldersOutput, error) {
	return func(ctx context.Context, in *listFoldersInput) (*listFoldersOutput, error) {
		if in == nil {
			in = &listFoldersInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderSpec())
		if err != nil {
			return nil, err
		}
		containsWorkID, err := optionalIDParam("contains_work_id", in.ContainsWorkID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListFolders(ctx, q, containsWorkID)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFoldersOutput{Body: page}, nil
	}
}

func createMyFolder(cat *Catalog) func(context.Context, *createFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *createFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &createFolderInput{}
		}
		rec, err := cat.CreateFolder(ctx, in.Body.Name, in.Body.Description, in.Body.Visibility, in.Body.IsDefault, in.Body.Tags)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderOutput{Body: rec}, nil
	}
}

func getMyFolder(cat *Catalog) func(context.Context, *getFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *getFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &getFolderInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetFolder(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderOutput{Body: rec}, nil
	}
}

func patchMyFolder(cat *Catalog) func(context.Context, *patchFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *patchFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &patchFolderInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		b := in.Body
		rec, err := cat.PatchFolder(ctx, id, folderWrite{Name: b.Name, Description: b.Description, Visibility: b.Visibility,
			IsDefault: b.IsDefault, Tags: b.Tags, CoverWorkID: b.CoverWorkID})
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderOutput{Body: rec}, nil
	}
}

func deleteMyFolder(cat *Catalog) func(context.Context, *getFolderInput) (*struct{}, error) {
	return func(ctx context.Context, in *getFolderInput) (*struct{}, error) {
		if in == nil {
			in = &getFolderInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		if err := cat.DeleteFolder(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func listMyFolderItems(cat *Catalog) func(context.Context, *listFolderItemsInput) (*listFolderItemsOutput, error) {
	return func(ctx context.Context, in *listFolderItemsInput) (*listFolderItemsOutput, error) {
		if in == nil {
			in = &listFolderItemsInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderItemSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListFolderItems(ctx, id, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFolderItemsOutput{Body: page}, nil
	}
}

func getMyFolderItem(cat *Catalog) func(context.Context, *putFolderItemInput) (*folderItemOutput, error) {
	return func(ctx context.Context, in *putFolderItemInput) (*folderItemOutput, error) {
		if in == nil {
			in = &putFolderItemInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		workID, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.GetFolderItem(ctx, id, workID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderItemOutput{Body: rec}, nil
	}
}

func putMyFolderItem(cat *Catalog) func(context.Context, *putFolderItemInput) (*folderItemOutput, error) {
	return func(ctx context.Context, in *putFolderItemInput) (*folderItemOutput, error) {
		if in == nil {
			in = &putFolderItemInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		workID, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.PutFolderItem(ctx, id, workID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderItemOutput{Body: rec}, nil
	}
}

func patchMyFolderItem(cat *Catalog) func(context.Context, *patchFolderItemInput) (*folderItemOutput, error) {
	return func(ctx context.Context, in *patchFolderItemInput) (*folderItemOutput, error) {
		if in == nil {
			in = &patchFolderItemInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		workID, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		rec, err := cat.PatchFolderItem(ctx, id, workID, in.Body.Note, in.Body.AfterWorkID, in.Body.BeforeWorkID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderItemOutput{Body: rec}, nil
	}
}

func deleteMyFolderItem(cat *Catalog) func(context.Context, *putFolderItemInput) (*struct{}, error) {
	return func(ctx context.Context, in *putFolderItemInput) (*struct{}, error) {
		if in == nil {
			in = &putFolderItemInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		workID, ok := repr.ParseID(in.WorkID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.WorkID))
		}
		if err := cat.DeleteFolderItem(ctx, id, workID); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func batchMyFolderItems(cat *Catalog) func(context.Context, *batchFolderItemsInput) (*batchFolderItemsOutput, error) {
	return func(ctx context.Context, in *batchFolderItemsInput) (*batchFolderItemsOutput, error) {
		if in == nil {
			in = &batchFolderItemsInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		ids := make([]string, 0, len(in.Body.Items))
		for _, it := range in.Body.Items {
			ids = append(ids, it.WorkID)
		}
		page, err := cat.BatchFolderItems(ctx, id, ids)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &batchFolderItemsOutput{Status: 207, Body: page}, nil
	}
}
