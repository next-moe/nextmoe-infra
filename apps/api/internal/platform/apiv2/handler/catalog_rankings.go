package handler

import (
	"context"
	"errors"
	"math"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/parse"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	catmodel "api/internal/platform/catalog/model"
	"api/internal/platform/catalog/ratingmodel"
	catsvc "api/internal/platform/catalog/service"

	"github.com/danielgtaylor/huma/v2"
)

type listRankingsInput struct {
	Cursor         string `query:"cursor" maxLength:"512" doc:"Opaque cursor from a prior next_cursor. Must start with cur_."`
	Limit          string `query:"limit" maxLength:"8" doc:"Page size 1-100, default 20. Values above 100 are 400 LIMIT_TOO_LARGE, not clamped."`
	View           string `query:"view" maxLength:"16" doc:"basic (default) or full, for the work inside each entry."`
	Include        string `query:"include" maxLength:"1024" doc:"Comma-separated blocks of the work inside each entry, as on GET /v2/catalog/works. Unknown token is 400 UNKNOWN_INCLUDE."`
	IncludeTotal   string `query:"include_total" maxLength:"8" doc:"true to include total. Only true or false."`
	Facets         string `query:"facets" maxLength:"512" doc:"Comma-separated facet names over the ranked population: tag_id, company_id, olang, content_rating, platform, circulation."`
	NSFW           string `query:"nsfw" maxLength:"8" doc:"true includes r18. false or absent hides r18. Only true or false."`
	By             string `query:"by" maxLength:"32" doc:"consensus (default) ranks by every source combined; a source key (vndb, bangumi, dlsite, erogamescape, nextmoe, howlongtobeat) ranks by that source alone."`
	Metric         string `query:"metric" maxLength:"32" doc:"The statistic to rank by. With by=consensus: lower, upper, mean, percentile, divergence, votes, sources. With a source: lower, upper, mean, median, native, percentile, spread, votes. The default is lower for order=desc and upper for order=asc: the bottom of the 90% interval for a best-of list, so a handful of enthusiastic votes cannot outrank an established work, and its top for a worst-of list, so a work is listed as bad only when it is bad with confidence. mean ranks by the best estimate; native by the source's own published score; divergence by how much the sources disagree."`
	Order          string `query:"order" maxLength:"8" doc:"desc (default, best first) or asc (worst first)."`
	Sources        string `query:"sources" maxLength:"128" doc:"by=consensus only: comma-separated source keys to build the consensus from, for example erogamescape,bangumi. Absent = every source. Every number in consensus then covers only these sources."`
	MinSources     string `query:"min_sources" maxLength:"2" doc:"by=consensus only: keep works at least this many of the consensus sources rated, 1-6."`
	MinVotes       string `query:"min_votes" maxLength:"256" doc:"Vote floors, comma-separated. A bare number applies to by= (the total over the consensus sources for by=consensus); source:number applies to that source, for example erogamescape:50,bangumi:100. Every floor must hold."`
	MaxVotes       string `query:"max_votes" maxLength:"256" doc:"Vote ceilings, in the same form as min_votes, for lists of lesser-known works: min_votes=5&max_votes=50 keeps works between five and fifty votes."`
	ScoreMin       string `query:"score_min" maxLength:"16" doc:"Keep works whose value (in the list's unit) is at least this."`
	ScoreMax       string `query:"score_max" maxLength:"16" doc:"Keep works whose value (in the list's unit) is at most this."`
	Year           string `query:"year" maxLength:"9" doc:"Release year, or an inclusive range such as 2015-2020. Cannot be combined with released_after or released_before."`
	ReleasedAfter  string `query:"released_after" maxLength:"10" doc:"YYYY-MM-DD inclusive, on each work's release_date."`
	ReleasedBefore string `query:"released_before" maxLength:"10" doc:"YYYY-MM-DD inclusive, on each work's release_date."`
	Circulation    string `query:"circulation" maxLength:"16" doc:"commercial: a game brand made or sold it. doujin: only a doujin circle did. Works with neither credit match neither."`
	Platform       string `query:"platform" maxLength:"256" doc:"Comma-separated platform tokens (win, ps4, swi, …), any of. Open vocabulary; unknown values match nothing."`
	PlaytimeMin    string `query:"playtime_min" maxLength:"8" doc:"Minutes. Keep works whose playtime is at least this; playtime is the median of the sources' estimates."`
	PlaytimeMax    string `query:"playtime_max" maxLength:"8" doc:"Minutes. Keep works whose playtime is at most this."`
	ContentRating  string `query:"content_rating" maxLength:"16" doc:"Closed: all_ages, sensitive, r18. r18 requires nsfw=true."`
	ContentLimit   string `query:"content_limit" maxLength:"32" doc:"Comma-separated closed editorial axis: sfw, nsfw."`
	Claimed        string `query:"claimed" maxLength:"8" doc:"true or false. Absent = no gate."`
	CompanyID      string `query:"company_id" maxLength:"20" doc:"Catalog company id."`
	TagID          string `query:"tag_id" maxLength:"256" doc:"Comma-separated canonical tag ids, AND, max 10."`
	ExcludeTagID   string `query:"exclude_tag_id" maxLength:"2048" doc:"Comma-separated canonical tag ids, max 100. Drops every work that carries any of them."`
	ExcludeTagTree string `query:"exclude_tag_tree" maxLength:"2048" doc:"Comma-separated canonical tag ids, max 100. As exclude_tag_id, and each id also drops the works carrying any tag below it."`
	SeriesID       string `query:"series_id" maxLength:"20" doc:"Catalog series id."`
	EngineID       string `query:"engine_id" maxLength:"20" doc:"Catalog engine id."`
	OLang          string `query:"olang" maxLength:"64" doc:"Comma-separated BCP-47, or all. Open vocabulary; unknown values match nothing."`
	PageInput
}

