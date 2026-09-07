package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listPublicFoldersInput struct {
	CollectionInput
	OwnerUID string `query:"owner_uid" minLength:"1" maxLength:"20" doc:"Required. The account whose public folders to list — the central sign-in user id, the same one a folder reports as owner_uid."`
}

type getPublicFolderInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}

type listPublicFolderItemsInput struct {
	CollectionInput
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Folder id."`
}

// A folder is on this face while its owner keeps it public, and a private one
// is 404 rather than 403 — a distinct refusal would turn the id space into an
// oracle for "this person has a folder here".
func registerPublicFolders(api huma.API, cat *Catalog) {
	tags := []string{"folders"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)

	huma.Register(api, huma.Operation{
		OperationID: "listPublicFolders", Method: http.MethodGet, Path: "/v2/folders",
		Summary:     "List a user's public folders",
		Description: "The public favorite folders of one account, id-ascending. owner_uid is required: there is no platform-wide folder directory. Private folders never appear here, not even for their own owner — /v2/me/folders is that face. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listPublicFolders(cat))

	huma.Register(api, huma.Operation{
		OperationID: "getPublicFolder", Method: http.MethodGet, Path: "/v2/folders/{id}",
		Summary:     "Get one public folder",
		Description: "404 when no folder with this id is public, whether it does not exist or its owner keeps it private. owner_uid names the account it belongs to. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, getPublicFolder(cat))

	huma.Register(api, huma.Operation{
		OperationID: "listPublicFolderItems", Method: http.MethodGet, Path: "/v2/folders/{id}/items",
		Summary:     "List a public folder's items",
		Description: "Keyset-paginated by updated_at, the same shape /v2/me/folders/{id}/items answers. The list carries every work the folder holds and applies no editorial gate, so item_count matches what is returned; a caller that hides r18 applies its own gate when it hydrates the works. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listPublicFolderItems(cat))
}

func listPublicFolders(cat *Catalog) func(context.Context, *listPublicFoldersInput) (*listFoldersOutput, error) {
	return func(ctx context.Context, in *listPublicFoldersInput) (*listFoldersOutput, error) {
		if in == nil {
			in = &listPublicFoldersInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderSpec())
		if err != nil {
			return nil, err
		}
		ownerUID, err := requiredIDParam("owner_uid", in.OwnerUID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListPublicFolders(ctx, ownerUID, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFoldersOutput{Body: page}, nil
	}
}

func getPublicFolder(cat *Catalog) func(context.Context, *getPublicFolderInput) (*folderOutput, error) {
	return func(ctx context.Context, in *getPublicFolderInput) (*folderOutput, error) {
		if in == nil {
			in = &getPublicFolderInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		rec, err := cat.GetPublicFolder(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &folderOutput{Body: rec}, nil
	}
}

func listPublicFolderItems(cat *Catalog) func(context.Context, *listPublicFolderItemsInput) (*listFolderItemsOutput, error) {
	return func(ctx context.Context, in *listPublicFolderItemsInput) (*listFolderItemsOutput, error) {
		if in == nil {
			in = &listPublicFolderItemsInput{}
		}
		id, ok := repr.ParseID(in.ID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.ID))
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderItemSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListPublicFolderItems(ctx, id, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFolderItemsOutput{Body: page}, nil
	}
}
