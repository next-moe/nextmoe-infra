package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listCompaniesOutput struct {
	Body repr.List[repr.Company]
}
type listTagsOutput struct {
	Body repr.List[repr.Tag]
}
type listSeriesOutput struct {
	Body repr.List[repr.Series]
}
type listEnginesOutput struct {
	Body repr.List[repr.Engine]
}
type listRolesOutput struct {
	Body repr.List[repr.Role]
}
type listReleasesOutput struct {
	Body repr.List[repr.Release]
}
type listCharactersOutput struct {
	Body repr.List[repr.Character]
}
type listCreditNamesOutput struct {
	Body repr.List[repr.CreditName]
}
type listPersonsOutput struct {
	Body repr.List[repr.Person]
}
type listTraitsOutput struct {
	Body repr.List[repr.Trait]
}

type listCreditNamesInput struct {
	CollectionInput
	Q string `query:"q" maxLength:"512" doc:"Name search. Empty lists by id. Must not be used as a discriminant."`
}

type listCompaniesInput struct {
	CollectionInput
	HasWorks string `query:"has_works" maxLength:"8" doc:"true keeps only companies whose work_count is > 0 under the same nsfw gate. Only true or false. Absent = every company."`
}

type listTagsInput struct {
	CollectionInput
	HasWorks string `query:"has_works" maxLength:"8" doc:"true keeps only tags whose work_count is > 0 under the same nsfw gate. Only true or false. Absent = every tag."`
}

func registerCatalogLists(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogCompanies",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/companies",
		Summary:            "List companies",
		Description:        "Keyset-paginated company registry (v1 labels). Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate. has_works=true keeps only companies with works visible under the same nsfw gate. include=aliases,logo fills on every lane; include=intros,links fills on the batch lane only (and on the detail face).",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogCompanies(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogTags",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/tags",
		Summary:            "List tags",
		Description:        "Keyset-paginated canonical tags. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate. has_works=true keeps only tags with works visible under the same nsfw gate.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogTags(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogSeries",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/series",
		Summary:            "List series",
		Description:        "Keyset-paginated series. Requires an application key or a user access token with catalog:read. ids= is a batch lane and does not paginate. refs= is not resolved: series has no catalog_external_ref entity_type.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogSeries(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogEngines",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/engines",
		Summary:            "List engines",
		Description:        "Keyset-paginated engines. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogEngines(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogRoles",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/roles",
		Summary:            "List roles",
		Description:        "Keyset-paginated credit-role registry. The full registry is ~231 rows, so a client building a picker can fetch it whole in three pages. key joins the role_key on credit groups; ids= is a batch lane and does not paginate. refs= is not resolved: role has no catalog_external_ref entity_type. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogRoles(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogReleases",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/releases",
		Summary:            "List releases",
		Description:        "Keyset-paginated dated releases, sorted by date_desc by default. Requires an application key or a user access token with catalog:read. ids= is a batch lane and does not paginate.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogReleases(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogCharacters",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/characters",
		Summary:            "List characters",
		Description:        "Keyset-paginated characters. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate. include=gender,birthday,height_cm,weight_kg,measurements,blood_type,instance_of_id,image,figure,traits,aliases,intros,refs fills on every lane, and view=full is all of them; traits are cut at the default spoiler ceiling and follow the nsfw gate, exactly as on the detail face.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogCharacters(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogCreditNames",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/credit-names",
		Summary:            "List credit names",
		Description:        "Keyset-paginated credited names. q= filters by name. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogCreditNames(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogPersons",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/persons",
		Summary:            "List persons",
		Description:        "Keyset-paginated persons. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogPersons(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogTraits",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/traits",
		Summary:            "List traits",
		Description:        "Keyset-paginated character traits. Requires an application key or a user access token with catalog:read. ids= is a batch lane. refs= is not resolved: traits have no catalog_external_ref entity_type.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogTraits(cat))
}

func listCatalogCompanies(cat *Catalog) func(context.Context, *listCompaniesInput) (*listCompaniesOutput, error) {
	return func(ctx context.Context, in *listCompaniesInput) (*listCompaniesOutput, error) {
		if in == nil {
			in = &listCompaniesInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.CompanySpec())
		if err != nil {
			return nil, err
		}
		hasWorks := false
		if in.HasWorks != "" {
			v, berr := parse.Bool(in.HasWorks, "has_works")
			if berr != nil {
				return nil, withIdent(ctx, berr)
			}
			hasWorks = v
		}
		page, lerr := cat.ListCompanies(ctx, q, hasWorks)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCompaniesOutput{Body: page}, nil
	}
}

func listCatalogTags(cat *Catalog) func(context.Context, *listTagsInput) (*listTagsOutput, error) {
	return func(ctx context.Context, in *listTagsInput) (*listTagsOutput, error) {
		if in == nil {
			in = &listTagsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.TagSpec())
		if err != nil {
			return nil, err
		}
		hasWorks := false
		if in.HasWorks != "" {
			v, berr := parse.Bool(in.HasWorks, "has_works")
			if berr != nil {
				return nil, withIdent(ctx, berr)
			}
			hasWorks = v
		}
		page, lerr := cat.ListTags(ctx, q, hasWorks)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listTagsOutput{Body: page}, nil
	}
}

func listCatalogSeries(cat *Catalog) func(context.Context, *CollectionInput) (*listSeriesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listSeriesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.SeriesSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListSeries(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listSeriesOutput{Body: page}, nil
	}
}

func listCatalogEngines(cat *Catalog) func(context.Context, *CollectionInput) (*listEnginesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listEnginesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.EngineSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListEngines(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listEnginesOutput{Body: page}, nil
	}
}

func listCatalogRoles(cat *Catalog) func(context.Context, *CollectionInput) (*listRolesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listRolesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.RoleSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListRoles(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listRolesOutput{Body: page}, nil
	}
}

func listCatalogReleases(cat *Catalog) func(context.Context, *CollectionInput) (*listReleasesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listReleasesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.ReleaseSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListReleases(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listReleasesOutput{Body: page}, nil
	}
}

func listCatalogCharacters(cat *Catalog) func(context.Context, *CollectionInput) (*listCharactersOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listCharactersOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.CharacterSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListCharacters(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCharactersOutput{Body: page}, nil
	}
}

func listCatalogCreditNames(cat *Catalog) func(context.Context, *listCreditNamesInput) (*listCreditNamesOutput, error) {
	return func(ctx context.Context, in *listCreditNamesInput) (*listCreditNamesOutput, error) {
		if in == nil {
			in = &listCreditNamesInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.CreditNameSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListCreditNames(ctx, q, in.Q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCreditNamesOutput{Body: page}, nil
	}
}

func listCatalogPersons(cat *Catalog) func(context.Context, *CollectionInput) (*listPersonsOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listPersonsOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.PersonSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListPersons(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listPersonsOutput{Body: page}, nil
	}
}

func listCatalogTraits(cat *Catalog) func(context.Context, *CollectionInput) (*listTraitsOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listTraitsOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.TraitSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListTraits(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listTraitsOutput{Body: page}, nil
	}
}

func parseCatalogList(ctx context.Context, in *CollectionInput, spec collect.Spec) (collect.Query, error) {
	q, err := collect.Parse(rawFrom(in), spec)
	if err != nil {
		return collect.Query{}, withIdent(ctx, err)
	}
	return q, nil
}
