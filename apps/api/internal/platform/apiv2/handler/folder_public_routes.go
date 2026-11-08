package handler

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/problem"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/model"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type listPublicFoldersInput struct {
	CollectionInput
	OwnerUID       string `query:"owner_uid" maxLength:"20" doc:"The account whose public folders to list — the central sign-in user id, the same one a folder reports as owner_uid. Absent: the folder directory instead."`
	ContainsWorkID string `query:"contains_work_id" maxLength:"20" doc:"Keep only the folders holding this catalog work: on the directory lane, the curated lists a work page shows."`
	Tag            string `query:"tag" maxLength:"16" doc:"Keep only the folders carrying this tag, matched exactly as GET /v2/folders/tags spells it. Must not be used as a discriminant."`
	Q              string `query:"q" maxLength:"50" doc:"Keep only the folders whose name, description or a tag contains this text, ignoring case. A plain substring, not a pattern; the sort stays the one asked for. Must not be used as a discriminant."`
}

type listFolderTagsInput struct {
	CollectionInput
}

type listFolderTagsOutput struct {
	Body repr.List[repr.FolderTagCount]
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
		Summary:     "List public folders",
		Description: "Two lanes. With owner_uid: that account's public folders, every one it keeps public, default sort id. Without owner_uid: the folder directory, the public folders that read as a curated list — not the owner's default folder, a non-blank name, and at least " + strconv.Itoa(model.FolderDirectoryMinItems) + " works — default sort followers_desc. sort: id (ascending), followers_desc, updated_desc (the folder's text or items changed), created_desc. contains_work_id narrows either lane to folders holding that work, tag to folders carrying that tag, and q to folders whose name, description or a tag contains the text. nsfw applies to the directory lane only: false or absent leaves out every folder whose content_rating is r18; the owner lane always lists everything the owner published, each folder carrying its content_rating. Private and unlisted folders never appear here, not even for their own owner — /v2/me/folders is that face; an unlisted folder opens only by id. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listPublicFolders(cat))

	huma.Register(api, huma.Operation{
		OperationID: "listFolderTags", Method: http.MethodGet, Path: "/v2/folders/tags",
		Summary:     "List the directory's tags",
		Description: "The tags directory folders carry, most used first, for a tag picker or tag cloud. Counts only folders the directory lists (see listPublicFolders), under the same nsfw rule. nsfw counts r18 folders too. One page of up to limit tags: there is no next_cursor. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listFolderTags(cat))

	huma.Register(api, huma.Operation{
		OperationID: "getPublicFolder", Method: http.MethodGet, Path: "/v2/folders/{id}",
		Summary:     "Get one public folder",
		Description: "Answers public and unlisted folders alike: unlisted only keeps a folder out of the listings. 404 when the folder does not exist or its owner keeps it private. owner_uid names the account it belongs to. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, getPublicFolder(cat))

	huma.Register(api, huma.Operation{
		OperationID: "listPublicFolderItems", Method: http.MethodGet, Path: "/v2/folders/{id}/items",
		Summary:     "List a public folder's items",
		Description: "The same shape and the same two sorts /v2/me/folders/{id}/items answers: updated (default) for incremental sync, position for the owner's display order. The list carries every work the folder holds and applies no editorial gate, so item_count matches what is returned; a caller that hides r18 applies its own gate when it hydrates the works. Requires an application key or a user access token with catalog:read.",
		Tags:        tags, Errors: errs, SkipValidateParams: true,
	}, listPublicFolderItems(cat))
}

func listPublicFolders(cat *Catalog) func(context.Context, *listPublicFoldersInput) (*listFoldersOutput, error) {
	return func(ctx context.Context, in *listPublicFoldersInput) (*listFoldersOutput, error) {
		if in == nil {
			in = &listPublicFoldersInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.PublicFolderSpec())
		if err != nil {
			return nil, err
		}
		ownerUID, err := optionalIDParam("owner_uid", in.OwnerUID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		containsWorkID, err := optionalIDParam("contains_work_id", in.ContainsWorkID)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		sort := folderSortOf(q.Sort)
		if ownerUID == 0 && in.Sort == "" {
			sort = catsvc.FolderSortFollowers
		}
		tag, err := folderTagParam(in.Tag)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		page, lerr := cat.ListPublicFolders(ctx, catsvc.PublicFolderQuery{
			OwnerUID: ownerUID, ContainsWorkID: containsWorkID, Tag: tag, Search: strings.TrimSpace(in.Q),
			NSFW: q.NSFW, Sort: sort,
		}, q)
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

func folderTagParam(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	tags, err := catsvc.NormalizeFolderTags([]string{raw})
	if err != nil || len(tags) == 0 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "tag must be 1 to 16 characters.")
		p.Errors = []problem.FieldError{{Parameter: "tag", Reason: problem.ReasonInvalidFormat, Detail: raw}}
		return "", p
	}
	return tags[0], nil
}

func listFolderTags(cat *Catalog) func(context.Context, *listFolderTagsInput) (*listFolderTagsOutput, error) {
	return func(ctx context.Context, in *listFolderTagsInput) (*listFolderTagsOutput, error) {
		if in == nil {
			in = &listFolderTagsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.FolderTagSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListFolderTags(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listFolderTagsOutput{Body: page}, nil
	}
}
