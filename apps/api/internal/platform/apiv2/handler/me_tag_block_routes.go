package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listTagBlocksInput struct {
	CollectionInput
	TagIDs string `query:"tag_ids" maxLength:"4096" doc:"Comma-separated tag ids, max 100. Batch read, no pagination."`
}
type listTagBlocksOutput struct {
	Body repr.List[repr.TagBlock]
}
type tagBlockPathInput struct {
	TagID string `path:"tag_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog tag id."`
}
type putTagBlockInput struct {
	TagID string `path:"tag_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog tag id."`
	Body  *struct {
		Mode string `json:"mode,omitempty" enum:"tree,exact" doc:"tree (the default) also blocks every tag below this one in the tag hierarchy; exact blocks this tag only."`
	}
}
type tagBlockOutput struct {
	Body repr.TagBlock
}

func registerMeTagBlocks(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listMyTagBlocks", Method: http.MethodGet, Path: "/v2/me/blocked-tags",
		Summary:     "List my blocked tags",
		Description: "The bearer user's blocked tags, newest first. Items carry ids only and are hydrated with /v2/catalog/tags?ids=. tag_ids= is a batch read. The list is private to its owner and the catalog applies none of it by itself: a client sends the tree ids in exclude_tag_tree and the exact ids in exclude_tag_id on /v2/catalog/works and the two calendars. At most 100 entries, so the whole list always fits those parameters. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyTagBlocks(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyTagBlock", Method: http.MethodGet, Path: "/v2/me/blocked-tags/{tag_id}",
		Summary:     "Get my block on one tag",
		Description: "404 when the bearer has not blocked this tag. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyTagBlock(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyTagBlock", Method: http.MethodPut, Path: "/v2/me/blocked-tags/{tag_id}",
		Summary:     "Block a tag",
		Description: "Optional body {mode}: tree (the default) or exact. Sets the mode of an existing block and otherwise writes nothing for it. Unknown tags are 404. At most 100 blocked tags. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, putMyTagBlock(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyTagBlock", Method: http.MethodDelete, Path: "/v2/me/blocked-tags/{tag_id}",
		Summary:     "Unblock a tag",
		Description: "204 with no body, also when the tag was not blocked. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyTagBlock(cat))
}

func listMyTagBlocks(cat *Catalog) func(context.Context, *listTagBlocksInput) (*listTagBlocksOutput, error) {
	return func(ctx context.Context, in *listTagBlocksInput) (*listTagBlocksOutput, error) {
		if in == nil {
			in = &listTagBlocksInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.TagBlockSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListTagBlocks(ctx, q, splitWorkIDs(in.TagIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listTagBlocksOutput{Body: page}, nil
	}
}

func getMyTagBlock(cat *Catalog) func(context.Context, *tagBlockPathInput) (*tagBlockOutput, error) {
	return func(ctx context.Context, in *tagBlockPathInput) (*tagBlockOutput, error) {
		id, ok := repr.ParseID(in.TagID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.TagID))
		}
		rec, err := cat.GetTagBlock(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &tagBlockOutput{Body: rec}, nil
	}
}

func putMyTagBlock(cat *Catalog) func(context.Context, *putTagBlockInput) (*tagBlockOutput, error) {
	return func(ctx context.Context, in *putTagBlockInput) (*tagBlockOutput, error) {
		id, ok := repr.ParseID(in.TagID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.TagID))
		}
		mode := ""
		if in.Body != nil {
			mode = in.Body.Mode
		}
		rec, err := cat.PutTagBlock(ctx, id, mode)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &tagBlockOutput{Body: rec}, nil
	}
}

func deleteMyTagBlock(cat *Catalog) func(context.Context, *tagBlockPathInput) (*struct{}, error) {
	return func(ctx context.Context, in *tagBlockPathInput) (*struct{}, error) {
		id, ok := repr.ParseID(in.TagID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.TagID))
		}
		if err := cat.DeleteTagBlock(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}