type listRankingsOutput struct {
	Body repr.ListRanking
}

func registerCatalogRankings(api huma.API, cat *Catalog) {
	huma.Register(api, huma.Operation{
		OperationID: "listCatalogRankings",
		Method:      http.MethodGet,
		Path:        "/v2/catalog/rankings",
		Summary:     "Rank works by their ratings",
		Description: "Ranks works by their ratings on every source, or on one. Every source keeps its own published score (per_source[].score); next to it each entry carries what a fitted model makes of it. " +
			"Per source, mean is the work's own mean pulled toward what works with as many votes usually score, so a work with three votes cannot outrank one with three thousand on luck, with a 90% interval and the work's percentile in that source. " +
			"The consensus puts the sources on one scale: each source's estimate becomes a normal score within that source, a one-factor model fitted on the works several sources share corrects for each source's audience, and the sources are combined weighted by how much each one knows about the work. It is read as a deviation score (偏差値): 50 is the typical work rated by two or more sources and every 10 points is one standard deviation. " +
			"The model is refitted from the data nightly and the ranking reads the search index, rebuilt nightly; model describes the fit in use. " +
			"Every filter of GET /v2/catalog/works that the search index carries applies here, plus year=, circulation=, platform=, playtime_min/max=, vote floors and score bounds. page= selects page mode. Answers 503 until the first model is fitted. " +
			"Anonymous calls are allowed and limited per IP address; an application key or a user access token with catalog:read gets its own, higher limit.",
		Tags:               []string{"catalog"},
		Errors:             collectionErrors(http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable),
		SkipValidateParams: true,
	}, listRankings(cat))
}

func rankingSpec() collect.Spec {
	return collect.Spec{
		Include: collect.WorkListInclude,
		FullSet: collect.WorkListFullSet,
		Fields:  collect.WorkBasicFields,
		Facets:  []string{"tag_id", "company_id", "olang", "content_rating", "platform", "circulation"},
		NoBatch: true,
		Pages:   true,
	}
}

