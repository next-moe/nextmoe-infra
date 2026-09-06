package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type searchInput struct {
	CollectionInput
	Q      string `query:"q" maxLength:"512" doc:"Search string. Empty runs a popularity-ordered listing of that family."`
	Object string `query:"object" maxLength:"32" doc:"Required family: work, character, credit_name, company, tag, series, engine, trait."`
	Locale string `query:"locale" maxLength:"8" doc:"zh or ja. Ignored for works. Must not be used as a discriminant."`
}

type listSearchOutput struct {
	Body repr.List[repr.SearchHit]
}

func registerCatalogSearch(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID:        "searchCatalog",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/search",
		Summary:            "Search catalog entities",
		Description:        "Cross-entity search. object= selects the family. Hits are search_result rows with target_object. Requires an application key or a user access token with catalog:read. cursor= pages the hits. ids= is not accepted.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, searchCatalog(cat))
}

func searchCatalog(cat *Catalog) func(context.Context, *searchInput) (*listSearchOutput, error) {
	return func(ctx context.Context, in *searchInput) (*listSearchOutput, error) {
		if in == nil {
			in = &searchInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.SearchSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.Search(ctx, q, in.Object, in.Q, in.Locale)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listSearchOutput{Body: page}, nil
	}
}
