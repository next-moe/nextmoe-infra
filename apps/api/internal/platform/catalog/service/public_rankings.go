package service

import (
	"context"
	"slices"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/ratingmodel"
	"api/internal/platform/catalog/search/spec"
)

const RankByConsensus = spec.RankConsensus

const (
	RankMetricLower      = "lower"
	RankMetricUpper      = "upper"
	RankMetricMean       = "mean"
	RankMetricMedian     = "median"
	RankMetricNative     = "native"
	RankMetricPercentile = "percentile"
	RankMetricSpread     = "spread"
	RankMetricVotes      = "votes"
	RankMetricSources    = "sources"
	RankMetricDivergence = "divergence"
)

var RankingConsensusMetrics = []string{
	RankMetricLower, RankMetricUpper, RankMetricMean, RankMetricPercentile, RankMetricDivergence, RankMetricVotes, RankMetricSources,
}

var RankingSourceMetrics = []string{
	RankMetricLower, RankMetricUpper, RankMetricMean, RankMetricMedian, RankMetricNative, RankMetricPercentile, RankMetricSpread, RankMetricVotes,
}

var rankingFacetAttr = map[string]string{
	"platform":    "platforms",
	"circulation": "circulation",
}

var RankingFacetTokens = append(append([]string{}, WorksSearchFacetTokens...), "platform", "circulation")

type RankingsFilter struct {
	Works       WorksSearchFilter
	Platforms   []string
	Circulation string
	PlaytimeMin int
	PlaytimeMax int
	By          string
	Metric      string
	Asc         bool
	Sources     []string
	MinSources  int
	MinVotes    map[string]int
	MaxVotes    map[string]int
	TotalVotes  int
	TotalMax    int
	Min         *float64
	Max         *float64
}

type RankingItem struct {
	Rank      int
	Value     float64
	Work      dto.PublicWorkListItem
	Sources   []ratingmodel.SourceStat
	Consensus *ratingmodel.Consensus
}

type RankingsData struct {
	Total  int64
	Page   int
	Limit  int
	Items  []RankingItem
	Facets map[string]map[string]int64
	Model  ratingmodel.Fitted
}

func (s *PublicService) Rankings(ctx context.Context, f RankingsFilter) (RankingsData, error) {
	if s.worksSearch == nil {
		return RankingsData{}, ErrSearchUnavailable
	}
	fitted, err := s.ratingModel.Get(ctx)
	if err != nil {
		return RankingsData{}, err
	}
	p := fitted.Params
	page, limit := max(f.Works.Page, 1), clampBrowseLimit(f.Works.Limit)
	subset := f.By == RankByConsensus && len(f.Sources) > 0
	conv := rankingScale(p, f.By, f.Metric)
	q := spec.RankingQuery{
		Filter: f.Works.worksFilter(""), Platforms: f.Platforms, Circulation: f.Circulation,
		PlaytimeMin: f.PlaytimeMin, PlaytimeMax: f.PlaytimeMax,
		By: f.By, Field: rankingField(f.By, f.Metric, subset), Asc: f.Asc,
		Sources: f.Sources, MinSources: f.MinSources, MinVotes: f.MinVotes, MaxVotes: f.MaxVotes, TotalVotes: f.TotalVotes, TotalMax: f.TotalMax,
		Z: ratingmodel.Z, DivergenceVotes: map[string]float64{},
		FacetAttrs: rankingFacetAttrs(f.Works.Facets), Page: page, Limit: limit,
	}
	for key, sp := range p.Sources {
		q.DivergenceVotes[key] = sp.DivergenceVotes()
	}
	if f.Min != nil {
		v := conv.toIndex(*f.Min)
		q.Min = &v
	}
	if f.Max != nil {
		v := conv.toIndex(*f.Max)
		q.Max = &v
	}
	res, err := s.worksSearch.SearchRankings(ctx, q)
	if err != nil {
		return RankingsData{}, err
	}
	works, err := s.hydrateWorkIDs(ctx, res.IDs, f.Works.NSFW, f.Works.Include, f.Works.Fields)
	if err != nil {
		return RankingsData{}, err
	}
	byID := make(map[int64]dto.PublicWorkListItem, len(works))
	for _, w := range works {
		byID[w.ID] = w
	}
	obs, err := ratingmodel.LoadWorks(ctx, s.db, res.IDs)
	if err != nil {
		return RankingsData{}, err
	}
	out := RankingsData{Total: res.Total, Page: page, Limit: limit, Model: fitted, Items: []RankingItem{},
		Facets: projectRankingFacets(f.Works.Facets, res.Facets)}
	for i, id := range res.IDs {
		w, ok := byID[id]
		if !ok {
			continue
		}
		ws := p.Score(obs[id])
		if subset {
			ws.Consensus = p.CombineSources(ws.Sources, f.Sources)
		}
		out.Items = append(out.Items, RankingItem{
			Rank: (page-1)*limit + i + 1, Value: conv.fromIndex(res.Values[i]),
			Work: w, Sources: ws.Sources, Consensus: ws.Consensus,
		})
	}
	return out, nil
}

func rankingField(by, metric string, subset bool) string {
	if by != RankByConsensus {
		switch metric {
		case RankMetricPercentile:
			return ratingmodel.FieldPercentile
		default:
			return metric
		}
	}
	switch metric {
	case RankMetricMean, RankMetricPercentile:
		return ratingmodel.FieldTheta
	case RankMetricLower:
		return ratingmodel.FieldLower
	case RankMetricUpper:
		return ratingmodel.FieldUpper
	default:
		return metric
	}
}

type rankScale struct {
	toIndex   func(float64) float64
	fromIndex func(float64) float64
}

// A consensus is stored and sorted in θ units; it is read and filtered as a
// deviation score, or as a percentile for metric=percentile.
func rankingScale(p *ratingmodel.Params, by, metric string) rankScale {
	identity := rankScale{toIndex: func(v float64) float64 { return v }, fromIndex: func(v float64) float64 { return v }}
	if by != RankByConsensus {
		return identity
	}
	switch metric {
	case RankMetricLower, RankMetricUpper, RankMetricMean:
		return rankScale{
			toIndex:   func(v float64) float64 { return (v - 50) / 10 },
			fromIndex: ratingmodel.DeviationScore,
		}
	case RankMetricPercentile:
		return rankScale{toIndex: p.ThetaAtPercentile, fromIndex: p.ThetaPercentile}
	}
	return identity
}

func rankingFacetAttrs(tokens []string) []string {
	out := worksSearchFacetAttrs(tokens)
	for _, t := range tokens {
		if attr, ok := rankingFacetAttr[t]; ok && !slices.Contains(out, attr) {
			out = append(out, attr)
		}
	}
	return out
}

func projectRankingFacets(tokens []string, dist map[string]map[string]int64) map[string]map[string]int64 {
	out := projectWorksSearchFacets(tokens, dist)
	for _, t := range tokens {
		attr, ok := rankingFacetAttr[t]
		if !ok {
			continue
		}
		if out == nil {
			out = map[string]map[string]int64{}
		}
		if out[t] = dist[attr]; out[t] == nil {
			out[t] = map[string]int64{}
		}
	}
	return out
}

func IsRankingFacet(tok string) bool {
	return slices.Contains(RankingFacetTokens, tok)
}