func listRankings(cat *Catalog) func(context.Context, *listRankingsInput) (*listRankingsOutput, error) {
	return func(ctx context.Context, in *listRankingsInput) (*listRankingsOutput, error) {
		if in == nil {
			in = &listRankingsInput{}
		}
		raw := rawFrom(&CollectionInput{
			Cursor: in.Cursor, Limit: in.Limit, View: in.View, Include: in.Include,
			IncludeTotal: in.IncludeTotal, Facets: in.Facets, NSFW: in.NSFW,
		})
		raw.Page = in.Page
		q, perr := collect.Parse(raw, rankingSpec())
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		if cat == nil || cat.Public == nil {
			return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "rankings are not bound."))
		}
		rf, perr := parseRankings(in)
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		wf, perr := parseWorksFilter(&listWorksInput{
			ContentRating: in.ContentRating, ContentLimit: in.ContentLimit, Claimed: in.Claimed,
			CompanyID: in.CompanyID, TagID: in.TagID, ExcludeTagID: in.ExcludeTagID, ExcludeTagTree: in.ExcludeTagTree,
			SeriesID: in.SeriesID, EngineID: in.EngineID, OLang: in.OLang,
			ReleasedAfter: in.ReleasedAfter, ReleasedBefore: in.ReleasedBefore,
		}, false)
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		if perr := applyYear(in, &wf); perr != nil {
			return nil, withIdent(ctx, perr)
		}
		if wf.ContentRating != nil && *wf.ContentRating == catmodel.ContentRatingR18 && !q.NSFW {
			p := problem.New(problem.CodeInvalidParameter, "", "", "content_rating=r18 requires nsfw=true.")
			p.Errors = []problem.FieldError{{Parameter: "content_rating", Reason: problem.ReasonNotAllowedValue, Detail: "nsfw=true is required"}}
			return nil, withIdent(ctx, p)
		}
		if len(wf.ExcludeTagIDs) > 0 || len(wf.ExcludeTrees) > 0 {
			excl, err := cat.Public.TagExclusion(ctx, wf.ExcludeTagIDs, wf.ExcludeTrees)
			if err != nil {
				return nil, catalogErr(ctx, err)
			}
			wf.ExcludeTagIDs = excl
		}
		page, perr := searchPage(q)
		if perr != nil {
			return nil, withIdent(ctx, perr)
		}
		inc := listWorksInclude(q.Include)
		rf.Works = catsvc.WorksSearchFilter{
			ContentRating: wf.ContentRating, Claimed: wf.Claimed, DisplayLimits: wf.DisplayLimits,
			LabelID: wf.CompanyID, TagIDs: wf.TagIDs, ExcludeTagIDs: wf.ExcludeTagIDs,
			EngineID: wf.EngineID, SeriesID: wf.SeriesID,
			ReleasedAfter: wf.ReleasedAfter, ReleasedBefore: wf.ReleasedBefore, OLang: wf.OLang,
			NSFW: q.NSFW, Facets: q.Facets, Page: page, Limit: q.Limit, Include: inc,
		}
		data, err := cat.Public.Rankings(ctx, rf)
		if err != nil {
			switch {
			case errors.Is(err, catsvc.ErrSearchUnavailable):
				return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "works search is not bound."))
			case errors.Is(err, ratingmodel.ErrNoModel):
				return nil, withIdent(ctx, problem.New(problem.CodeServiceUnavailable, "", "", "no rating model has been fitted yet."))
			}
			return nil, catalogErr(ctx, err)
		}
		return &listRankingsOutput{Body: rankingList(cat, q, rf, data, page)}, nil
	}
}

