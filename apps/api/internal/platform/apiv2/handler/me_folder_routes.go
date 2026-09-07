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
		Name        string `json:"name" minLength:"1" maxLength:"100" doc:"Display name. Must not be used as a discriminant."`
		Description string `json:"description,omitempty" maxLength:"500" doc:"Owner's own note. Must not be used as a discriminant."`
		Visibility  string `json:"visibility,omitempty" enum:"private,public" doc:"Defaults to private."`
		IsDefault   bool   `json:"is_default,omitempty" doc:"true makes this the user's default folder and clears the flag on the previous holder."`
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
		Name        *string `json:"name,omitempty" minLength:"1" maxLength:"100" doc:"New display name. Must not be used as a discriminant."`
		Description *string `json:"description,omitempty" maxLength:"500" doc:"New note; an empty string clears it. Must not be used as a discriminant."`
		Visibility  *string `json:"visibility,omitempty" enum:"private,public" doc:"New visibility."`
		IsDefault   *bool   `json:"is_default,omitempty" doc:"Only true is accepted: it moves the default flag to this folder. Sending false is 422 — set the flag on another folder instead."`
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

// Named, not anonymous: huma names an anonymous `Items []struct{...}` element
// "Item" after the field, and two batch routes with that shape panic the
// registry with "duplicate name: Item" at startup.
type folderBatchEntry struct {
	WorkID string `json:"work_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog work id."`
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
		Summary: "Create a folder", Description: "Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusCreated, SkipValidateParams: true,
	}, createMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyFolder", Method: http.MethodGet, Path: "/v2/me/folders/{id}",
		Summary: "Get one folder", Description: "404 when the bearer owns no folder with this id. Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, getMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "patchMyFolder", Method: http.MethodPatch, Path: "/v2/me/folders/{id}",
		Summary: "Update a folder", Description: "Partial update: name, description, visibility, is_default. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, patchMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyFolder", Method: http.MethodDelete, Path: "/v2/me/folders/{id}",
		Summary: "Delete a folder", Description: "Deletes the folder and its items. The default folder is refused with 422; move the flag first. 204 with no body. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyFolder(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyFolderItems", Method: http.MethodGet, Path: "/v2/me/folders/{id}/items",
		Summary: "List a folder's items", Description: "Keyset-paginated by updated_at; the final next_cursor can be stored and replayed later as an incremental-sync watermark (deletions are not replayed). Requires a user access token with folder:read (folder:write also grants reads).",
		Tags: me, Errors: errs, SkipValidateParams: true,
	}, listMyFolderItems(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyFolderItem", Method: http.MethodPut, Path: "/v2/me/folders/{id}/items/{work_id}",
		Summary: "Add a work to a folder", Description: "Naturally idempotent: re-adding an existing membership answers the stored row and touches nothing. Requires a user access token with folder:write.",
		Tags: me, Errors: writeErrs, SkipValidateParams: true,
	}, putMyFolderItem(cat))
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
		rec, err := cat.CreateFolder(ctx, in.Body.Name, in.Body.Description, in.Body.Visibility, in.Body.IsDefault)
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
		rec, err := cat.PatchFolder(ctx, id, in.Body.Name, in.Body.Description, in.Body.Visibility, in.Body.IsDefault)
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
