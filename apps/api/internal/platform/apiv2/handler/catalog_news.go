package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	newsmodel "api/internal/platform/news/model"
	newssvc "api/internal/platform/news/service"

	"github.com/danielgtaylor/huma/v2"
)

type newsIDInput struct {
	ID string `path:"id" minLength:"1" maxLength:"20" pattern:"^[0-9]+$" doc:"Decimal news item id."`
}

// The four filters news/service.FeedFilter has always supported. They were not
// declared, so SkipValidateParams dropped them without a word and the forum —
// which sends all four on every request — was served an unfiltered feed.
// published_* is an instant and not a YYYY-MM-DD day like released_after= on
// works: published_at is a timestamptz the caller windows to the minute, and
// the one live caller already sends RFC 3339.
type listNewsInput struct {
	CollectionInput
	Lane            string `query:"lane" maxLength:"64" doc:"Comma-separated closed lanes: news, column. Absent means both. An unknown lane is 400 UNKNOWN_ENUM_VALUE."`
	Source          string `query:"source" maxLength:"256" doc:"Comma-separated news source keys, at most 20. Open vocabulary; an unknown key matches nothing rather than failing."`
	PublishedAfter  string `query:"published_after" maxLength:"40" doc:"Inclusive lower bound on published_at. RFC 3339 UTC ending in Z."`
	PublishedBefore string `query:"published_before" maxLength:"40" doc:"Inclusive upper bound on published_at. RFC 3339 UTC ending in Z."`
}

// The bound source= carries because its vocabulary is open: without one a
// caller could paste an unbounded IN list into the feed query.
const newsMaxSources = 20

type listNewsOutput struct {
	Body repr.List[repr.NewsItem]
}
type getNewsOutput struct {
	Body repr.NewsItem
}
type listNewsSourcesOutput struct {
	Body repr.List[repr.NewsSource]
}

func registerNews(api huma.API, cat *Catalog) {
	news := []string{"news"}
	huma.Register(api, huma.Operation{
		OperationID:        "listNews",
		Method:             http.MethodGet,
		Path:               "/v2/news",
		Summary:            "List news items",
		Description:        "Published news feed. Keyset-paginated. Unauthenticated. Attribution fields are required on every item. lane=, source=, published_after= and published_before= narrow the population, and include_total= counts the narrowed one. ids= and refs= are not accepted.",
		Tags:               news,
		Errors:             collectionErrors(http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listNews(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "listNewsSources",
		Method:             http.MethodGet,
		Path:               "/v2/news/sources",
		Summary:            "List news sources",
		Description:        "Every attributed news source. Unauthenticated.",
		Tags:               news,
		Errors:             collectionErrors(http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listNewsSources(cat))
	huma.Register(api, huma.Operation{
		OperationID:        "getNewsItem",
		Method:             http.MethodGet,
		Path:               "/v2/news/{id}",
		Summary:            "Get one news item",
		Description:        "A published news item. A withdrawn item is 410 GONE, not 404: a mirror that only sees the item leave the list never learns the copy it took was pulled. An item that never existed, or is still pending, is 404. Unauthenticated. source and source_url are always present.",
		Tags:               news,
		Errors:             collectionErrors(http.StatusNotFound, http.StatusGone, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, getNewsItem(cat))
}

func listNews(cat *Catalog) func(context.Context, *listNewsInput) (*listNewsOutput, error) {
	return func(ctx context.Context, in *listNewsInput) (*listNewsOutput, error) {
		if in == nil {
			in = &listNewsInput{}
		}
		q, err := collect.Parse(rawFrom(&in.CollectionInput), collect.NewsSpec())
		if err != nil {
			return nil, withIdent(ctx, err)
		}
		f, ferr := parseNewsFeedFilter(in)
		if ferr != nil {
			return nil, withIdent(ctx, ferr)
		}
		page, lerr := cat.ListNews(ctx, q, f)
		if lerr != nil {
			return nil, catalogErr(ctx, lerr)
		}
		return &listNewsOutput{Body: page}, nil
	}
}

func parseNewsFeedFilter(in *listNewsInput) (newssvc.FeedFilter, *problem.Problem) {
	var f newssvc.FeedFilter
	lanes, err := closedCSV(in.Lane, "lane", []string{newsmodel.LaneNews, newsmodel.LaneColumn})
	if err != nil {
		return f, err
	}
	f.Lanes = lanes

	for _, key := range strings.Split(in.Source, ",") {
		if key = strings.TrimSpace(key); key != "" {
			f.Sources = append(f.Sources, key)
		}
	}
	if len(f.Sources) > newsMaxSources {
		p := problem.New(problem.CodeInvalidParameter, "", "", "source= accepts at most 20 keys.")
		p.Errors = []problem.FieldError{{
			Parameter: "source", Reason: problem.ReasonTooManyItems, Detail: "maximum 20",
		}}
		return newssvc.FeedFilter{}, p
	}

	for _, w := range []struct {
		raw, name string
		into      *time.Time
	}{
		{in.PublishedAfter, "published_after", &f.PublishedAfter},
		{in.PublishedBefore, "published_before", &f.PublishedBefore},
	} {
		if w.raw == "" {
			continue
		}
		t, perr := parse.DateTimeUTC(w.raw, w.name)
		if perr != nil {
			return newssvc.FeedFilter{}, perr
		}
		*w.into = t
	}
	if !f.PublishedAfter.IsZero() && !f.PublishedBefore.IsZero() && f.PublishedAfter.After(f.PublishedBefore) {
		p := problem.New(problem.CodeInvalidParameter, "", "", "the published window is empty.")
		p.Errors = []problem.FieldError{{
			Parameter: "published_after", Reason: problem.ReasonOutOfRange,
			Detail: "published_after must not be later than published_before",
		}}
		return newssvc.FeedFilter{}, p
	}
	return f, nil
}

func listNewsSources(cat *Catalog) func(context.Context, *struct{}) (*listNewsSourcesOutput, error) {
	return func(ctx context.Context, _ *struct{}) (*listNewsSourcesOutput, error) {
		page, err := cat.NewsSources(ctx)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &listNewsSourcesOutput{Body: page}, nil
	}
}

func getNewsItem(cat *Catalog) func(context.Context, *newsIDInput) (*getNewsOutput, error) {
	return func(ctx context.Context, in *newsIDInput) (*getNewsOutput, error) {
		id, ok := repr.ParseID(in.ID)
		if !ok {
			p := problem.New(problem.CodeInvalidParameter, "", "", "id must be a positive decimal id.")
			p.Errors = []problem.FieldError{{Parameter: "id", Reason: problem.ReasonInvalidFormat, Detail: in.ID}}
			return nil, withIdent(ctx, p)
		}
		rec, err := cat.NewsItem(ctx, id)
		if err != nil {
			return nil, catalogErr(ctx, err)
		}
		return &getNewsOutput{Body: rec}, nil
	}
}