func parseRankings(in *listRankingsInput) (catsvc.RankingsFilter, *problem.Problem) {
	f := catsvc.RankingsFilter{By: catsvc.RankByConsensus}
	keys := ratingmodel.SourceKeys()
	if in.By != "" {
		v, err := parse.Enum(in.By, "by", append([]string{catsvc.RankByConsensus}, keys...))
		if err != nil {
			return f, err
		}
		f.By = v
	}
	if in.Order != "" {
		v, err := parse.Enum(in.Order, "order", []string{"desc", "asc"})
		if err != nil {
			return f, err
		}
		f.Asc = v == "asc"
	}
	f.Metric = catsvc.RankMetricLower
	if f.Asc {
		f.Metric = catsvc.RankMetricUpper
	}
	consensus := f.By == catsvc.RankByConsensus
	metrics := catsvc.RankingSourceMetrics
	if consensus {
		metrics = catsvc.RankingConsensusMetrics
	}
	if in.Metric != "" {
		v, err := parse.Enum(in.Metric, "metric", metrics)
		if err != nil {
			return f, err
		}
		f.Metric = v
	}
	if in.Sources != "" || in.MinSources != "" {
		name := "sources"
		if in.Sources == "" {
			name = "min_sources"
		}
		if !consensus {
			return f, inconsistent(name, name+"= shapes the consensus and needs by=consensus")
		}
	}
	srcs, err := closedCSV(in.Sources, "sources", keys)
	if err != nil {
		return f, err
	}
	if len(srcs) < len(keys) {
		f.Sources = srcs
	}
	if in.MinSources != "" {
		n, err := boundedInt(in.MinSources, "min_sources", 1, len(keys))
		if err != nil {
			return f, err
		}
		f.MinSources = n
	}
	if f.MinVotes, f.TotalVotes, err = parseVoteBounds(in.MinVotes, "min_votes", f.By, keys); err != nil {
		return f, err
	}
	if f.MaxVotes, f.TotalMax, err = parseVoteBounds(in.MaxVotes, "max_votes", f.By, keys); err != nil {
		return f, err
	}
	if f.Min, err = optionalFloat(in.ScoreMin, "score_min"); err != nil {
		return f, err
	}
	if f.Max, err = optionalFloat(in.ScoreMax, "score_max"); err != nil {
		return f, err
	}
	if in.Circulation != "" {
		v, err := parse.Enum(in.Circulation, "circulation", []string{"commercial", "doujin"})
		if err != nil {
			return f, err
		}
		f.Circulation = v
	}
	for _, tok := range strings.Split(in.Platform, ",") {
		if tok = strings.TrimSpace(tok); tok != "" && !slices.Contains(f.Platforms, tok) {
			f.Platforms = append(f.Platforms, tok)
		}
	}
	if in.PlaytimeMin != "" {
		if f.PlaytimeMin, err = boundedInt(in.PlaytimeMin, "playtime_min", 1, 1_000_000); err != nil {
			return f, err
		}
	}
	if in.PlaytimeMax != "" {
		if f.PlaytimeMax, err = boundedInt(in.PlaytimeMax, "playtime_max", 1, 1_000_000); err != nil {
			return f, err
		}
	}
	return f, nil
}

func parseVoteBounds(raw, name, by string, keys []string) (map[string]int, int, *problem.Problem) {
	var per map[string]int
	total := 0
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		src, num, named := strings.Cut(tok, ":")
		if !named {
			num, src = src, by
		}
		n, err := boundedInt(num, name, 1, 10_000_000)
		if err != nil {
			return nil, 0, err
		}
		if src == catsvc.RankByConsensus {
			total = n
			continue
		}
		if !slices.Contains(keys, src) {
			return nil, 0, closedParam(name, strings.Join(keys, ", "))
		}
		if per == nil {
			per = map[string]int{}
		}
		per[src] = n
	}
	return per, total, nil
}

func applyYear(in *listRankingsInput, f *worksFilter) *problem.Problem {
	raw := strings.TrimSpace(in.Year)
	if raw == "" {
		return nil
	}
	if in.ReleasedAfter != "" || in.ReleasedBefore != "" {
		p := problem.New(problem.CodeMutuallyExclusiveParameters, "", "", "year= cannot be combined with released_after= or released_before=.")
		p.Errors = []problem.FieldError{{Parameter: "year", Reason: problem.ReasonNotAllowedValue, Detail: "year= is shorthand for both"}}
		return p
	}
	from, to, ranged := strings.Cut(raw, "-")
	if !ranged {
		to = from
	}
	lo, err := boundedInt(from, "year", 1900, 2100)
	if err != nil {
		return err
	}
	hi, err := boundedInt(to, "year", 1900, 2100)
	if err != nil {
		return err
	}
	if hi < lo {
		return inconsistent("year", "the range ends before it starts")
	}
	f.ReleasedAfter, f.ReleasedBefore = int64(lo)*10000+101, int64(hi)*10000+1231
	return nil
}

func boundedInt(raw, name string, lo, hi int) (int, *problem.Problem) {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" must be an integer.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: raw}}
		return 0, p
	}
	if n < lo || n > hi {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" is out of range.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonOutOfRange,
			Detail: strconv.Itoa(lo) + "-" + strconv.Itoa(hi),
			Params: &problem.FieldParams{Minimum: problem.Ptr(float64(lo)), Maximum: problem.Ptr(float64(hi))}}}
		return 0, p
	}
	return n, nil
}

