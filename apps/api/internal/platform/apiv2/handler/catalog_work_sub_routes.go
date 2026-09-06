package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

// Exported for the same reason as ResourceIDInput: huma drops the params of an
// unexported embedded input struct without a word.
type WorkSubInput struct {
	ID     string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Decimal catalog work id."`
	NSFW   string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false."`
	Cursor string `query:"cursor" maxLength:"512" doc:"Opaque keyset cursor from a prior next_cursor. Must start with cur_."`
	Limit  string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20."`
}

type workTagsInput struct {
	WorkSubInput
	Spoiler string `query:"spoiler" maxLength:"16" doc:"Spoiler ceiling for this page: none (default), minor or major. Closed vocabulary; an unknown value is 400. Tag rows above the ceiling are not returned. Only the VNDB-derived tag vocabulary carries a spoiler level — Bangumi and DLsite folksonomy publish no spoiler concept, so those rows read none — and the default is the safe ceiling."`
}

func registerWorkSubs(api huma.API, cat *Catalog) {
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	type op struct {
		id, path, summary, desc string
		handler                 any
	}
	catalog := []string{"catalog"}
	huma.Register(api, subOp("getCatalogWorkCovers", "/v2/catalog/works/{id}/covers", "List covers of one work", "Work cover rows. Same items as include=covers. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkCovers(cat))
	huma.Register(api, subOp("getCatalogWorkScreenshots", "/v2/catalog/works/{id}/screenshots", "List screenshots of one work", "Work screenshots. Same items as include=screenshots. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkScreenshots(cat))
	huma.Register(api, subOp("getCatalogWorkTags", "/v2/catalog/works/{id}/tags", "List tags of one work", "Tags attached to this work. Same items as include=tags. spoiler=none|minor|major is the ceiling of this page and defaults to none, exactly as on the work detail face. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkTags(cat))
	huma.Register(api, subOp("getCatalogWorkCharacters", "/v2/catalog/works/{id}/characters", "List characters of one work", "Roster characters. Same items as include=characters. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkCharacters(cat))
	huma.Register(api, subOp("getCatalogWorkCredits", "/v2/catalog/works/{id}/credits", "List credits of one work", "Credits grouped by role. Same items as include=credits. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkCredits(cat))
	huma.Register(api, subOp("getCatalogWorkReleases", "/v2/catalog/works/{id}/releases", "List releases of one work", "Releases of this work. Same items as include=releases. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkReleases(cat))
	huma.Register(api, subOp("getCatalogWorkIntros", "/v2/catalog/works/{id}/intros", "List intros of one work", "Intros. Same items as include=intros. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkIntros(cat))
	huma.Register(api, subOp("getCatalogWorkRatings", "/v2/catalog/works/{id}/ratings", "List ratings of one work", "Source ratings. Same items as include=ratings. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkRatings(cat))
	huma.Register(api, subOp("getCatalogWorkRelations", "/v2/catalog/works/{id}/relations", "List relations of one work", "Related works. Same items as include=relations. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkRelations(cat))
	huma.Register(api, subOp("getCatalogWorkSeries", "/v2/catalog/works/{id}/series", "List series of one work", "Series memberships. Same items as include=series. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkSeries(cat))
	huma.Register(api, subOp("getCatalogWorkLinks", "/v2/catalog/works/{id}/links", "List links of one work", "Outbound links. Same items as include=links. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkLinks(cat))
	huma.Register(api, subOp("getCatalogWorkEngines", "/v2/catalog/works/{id}/engines", "List engines of one work", "Engines. Same items as include=engines. Requires an application key or a user access token with catalog:read.", errs, catalog), getWorkEngines(cat))
}

func subOp(id, path, summary, desc string, errs []int, tags []string) huma.Operation {
	return huma.Operation{
		OperationID: id, Method: http.MethodGet, Path: path,
		Summary: summary, Description: desc, Tags: tags, Errors: errs, SkipValidateParams: true,
	}
}

type listCoversOut struct{ Body repr.List[repr.Cover] }
type listScreenshotsOut struct{ Body repr.List[repr.Screenshot] }
type listWorkTagsOut struct{ Body repr.List[repr.WorkTag] }
type listWorkCharsOut struct{ Body repr.List[repr.WorkCharacter] }
type listCreditsOut struct{ Body repr.List[repr.CreditGroup] }
type listReleasesOut struct{ Body repr.List[repr.Release] }
type listIntrosOut struct{ Body repr.List[repr.Intro] }
type listRatingsOut struct{ Body repr.List[repr.Rating] }
type listRelationsOut struct{ Body repr.List[repr.Relation] }
type listWorkSeriesOut struct{ Body repr.List[repr.WorkSeriesRef] }
type listLinksOut struct{ Body repr.List[repr.WorkLink] }
type listWorkEnginesOut struct{ Body repr.List[repr.WorkEngineRef] }

func parseWorkSub(ctx context.Context, in *WorkSubInput) (int64, bool, string, int, error) {
	if in == nil {
		in = &WorkSubInput{}
	}
	id, ok := repr.ParseID(in.ID)
	if !ok {
		p := problem.New(problem.CodeInvalidParameter, "", "", "id must be a positive decimal catalog id.")
		p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: in.ID}}
		return 0, false, "", 0, withIdent(ctx, p)
	}
	q, err := collect.Parse(collect.Raw{Cursor: in.Cursor, Limit: in.Limit, NSFW: in.NSFW}, collect.WorkSubSpec())
	if err != nil {
		return 0, false, "", 0, withIdent(ctx, err)
	}
	return id, q.NSFW, q.Cursor, q.Limit, nil
}

func getWorkCovers(cat *Catalog) func(context.Context, *WorkSubInput) (*listCoversOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listCoversOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkCovers(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listCoversOut{Body: page}, nil
	}
}

func getWorkScreenshots(cat *Catalog) func(context.Context, *WorkSubInput) (*listScreenshotsOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listScreenshotsOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkScreenshots(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listScreenshotsOut{Body: page}, nil
	}
}

func getWorkTags(cat *Catalog) func(context.Context, *workTagsInput) (*listWorkTagsOut, error) {
	return func(ctx context.Context, in *workTagsInput) (*listWorkTagsOut, error) {
		if in == nil {
			in = &workTagsInput{}
		}
		id, nsfw, cur, limit, err := parseWorkSub(ctx, &in.WorkSubInput)
		if err != nil {
			return nil, err
		}
		spoiler, serr := parseSpoiler(ctx, in.Spoiler)
		if serr != nil {
			return nil, serr
		}
		page, gerr := cat.WorkTags(ctx, id, nsfw, spoiler, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listWorkTagsOut{Body: page}, nil
	}
}

func getWorkCharacters(cat *Catalog) func(context.Context, *WorkSubInput) (*listWorkCharsOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listWorkCharsOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkCharacters(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listWorkCharsOut{Body: page}, nil
	}
}

func getWorkCredits(cat *Catalog) func(context.Context, *WorkSubInput) (*listCreditsOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listCreditsOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkCredits(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listCreditsOut{Body: page}, nil
	}
}

func getWorkReleases(cat *Catalog) func(context.Context, *WorkSubInput) (*listReleasesOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listReleasesOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkReleases(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listReleasesOut{Body: page}, nil
	}
}

func getWorkIntros(cat *Catalog) func(context.Context, *WorkSubInput) (*listIntrosOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listIntrosOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkIntros(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listIntrosOut{Body: page}, nil
	}
}

func getWorkRatings(cat *Catalog) func(context.Context, *WorkSubInput) (*listRatingsOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listRatingsOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkRatings(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listRatingsOut{Body: page}, nil
	}
}

func getWorkRelations(cat *Catalog) func(context.Context, *WorkSubInput) (*listRelationsOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listRelationsOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkRelations(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listRelationsOut{Body: page}, nil
	}
}

func getWorkSeries(cat *Catalog) func(context.Context, *WorkSubInput) (*listWorkSeriesOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listWorkSeriesOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkSeries(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listWorkSeriesOut{Body: page}, nil
	}
}

func getWorkLinks(cat *Catalog) func(context.Context, *WorkSubInput) (*listLinksOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listLinksOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkLinks(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listLinksOut{Body: page}, nil
	}
}

func getWorkEngines(cat *Catalog) func(context.Context, *WorkSubInput) (*listWorkEnginesOut, error) {
	return func(ctx context.Context, in *WorkSubInput) (*listWorkEnginesOut, error) {
		id, nsfw, cur, limit, err := parseWorkSub(ctx, in)
		if err != nil {
			return nil, err
		}
		page, gerr := cat.WorkEngines(ctx, id, nsfw, cur, limit)
		if gerr != nil {
			return nil, catalogErr(ctx, gerr)
		}
		return &listWorkEnginesOut{Body: page}, nil
	}
}
