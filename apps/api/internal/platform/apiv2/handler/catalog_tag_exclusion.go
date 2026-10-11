package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type tagExclusionInput struct {
	ExcludeTagID   string `query:"exclude_tag_id" maxLength:"2048" doc:"Comma-separated canonical tag ids, max 100. Each excludes its own works."`
	ExcludeTagTree string `query:"exclude_tag_tree" maxLength:"2048" doc:"Comma-separated canonical tag ids, max 100. Each excludes its own works and those of every tag below it."`
}

type tagExclusionOutput struct {
	Body repr.TagExclusion
}

func registerCatalogTagExclusion(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogTagExclusion",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/tag-exclusion",
		Summary:            "Resolve a tag exclusion",
		Description:        "What exclude_tag_id= and exclude_tag_tree= exclude, taking the same two parameters as /v2/catalog/works: the expanded tag ids and the ids of every work carrying one of them. A site that lists works from its own store applies a reader's blocked tags by dropping work_ids, and one with its own tag mirror can match tag_ids instead. At least one parameter is required. The body depends only on the parameters, so cache it per parameter set. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getCatalogTagExclusion(cat))
}

func getCatalogTagExclusion(cat *Catalog) func(context.Context, *tagExclusionInput) (*tagExclusionOutput, error) {
	return func(ctx context.Context, in *tagExclusionInput) (*tagExclusionOutput, error) {
		if in == nil {
			in = &tagExclusionInput{}
		}
		rec, err := cat.TagExclusionSet(ctx, in.ExcludeTagID, in.ExcludeTagTree)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &tagExclusionOutput{Body: rec}, nil
	}
}

func (c *Catalog) TagExclusionSet(ctx context.Context, rawExact, rawTrees string) (repr.TagExclusion, error) {
	if c == nil || c.Public == nil {
		return repr.TagExclusion{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog read is not bound.")
	}
	exact, perr := idList(rawExact, "exclude_tag_id", 100)
	if perr != nil {
		return repr.TagExclusion{}, perr
	}
	trees, perr := idList(rawTrees, "exclude_tag_tree", 100)
	if perr != nil {
		return repr.TagExclusion{}, perr
	}
	if len(exact) == 0 && len(trees) == 0 {
		p := problem.New(problem.CodeInvalidParameter, "", "", "exclude_tag_id or exclude_tag_tree is required.")
		p.Errors = []problem.FieldError{{Parameter: "exclude_tag_tree", Reason: problem.ReasonRequired, Detail: "name at least one tag id in exclude_tag_tree or exclude_tag_id"}}
		return repr.TagExclusion{}, p
	}
	tags, err := c.Public.TagExclusion(ctx, exact, trees)
	if err != nil {
		return repr.TagExclusion{}, err
	}
	works, err := c.Public.ExcludedWorkIDs(ctx, tags)
	if err != nil {
		return repr.TagExclusion{}, err
	}
	out := repr.TagExclusion{Object: "tag_exclusion", TagIDs: make([]string, len(tags)), WorkIDs: make([]string, len(works))}
	for i, id := range tags {
		out.TagIDs[i] = repr.ID(id)
	}
	for i, id := range works {
		out.WorkIDs[i] = repr.ID(id)
	}
	return out, nil
}
