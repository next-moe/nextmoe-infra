package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

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

func registerCatalogEntityExtras(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	authErrs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCreditNameCredits",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/credit-names/{id}/credits",
		Summary:            "Credits of one credit name",
		Description:        "Works this name is credited on. Offset cursor. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCreditNameCredits(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCharacterAppearances",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/characters/{id}/appearances",
		Summary:            "Appearances of one character",
		Description:        "Works this character appears in, with roster_role, spoiler, and voice credits. Offset cursor. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCharacterAppearances(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogPersonCreditNames",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/persons/{id}/credit-names",
		Summary:            "Credit names of one person",
		Description:        "Every credited name linked to this person. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogPersonCreditNames(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogPerson",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/persons/{id}",
		Summary:            "Get one person",
		Description:        "A person identity that groups credit names. Merged ids are 404 ENTITY_MERGED. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogPerson(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogTrait",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/traits/{id}",
		Summary:            "Get one trait",
		Description:        "A character-trait vocabulary row. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogTrait(cat))
}

func getCatalogCreditNameCredits(cat *Catalog) func(context.Context, *WorkSubInput) (*listNameCreditsOutput, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listNameCreditsOutput, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.CreditNameCredits(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listNameCreditsOutput{Body: page}, nil
	}
}

func getCatalogCharacterAppearances(cat *Catalog) func(context.Context, *WorkSubInput) (*listAppearancesOutput, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listAppearancesOutput, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.CharacterAppearances(ctx, id, nsfw, cur, limit)
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
		id, _, err := parseResource(ctx, in, collect.TraitSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetTrait(ctx, id)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getTraitOutput{Body: rec}, nil
	}
}
