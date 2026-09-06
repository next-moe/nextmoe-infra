package handler

import (
	"context"
	"errors"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/apiv2/vocab"

	"github.com/danielgtaylor/huma/v2"
)

// Exported only so it can be embedded: huma walks input structs with
// reflect and skips every field where IsExported() is false, and an embedded
// field takes its name from the type. With `resourceIDInput` embedded, id /
// nsfw / view / include / fields vanished from BOTH the generated spec and
// request binding, silently — the route still answered 200 with an empty id.
type ResourceIDInput struct {
	ID      string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Decimal catalog id."`
	NSFW    string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false."`
	View    string `query:"view" maxLength:"16" doc:"basic (default) or full. Closed vocabulary."`
	Include string `query:"include" maxLength:"1024" doc:"Comma-separated blocks. Unknown token is 400 UNKNOWN_INCLUDE."`
	Fields  string `query:"fields" maxLength:"1024" doc:"Comma-separated top-level keys. Unknown token is 400 UNKNOWN_FIELD. object and id are always kept."`
}

type workDetailInput struct {
	ResourceIDInput
	Spoiler string `query:"spoiler" maxLength:"16" doc:"Spoiler ceiling for the tags block: none (default), minor or major. Closed vocabulary; an unknown value is 400. Tag rows above the ceiling are not returned. Only the VNDB-derived tag vocabulary carries a spoiler level — Bangumi and DLsite folksonomy publish no spoiler concept, so those rows read none — and the default is the safe ceiling."`
}

type characterDetailInput struct {
	ResourceIDInput
	Spoiler string `query:"spoiler" maxLength:"16" doc:"Spoiler ceiling for the traits block: none (default), minor or major. Closed vocabulary; an unknown value is 400. Trait rows above the ceiling are not returned. Only the VNDB-derived trait vocabulary carries a spoiler level, and the default is the safe ceiling."`
}

type getWorkOutput struct {
	Body repr.Work
}
type getCompanyOutput struct {
	Body repr.Company
}
type getCreditNameOutput struct {
	Body repr.CreditName
}
type getCharacterOutput struct {
	Body repr.Character
}
type getTagOutput struct {
	Body repr.Tag
}
type getSeriesOutput struct {
	Body repr.Series
}
type getEngineOutput struct {
	Body repr.Engine
}
type getRoleOutput struct {
	Body repr.Role
}
type getReleaseOutput struct {
	Body repr.Release
}
type getCompanyGraphOutput struct {
	Body repr.CompanyGraph
}
type getStatsOutput struct {
	Body repr.CatalogStats
}

func registerCatalog(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	authErrs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	statsErrs := collectionErrors(http.StatusServiceUnavailable)

	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogWork",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/works/{id}",
		Summary:            "Get one catalog work",
		Description:        "Work detail. spoiler=none|minor|major is the ceiling of the tags block and defaults to none. Merged ids are 404 ENTITY_MERGED with Link rel=canonical. r18 is 404 without nsfw=true. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogWork(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCompanyGraph",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/companies/{id}/graph",
		Summary:            "Company family graph",
		Description:        "Corporate-family nodes and directed edges around one company. Inverse relations are not emitted. include= is validated against the company token set, so aliases, intros and links are accepted and answered without; only include=logo changes a node, adding the brand mark to the nodes that have one. Merged ids are 404 ENTITY_MERGED. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCompanyGraph(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCompany",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/companies/{id}",
		Summary:            "Get one company",
		Description:        "Company registry row (v1 labels). include=aliases,logo,intros,links adds the corresponding blocks. Merged ids are 404 ENTITY_MERGED. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCompany(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCreditName",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/credit-names/{id}",
		Summary:            "Get one credit name",
		Description:        "A credited name, not a person — this is the staff and voice-actor read surface. person_id is null when unlinked; gender and the fuzzy birth parts are person-level facts reached through that link. include=aliases,photo,siblings,intros,links,refs adds the corresponding blocks. Works this name is credited on live at /v2/catalog/credit-names/{id}/credits. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCreditName(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogCharacter",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/characters/{id}",
		Summary:            "Get one character",
		Description:        "Character detail. view=full adds gender, birthday, measurements, blood_type, instance_of_id. include=image,figure,traits,aliases,intros,refs adds art, trait, name, description and anchor blocks. spoiler=none|minor|major is the ceiling of the traits block and defaults to none. Merged ids are 404 ENTITY_MERGED. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogCharacter(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogTag",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/tags/{id}",
		Summary:            "Get one tag",
		Description:        "Canonical tag. include=intros adds the per-language tag descriptions. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogTag(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogSeries",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/series/{id}",
		Summary:            "Get one series",
		Description:        "Series detail. has_nsfw reports whether any member work sits behind the r18 display gate. include=intros,refs adds the corresponding blocks. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogSeries(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogEngine",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/engines/{id}",
		Summary:            "Get one engine",
		Description:        "Engine detail. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogEngine(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogRole",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/roles/{id}",
		Summary:            "Get one role",
		Description:        "A credit-role registry row. Unknown id is 404 NOT_FOUND. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogRole(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogRelease",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/releases/{id}",
		Summary:            "Get one release",
		Description:        "A catalog release. Merged ids are 404 ENTITY_MERGED. r18 parent works are 404 without nsfw=true. Requires an application key or a user access token with catalog:read.",
		Tags:               catalog,
		Errors:             authErrs,
		SkipValidateParams: true,
	}, getCatalogRelease(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getCatalogStats",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/stats",
		Summary:            "Catalog totals",
		Description:        "Live entity counts. Unauthenticated.",
		Tags:               catalog,
		Errors:             statsErrs,
		SkipValidateParams: true,
	}, getCatalogStats(cat))
	registerCatalogLists(api, cat)
	registerCatalogFeeds(api, cat)
	registerCatalogEditHistory(api, cat)
	registerCatalogClaimEvents(api, cat)
	registerCatalogSearch(api, cat)
	registerCatalogSchemas(api, cat)
	registerCatalogEntityExtras(api, cat)
	registerWorkSubs(api, cat)
	registerNews(api, cat)
}

