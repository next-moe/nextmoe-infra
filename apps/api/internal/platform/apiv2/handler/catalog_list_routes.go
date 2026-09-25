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

type listCharactersInput struct {
	CollectionInput
	PageInput
	Q          string `query:"q" maxLength:"512" doc:"Character name search. Switches this collection to the search index; sort defaults to relevance. Must not be used as a discriminant."`
	TraitID    string `query:"trait_id" maxLength:"256" doc:"Comma-separated catalog trait ids, max 10. Descendants included. Matches under the same spoiler and nsfw gates as the traits block. Naming a sexual trait without nsfw=true is 400. Unknown ids match nothing."`
	TraitMatch string `query:"trait_match" maxLength:"8" doc:"Closed: all (default), any. No effect without trait_id."`
	Gender     string `query:"gender" maxLength:"32" doc:"Comma-separated closed vocabulary: male, female, other. OR within the parameter. Unknown token is 400."`
}

type listTraitsInput struct {
	CollectionInput
	ParentID string `query:"parent_id" maxLength:"20" doc:"Catalog trait id. Direct children of this trait only. Naming a sexual-family trait without nsfw=true is 400."`
	GroupID  string `query:"group_id" maxLength:"20" doc:"Catalog trait id of a root group. Traits in that group, excluding the root itself. Naming a sexual-family trait without nsfw=true is 400."`
	Root     string `query:"root" maxLength:"8" doc:"true: only root traits. false: only non-root traits. Only true or false."`
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
		Description:        "Characters from one of two lanes. The index lane is used when q= is non-empty, page= is present, or sort is popularity, relevance or newest; otherwise the registry lane (sort=id, live SQL). The index lane reflects the nightly search index; the registry lane is live. q= with no sort defaults to relevance. sort=relevance requires q=. Popularity is log1p of the character's live roster, with main appearances at full work popularity and every other kind at half; under nsfw not true only works without an r18 rating count. Requires an application key or a user access token with catalog:read. ids=/refs= is a batch lane and does not paginate; it cannot be combined with q= or a search sort. include=gender,birthday,height_cm,weight_kg,measurements,blood_type,instance_of_id,image,figure,traits,aliases,intros,refs,work_count fills on every lane, and view=full is all of them; traits are cut at the default spoiler ceiling and follow the nsfw gate, exactly as on the detail face. trait_id= filters by trait (descendants included, max 10), combined by trait_match=all|any; a character matches under the same spoiler and nsfw gates as its traits block. When trait_id= is given, each item carries matched_trait_ids: this character's own traits that satisfied the filter. gender= filters by the closed vocabulary male,female,other. include=work_count is the number of distinct works the character appears in under the same nsfw gate as /v2/catalog/characters/{id}/appearances. page= selects page mode on the index lane.",
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
		Description:        "Keyset-paginated character traits. Requires an application key or a user access token with catalog:read. ids= is a batch lane. refs= is not resolved: traits have no catalog_external_ref entity_type. parent_id= lists direct children; group_id= lists traits in that root group (the root excluded); root=true|false keeps only roots or only non-roots. Filters are conjunctive. Without nsfw=true, sexual-family traits are excluded from the list and land in missing[] on the ids= batch; naming one as parent_id or group_id is 400. include=aliases,description,intros (and view=full) add those blocks. include=character_count is the nightly index total that GET /v2/catalog/characters?trait_id=<this id>&page=1 answers under this request's nsfw; the engine failing is 503. It is an explicit ask: view=full does not add it. is_sexual reports the sexual-family flag.",
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

func listCatalogCharacters(cat *Catalog) func(context.Context, *listCharactersInput) (*listCharactersOutput, error) {
	return func(ctx context.Context, in *listCharactersInput) (*listCharactersOutput, error) {
		if in == nil {
			in = &listCharactersInput{}
		}
		raw := rawFrom(&in.CollectionInput)
		raw.Page = in.Page
		q, err := collect.Parse(raw, collect.CharacterSpec())
		if err != nil {
			return nil, withIdent(ctx, err)
		}
		f, ferr := parseCharacterFilter(in)
		if ferr != nil {
			return nil, withIdent(ctx, ferr)
		}
		page, lerr := cat.ListCharacters(ctx, q, f)
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

func listCatalogTraits(cat *Catalog) func(context.Context, *listTraitsInput) (*listTraitsOutput, error) {
	return func(ctx context.Context, in *listTraitsInput) (*listTraitsOutput, error) {
		if in == nil {
			in = &listTraitsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.TraitSpec())
		if err != nil {
			return nil, err
		}
		f, ferr := parseTraitFilter(in)
		if ferr != nil {
			return nil, withIdent(ctx, ferr)
		}
		page, lerr := cat.ListTraits(ctx, q, f)
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
