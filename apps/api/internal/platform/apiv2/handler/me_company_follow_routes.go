package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listCompanyFollowsInput struct {
	CollectionInput
	CompanyIDs string `query:"company_ids" maxLength:"4096" doc:"Comma-separated company ids, max 100. Batch read, no pagination."`
}
type listCompanyFollowsOutput struct {
	Body repr.List[repr.CompanyFollow]
}
type getCompanyFollowInput struct {
	CompanyID string `path:"company_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog company id."`
}
type getCompanyFollowOutput struct {
	Body repr.CompanyFollow
}
type deleteCompanyFollowInput struct {
	CompanyID string `path:"company_id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Catalog company id."`
}

type myCalendarInput struct {
	CollectionInput
	Month              string `query:"month" maxLength:"7" doc:"Dated month window YYYY-MM. Default: current month in Asia/Tokyo."`
	Year               string `query:"year" maxLength:"4" doc:"Year-only window YYYY (v1 pending). Default with precision=year: current year in Asia/Tokyo."`
	Precision          string `query:"precision" maxLength:"8" doc:"day, month, or year. year selects the year-only window. day and month use the dated month window."`
	Status             string `query:"status" maxLength:"16" doc:"released, dated, announced, cancelled, unknown. announced and unknown select the undated window. cancelled is empty until the catalog records cancellations."`
	ContentLimit       string `query:"content_limit" maxLength:"32" doc:"Comma-separated closed editorial axis: sfw, nsfw."`
	OLang              string `query:"olang" maxLength:"64" doc:"Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = all languages. The population is already the bearer's choice, so this face does not default to ja."`
	ExcludeCompanyKind string `query:"exclude_company_kind" maxLength:"96" doc:"Comma-separated closed company_kind values: game_brand, bunko, publisher, anime_studio, doujin_circle, group, or none alone to exclude nothing. Drops a work only when it has at least one company and every one of its companies is of an excluded kind; a work that also carries a company of another kind stays, and so does a work with no company. Applies to the page, total, and the month navigation in meta. Absent = exclude nothing. The population is already the bearer's choice, so this face does not default to doujin_circle."`
}

func registerMeCompanyFollows(api huma.API, cat *Catalog) {
	me := []string{"me"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listMyCompanyFollows", Method: http.MethodGet, Path: "/v2/me/followed-companies",
		Summary:     "List my followed companies",
		Description: "The bearer user's followed companies, newest follow first. Items carry ids only and are hydrated with /v2/catalog/companies?ids=. company_ids= is a batch read. The list is private to its owner; only the count is public (follower_count on /v2/catalog/companies/{id}). Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyCompanyFollows(cat))
	huma.Register(api, huma.Operation{
		OperationID: "getMyCompanyFollow", Method: http.MethodGet, Path: "/v2/me/followed-companies/{company_id}",
		Summary:     "Get my follow on one company",
		Description: "404 when the bearer is not following this company. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, getMyCompanyFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "putMyCompanyFollow", Method: http.MethodPut, Path: "/v2/me/followed-companies/{company_id}",
		Summary:     "Follow a company",
		Description: "No request body. Idempotent: following an already-followed company answers the stored row and writes nothing. Unknown or deleted companies are 404, or the merged-entity problem when the id was merged away. At most 1000 followed companies. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, putMyCompanyFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "deleteMyCompanyFollow", Method: http.MethodDelete, Path: "/v2/me/followed-companies/{company_id}",
		Summary:     "Unfollow a company",
		Description: "204 with no body, also when the bearer was not following. Requires a user access token. Any app may call this; no scope is required.",
		Tags:        me, Errors: errs, DefaultStatus: http.StatusNoContent, SkipValidateParams: true,
	}, deleteMyCompanyFollow(cat))
	huma.Register(api, huma.Operation{
		OperationID: "listMyCalendar", Method: http.MethodGet, Path: "/v2/me/calendar",
		Summary:     "My release calendar",
		Description: "The bearer's release calendar: the works of the companies they follow, with the same windows, parameters and meta as /v2/catalog/calendar. Absent olang means all languages and absent exclude_company_kind excludes nothing, because the population is already the bearer's choice; the public calendar's defaults (Japanese, commercial only) would hide a followed doujin circle or a Chinese company. A user who follows no company gets an empty window. Requires a user access token. Any app may call this; no scope is required. ids= is not accepted. include=titles,refs,intros,covers,companies,ratings,tags,credits fills on this lane; view=full is all of them except credits, which is an explicit ask. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations/releases/popularity/playtimes/series/platforms/screenshots/characters/engines/links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE.",
		Tags:        me, Errors: errs, SkipValidateParams: true,
	}, listMyCalendar(cat))
}

func listMyCompanyFollows(cat *Catalog) func(context.Context, *listCompanyFollowsInput) (*listCompanyFollowsOutput, error) {
	return func(ctx context.Context, in *listCompanyFollowsInput) (*listCompanyFollowsOutput, error) {
		if in == nil {
			in = &listCompanyFollowsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.CompanyFollowSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListCompanyFollows(ctx, q, splitWorkIDs(in.CompanyIDs))
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCompanyFollowsOutput{Body: page}, nil
	}
}

func getMyCompanyFollow(cat *Catalog) func(context.Context, *getCompanyFollowInput) (*getCompanyFollowOutput, error) {
	return func(ctx context.Context, in *getCompanyFollowInput) (*getCompanyFollowOutput, error) {
		if in == nil {
			in = &getCompanyFollowInput{}
		}
		id, ok := repr.ParseID(in.CompanyID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.CompanyID))
		}
		rec, err := cat.GetCompanyFollow(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getCompanyFollowOutput{Body: rec}, nil
	}
}

func putMyCompanyFollow(cat *Catalog) func(context.Context, *getCompanyFollowInput) (*getCompanyFollowOutput, error) {
	return func(ctx context.Context, in *getCompanyFollowInput) (*getCompanyFollowOutput, error) {
		if in == nil {
			in = &getCompanyFollowInput{}
		}
		id, ok := repr.ParseID(in.CompanyID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.CompanyID))
		}
		rec, err := cat.PutCompanyFollow(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getCompanyFollowOutput{Body: rec}, nil
	}
}

func deleteMyCompanyFollow(cat *Catalog) func(context.Context, *deleteCompanyFollowInput) (*struct{}, error) {
	return func(ctx context.Context, in *deleteCompanyFollowInput) (*struct{}, error) {
		if in == nil {
			in = &deleteCompanyFollowInput{}
		}
		id, ok := repr.ParseID(in.CompanyID)
		if !ok {
			return nil, catalogErr(ctx, problemInvalidID(in.CompanyID))
		}
		if err := cat.DeleteCompanyFollow(ctx, id); err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &struct{}{}, nil
	}
}

func listMyCalendar(cat *Catalog) func(context.Context, *myCalendarInput) (*listCalendarOutput, error) {
	return func(ctx context.Context, in *myCalendarInput) (*listCalendarOutput, error) {
		if in == nil {
			in = &myCalendarInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.CalendarSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListMyCalendar(ctx, q, calendarParams{
			Month: in.Month, Year: in.Year, Precision: in.Precision, Status: in.Status,
			ContentLimit: in.ContentLimit, OLang: in.OLang, ExcludeCompanyKind: in.ExcludeCompanyKind,
		})
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCalendarOutput{Body: page}, nil
	}
}
