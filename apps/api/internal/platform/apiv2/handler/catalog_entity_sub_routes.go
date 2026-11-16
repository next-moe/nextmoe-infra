package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/repr"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type getPersonOutput struct {
	Body repr.Person
}
type getTraitOutput struct {
	Body repr.Trait
}
type listNameCreditsOutput struct {
	Body repr.List[repr.NameCredit]
}
type listPersonNamesOutput struct {
	Body repr.List[repr.CreditName]
}
type listAppearancesOutput struct {
	Body repr.List[repr.Appearance]
}

type entityWorksInput struct {
	WorkSubInput
	ContentLimit string `query:"content_limit" maxLength:"32" example:"sfw" doc:"Comma-separated closed editorial axis: sfw, nsfw, applied to each work exactly as on /v2/catalog/works. Absent applies no gate. It gates items, the offset cursor and total alike, so a page is never short of works the reader may see."`
	IncludeTotal string `query:"include_total" maxLength:"8" doc:"true adds total: the distinct works every page of this list returns together, under this request's nsfw and content_limit. Only true or false."`
}

func parseEntityWorks(ctx context.Context, in *entityWorksInput) (int64, catsvc.EntityWorksGate, string, int, error) {
	if in == nil {
		in = &entityWorksInput{}
	}
	id, nsfw, cur, limit, err := parseWorkSub(ctx, &in.WorkSubInput)
	if err != nil {
		return 0, catsvc.EntityWorksGate{}, "", 0, err
	}
	limits, perr := closedCSV(in.ContentLimit, "content_limit", catsvc.DisplayLimitTokens)
	if perr != nil {
		return 0, catsvc.EntityWorksGate{}, "", 0, withIdent(ctx, perr)
	}
	gate := catsvc.EntityWorksGate{NSFW: nsfw, DisplayLimits: limits}
	if in.IncludeTotal != "" {
		if gate.WithTotal, perr = parse.Bool(in.IncludeTotal, "include_total"); perr != nil {
			return 0, catsvc.EntityWorksGate{}, "", 0, withIdent(ctx, perr)
		}
	}
	return id, gate, cur, limit, nil
}

func registerCatalogEntityExtras(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	authErrs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCreditNameCredits",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/credit-names/{id}/credits",
		Summary:            "Credits of one credit name",
		Description:        "Works this name is credited on. Offset cursor. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCreditNameCredits(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCharacterAppearances",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/characters/{id}/appearances",
		Summary:            "Appearances of one character",
		Description:        "Works this character appears in, with roster_role, spoiler, and voice credits. Offset cursor. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCharacterAppearances(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogPersonCreditNames",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/persons/{id}/credit-names",
		Summary:            "Credit names of one person",
		Description:        "Every credited name linked to this person. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogPersonCreditNames(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogPerson",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/persons/{id}",
		Summary:            "Get one person",
		Description:        "A person identity that groups credit names. Merged ids are 404 ENTITY_MERGED. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogPerson(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogTrait",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/traits/{id}",
		Summary:            "Get one trait",
		Description:        "A character-trait vocabulary row. Without nsfw=true a sexual-family trait is 404 NOT_FOUND. include=aliases,description,intros (and view=full) add those blocks. include=character_count is the nightly index total that GET /v2/catalog/characters?trait_id=<this id>&page=1 answers under this request's nsfw; the engine failing is 503. It is an explicit ask: view=full does not add it. is_sexual reports the sexual-family flag. Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogTrait(cat))
}

func getCatalogCreditNameCredits(cat *Catalog) func(context.Context, *entityWorksInput) (*listNameCreditsOutput, error) {
	return func(ctx context.Context, in *entityWorksInput) (*listNameCreditsOutput, error) {
		id, gate, cur, limit, err := parseEntityWorks(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.CreditNameCredits(ctx, id, gate, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listNameCreditsOutput{Body: page}, nil
	}
}

func getCatalogCharacterAppearances(cat *Catalog) func(context.Context, *entityWorksInput) (*listAppearancesOutput, error) {
	return func(ctx context.Context, in *entityWorksInput) (*listAppearancesOutput, error) {
		id, gate, cur, limit, err := parseEntityWorks(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.CharacterAppearances(ctx, id, gate, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listAppearancesOutput{Body: page}, nil
	}
}

func getCatalogPersonCreditNames(cat *Catalog) func(context.Context, *ResourceIDInput) (*listPersonNamesOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*listPersonNamesOutput, error) {
		id, _, err := parseResource(ctx, in, collect.CreditNameSpec())
		if err != nil {
			return nil, err
		}
		page, gerr := cat.PersonCreditNames(ctx, id)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listPersonNamesOutput{Body: page}, nil
	}
}

func getCatalogPerson(cat *Catalog) func(context.Context, *ResourceIDInput) (*getPersonOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getPersonOutput, error) {
		id, _, err := parseResource(ctx, in, collect.PersonSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetPerson(ctx, id)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getPersonOutput{Body: rec}, nil
	}
}

func getCatalogTrait(cat *Catalog) func(context.Context, *ResourceIDInput) (*getTraitOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getTraitOutput, error) {
		id, q, err := parseResource(ctx, in, collect.TraitSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetTrait(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getTraitOutput{Body: rec}, nil
	}
}