func getCatalogWork(cat *Catalog) func(context.Context, *workDetailInput) (*getWorkOutput, error) {
	return func(ctx context.Context, in *workDetailInput) (*getWorkOutput, error) {
		if in == nil {
			in = &workDetailInput{}
		}
		id, q, err := parseResource(ctx, &in.ResourceIDInput, collect.WorkSpec())
		if err != nil {
			return nil, err
		}
		spoiler, serr := parseSpoiler(ctx, in.Spoiler)
		if serr != nil {
			return nil, serr
		}
		rec, gerr := cat.GetWork(ctx, id, q.NSFW, spoiler, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getWorkOutput{Body: rec}, nil
	}
}

func getCatalogCompanyGraph(cat *Catalog) func(context.Context, *ResourceIDInput) (*getCompanyGraphOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getCompanyGraphOutput, error) {
		id, q, err := parseResource(ctx, in, collect.CompanySpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetCompanyGraph(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getCompanyGraphOutput{Body: rec}, nil
	}
}

func getCatalogCompany(cat *Catalog) func(context.Context, *ResourceIDInput) (*getCompanyOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getCompanyOutput, error) {
		id, q, err := parseResource(ctx, in, collect.CompanySpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetCompany(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getCompanyOutput{Body: rec}, nil
	}
}

func getCatalogCreditName(cat *Catalog) func(context.Context, *ResourceIDInput) (*getCreditNameOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getCreditNameOutput, error) {
		id, q, err := parseResource(ctx, in, collect.CreditNameSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetCreditName(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getCreditNameOutput{Body: rec}, nil
	}
}

func getCatalogCharacter(cat *Catalog) func(context.Context, *characterDetailInput) (*getCharacterOutput, error) {
	return func(ctx context.Context, in *characterDetailInput) (*getCharacterOutput, error) {
		if in == nil {
			in = &characterDetailInput{}
		}
		id, q, err := parseResource(ctx, &in.ResourceIDInput, collect.CharacterSpec())
		if err != nil {
			return nil, err
		}
		spoiler, serr := parseSpoiler(ctx, in.Spoiler)
		if serr != nil {
			return nil, serr
		}
		rec, gerr := cat.GetCharacter(ctx, id, q.NSFW, spoiler, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getCharacterOutput{Body: rec}, nil
	}
}

func getCatalogTag(cat *Catalog) func(context.Context, *ResourceIDInput) (*getTagOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getTagOutput, error) {
		id, q, err := parseResource(ctx, in, collect.TagSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetTag(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getTagOutput{Body: rec}, nil
	}
}

func getCatalogSeries(cat *Catalog) func(context.Context, *ResourceIDInput) (*getSeriesOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getSeriesOutput, error) {
		id, q, err := parseResource(ctx, in, collect.SeriesSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetSeries(ctx, id, q.NSFW, q.Include)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getSeriesOutput{Body: rec}, nil
	}
}

func getCatalogEngine(cat *Catalog) func(context.Context, *ResourceIDInput) (*getEngineOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getEngineOutput, error) {
		id, q, err := parseResource(ctx, in, collect.EngineSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetEngine(ctx, id, q.NSFW)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getEngineOutput{Body: rec}, nil
	}
}

func getCatalogRole(cat *Catalog) func(context.Context, *ResourceIDInput) (*getRoleOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getRoleOutput, error) {
		id, _, err := parseResource(ctx, in, collect.RoleSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetRole(ctx, id)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getRoleOutput{Body: rec}, nil
	}
}

func getCatalogRelease(cat *Catalog) func(context.Context, *ResourceIDInput) (*getReleaseOutput, error) {
	return func(ctx context.Context, in *ResourceIDInput) (*getReleaseOutput, error) {
		id, q, err := parseResource(ctx, in, collect.ReleaseSpec())
		if err != nil {
			return nil, err
		}
		rec, gerr := cat.GetRelease(ctx, id, q.NSFW)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &getReleaseOutput{Body: rec}, nil
	}
}

func getCatalogStats(cat *Catalog) func(context.Context, *struct{}) (*getStatsOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*getStatsOutput, error) {
		rec, err := cat.Stats(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getStatsOutput{Body: rec}, nil
	}
}

func parseResource(ctx context.Context, in *ResourceIDInput, spec collect.Spec) (int64, collect.Query, error) {
	if in == nil {
		in = &ResourceIDInput{}
	}
	id, ok := repr.ParseID(in.ID)
	if !ok {
		p := problem.New(problem.CodeInvalidParameter, "", "", "id must be a positive decimal catalog id.")
		p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: in.ID}}
		return 0, collect.Query{}, withIdent(ctx, p)
	}
	q, err := collect.Parse(collect.Raw{View: in.View, Include: in.Include, Fields: in.Fields, NSFW: in.NSFW}, spec)
	if err != nil {
		return 0, collect.Query{}, withIdent(ctx, err)
	}
	return id, q, nil
}

func parseSpoiler(ctx context.Context, raw string) (int16, error) {
	if raw == "" {
		return 0, nil
	}
	key, err := parse.Enum(raw, "spoiler", vocab.Tokens("spoiler"))
	if err != nil {
		return 0, withIdent(ctx, err)
	}
	level, _ := repr.SpoilerFromKey(key)
	return level, nil
}

func catalogErr(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	var p *problem.Problem
	if errors.As(err, &p) {
		return withIdent(ctx, p)
	}
	return err
}
