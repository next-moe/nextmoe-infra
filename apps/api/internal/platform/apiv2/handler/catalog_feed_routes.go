package handler

import (
	"context"
	"net/http"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

type listChangesOutput struct {
	Body repr.List[repr.Change]
}
type listRedirectsOutput struct {
	Body repr.List[repr.Redirect]
}

type listRedirectsInput struct {
	CollectionInput
	Object string `query:"object" maxLength:"32" doc:"Restrict to one family. Closed: work, release, character, credit_name, person, company, tag, engine."`
}

type calendarInput struct {
	CollectionInput
	Month        string `query:"month" maxLength:"7" doc:"Dated month window YYYY-MM. Default: current month in Asia/Tokyo."`
	Year         string `query:"year" maxLength:"4" doc:"Year-only window YYYY (v1 pending). Default with precision=year: current year in Asia/Tokyo."`
	Precision    string `query:"precision" maxLength:"8" doc:"day, month, or year. year selects the year-only window. day and month use the dated month window."`
	Status       string `query:"status" maxLength:"16" doc:"released, dated, announced, cancelled, unknown. announced and unknown select the undated window. cancelled is empty until the catalog records cancellations."`
	ContentLimit string `query:"content_limit" maxLength:"32" doc:"Comma-separated closed editorial axis: sfw, nsfw."`
	OLang        string `query:"olang" maxLength:"64" doc:"Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing. Absent = the calendar's home population, ja plus zh."`
}

type listCalendarOutput struct {
	Body repr.CalendarList
}

func registerCatalogFeeds(api huma.API, cat *Catalog) {
	catalog := []string{"catalog"}
	errs := collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable)
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogChanges",
		Method:      http.MethodGet,
		Path:        "/v2/catalog/changes",
		Summary:     "Catalog changes feed",
		Description: "Works updated recently, oldest first. Keyset-paginated. Requires an application key or a user access token with catalog:read. ids= is not accepted.\n\n" +
			"**This is the mirror channel.** If you cache any catalog-owned fact per work — above all the editorial display axis `content_limit`, whose verdict is `claimed_by.content_limit` when the claim block is present and otherwise `nsfw` when `content_rating` is `r18`, `sfw` otherwise — poll this feed instead of sweeping the catalog. Every write that changes a work's claim state, its display axis (the editorial NSFW flag or its content rating), or its existence bumps `updated_at` and surfaces the id here.\n\n" +
			"Bootstrap from an empty cursor: the feed enumerates the whole population oldest-updated-first, so the first drain IS the full inventory. Hydrate each page against /v2/catalog/works with ids= in batches of at most 100, with both gates open (nsfw=true and no content_limit), then keep the cursor and poll it at your own cadence.\n\n" +
			"gone: an entry carrying `gone: true` has left the public population — drop the mirrored row. Merged-away ids appear here as gone AND in /v2/catalog/redirects, which names the id that replaced them; repoint rather than delete when the redirect exists.\n\n" +
			"Everything else a work serves — covers, tags, titles, intros, ratings — surfaces best-effort: most of those writers touch the work too, but only claim state, the display axis and existence are promised.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogChanges(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogRedirects",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/redirects",
		Summary:            "Entity merge feed",
		Description:        "Redirects from merged-away ids, oldest first. Keyset-paginated. object= restricts to one family. Requires an application key or a user access token with catalog:read. ids= is not accepted.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogRedirects(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listCatalogCalendar",
		Method:             http.MethodGet,
		Path:               "/v2/catalog/calendar",
		Summary:            "Release calendar",
		Description:        "One collection. month=/year= pick a window; precision= and status= select among the dated month, year-only, and undated views that were three v1 routes. content_limit= gates on the editorial display axis and olang= on the original language (absent = ja plus zh). meta carries today plus, on the dated month window, min_month/max_month/has_prev/has_next for month navigation. Requires an application key or a user access token with catalog:read. ids= is not accepted. include=titles,refs,intros,covers,companies,ratings,tags,credits fills on this lane; view=full is all of them except credits, which is an explicit ask. On a collection lane titles elects latin/localized and covers elects the two cover slots that grade the base cover — the full titles[] and covers[] arrays, and relations/releases/popularity/playtimes/series/platforms/screenshots/characters/engines/links, are per-record blocks and live on /v2/catalog/works/{id} and its sub-resources; asking for one here is 400 UNKNOWN_INCLUDE.",
		Tags:               catalog,
		Errors:             errs,
		SkipValidateParams: true,
	}, listCatalogCalendar(cat))
}

func listCatalogChanges(cat *Catalog) func(context.Context, *CollectionInput) (*listChangesOutput, error) {
	return func(ctx context.Context, in *CollectionInput) (*listChangesOutput, error) {
		q, err := parseCatalogList(ctx, in, collect.ChangesSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListChanges(ctx, q)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listChangesOutput{Body: page}, nil
	}
}

func listCatalogRedirects(cat *Catalog) func(context.Context, *listRedirectsInput) (*listRedirectsOutput, error) {
	return func(ctx context.Context, in *listRedirectsInput) (*listRedirectsOutput, error) {
		if in == nil {
			in = &listRedirectsInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.RedirectsSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListRedirects(ctx, q, in.Object)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listRedirectsOutput{Body: page}, nil
	}
}

func listCatalogCalendar(cat *Catalog) func(context.Context, *calendarInput) (*listCalendarOutput, error) {
	return func(ctx context.Context, in *calendarInput) (*listCalendarOutput, error) {
		if in == nil {
			in = &calendarInput{}
		}
		q, err := parseCatalogList(ctx, &in.CollectionInput, collect.CalendarSpec())
		if err != nil {
			return nil, err
		}
		page, lerr := cat.ListCalendar(ctx, q, calendarParams{
			Month: in.Month, Year: in.Year, Precision: in.Precision, Status: in.Status,
			ContentLimit: in.ContentLimit, OLang: in.OLang,
		})
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listCalendarOutput{Body: page}, nil
	}
}