func optionalFloat(raw, name string) (*float64, *problem.Problem) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		p := problem.New(problem.CodeInvalidParameter, "", "", name+" must be a number.")
		p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInvalidFormat, Detail: raw}}
		return nil, p
	}
	return &v, nil
}

func inconsistent(name, detail string) *problem.Problem {
	p := problem.New(problem.CodeValidationFailed, "", "", name+"= does not apply here.")
	p.Errors = []problem.FieldError{{Parameter: name, Reason: problem.ReasonInconsistentWith, Detail: detail}}
	return p
}

func rankingUnit(by, metric string) string {
	switch metric {
	case catsvc.RankMetricVotes:
		return "votes"
	case catsvc.RankMetricSources:
		return "count"
	case catsvc.RankMetricPercentile:
		return "percentile"
	case catsvc.RankMetricDivergence:
		return "points"
	}
	if by == catsvc.RankByConsensus {
		return "deviation"
	}
	return "native"
}

func rankingList(cat *Catalog, q collect.Query, f catsvc.RankingsFilter, data catsvc.RankingsData, page int) repr.ListRanking {
	items := make([]repr.RankingEntry, 0, len(data.Items))
	for _, it := range data.Items {
		e := repr.RankingEntry{
			Object: "ranking_entry", Rank: it.Rank, Value: round2(it.Value),
			Work:      workFromListItem(it.Work, q.Include, cat.imageURL),
			PerSource: make([]repr.RankingSource, 0, len(it.Sources)),
		}
		for _, st := range it.Sources {
			e.PerSource = append(e.PerSource, repr.RankingSource{
				Source: st.Source, Score: st.Native, VoteCount: st.Votes,
				Mean: round2(st.Mean), Lower: round2(st.Lower), Upper: round2(st.Upper),
				Median: round2p(st.Median), Spread: round2p(st.Spread), Percentile: round2(st.Percentile),
			})
		}
		if c := it.Consensus; c != nil {
			e.Consensus = &repr.RankingConsensus{
				Score: round2(c.Score), Lower: round2(c.Lower), Upper: round2(c.Upper),
				Percentile: round2(c.Percentile), SourceCount: c.Sources, VoteCount: c.Votes,
				Divergence: round2p(c.Divergence),
			}
		}
		items = append(items, e)
	}
	var out repr.List[repr.RankingEntry]
	if q.Page > 0 {
		out = finishPageList(items, data.Total)
	} else {
		var next *string
		if int64(page)*int64(data.Limit) < data.Total && len(data.Items) > 0 {
			enc := collect.EncodeCursor(strconv.Itoa(page + 1))
			next = &enc
		}
		out = repr.NewList(items, next)
		if q.IncludeTotal {
			n := data.Total
			out.Total = &n
		}
	}
	if len(q.Facets) > 0 {
		out.Facets = mapSearchFacets(q.Facets, data.Facets)
	}
	order := "desc"
	if f.Asc {
		order = "asc"
	}
	return repr.ListRanking{
		List: out, By: f.By, Metric: f.Metric, Direction: order, Unit: rankingUnit(f.By, f.Metric),
		Model: ratingModelRepr(data.Model),
	}
}

func ratingModelRepr(m ratingmodel.Fitted) repr.RatingModel {
	out := repr.RatingModel{
		Object: "rating_model", ID: repr.ID(m.ID), Version: m.Params.Version,
		FittedAt: m.FittedAt.UTC(), Interval: 0.9, Sources: []repr.RatingModelSource{},
	}
	for _, spec := range ratingmodel.Sources {
		sp := m.Params.Sources[spec.Key]
		if sp == nil {
			continue
		}
		out.Sources = append(out.Sources, repr.RatingModelSource{
			Source: spec.Key, ScaleMin: spec.ScaleMin, ScaleMax: spec.ScaleMax,
			PriorVotes: round2(sp.PriorVotes), Works: sp.Works, InConsensus: sp.Calibrated,
		})
	}
	return out
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func round2p(v *float64) *float64 {
	if v == nil {
		return nil
	}
	r := round2(*v)
	return &r
}
