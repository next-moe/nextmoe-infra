package osengine

import (
	"context"
	"net/http"

	"api/internal/platform/catalog/search/spec"
)

type RankingResult struct {
	DocIDs []string
	Values []float64
	Total  int64
	Facets map[string]map[string]int64
}

func (e *Engine) SearchRankings(ctx context.Context, q spec.RankingQuery) (RankingResult, error) {
	body, dropHits := rankingSearchBody(q)
	var resp osSearchResponse
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(IndexWorks)+"/_search", body, &resp); err != nil {
		return RankingResult{}, err
	}
	out := RankingResult{Total: parseTotal(resp.Hits.Total), Facets: parseFacets(resp.Aggregations)}
	if dropHits {
		return out, nil
	}
	for _, h := range resp.Hits.Hits {
		out.DocIDs = append(out.DocIDs, h.ID)
		v := 0.0
		if len(h.Sort) > 0 {
			if f, ok := h.Sort[0].(float64); ok {
				v = f
			}
		}
		out.Values = append(out.Values, v)
	}
	return out, nil
}

func rankField(src, field string) string { return "rank." + src + "." + field }

func rankingSearchBody(q spec.RankingQuery) (map[string]any, bool) {
	page := max(q.Page, 1)
	size := q.Limit
	from := (page - 1) * size
	dropHits := false
	if from+size > maxResultWindow {
		from, dropHits = max(maxResultWindow-size, 0), true
	}
	filters := filterClauses(q.Filter)
	if len(q.Platforms) > 0 {
		filters = append(filters, termsClause("platforms", q.Platforms))
	}
	if q.Circulation != "" {
		filters = append(filters, termClause("circulation", q.Circulation))
	}
	if q.PlaytimeMin > 0 || q.PlaytimeMax > 0 {
		filters = append(filters, rangeClause("playtime", intBound(q.PlaytimeMin), intBound(q.PlaytimeMax)))
	}
	for src, n := range q.MinVotes {
		filters = append(filters, rangeClause(rankField(src, "votes"), float64(n), nil))
	}
	for src, n := range q.MaxVotes {
		filters = append(filters, rangeClause(rankField(src, "votes"), nil, float64(n)))
	}

	order := "desc"
	if q.Asc {
		order = "asc"
	}
	var primary map[string]any
	if subset := q.By == spec.RankConsensus && len(q.Sources) > 0; subset {
		filters = append(filters, subsetPresence(q.Sources, max(q.MinSources, 1)))
		params := subsetParams(q)
		if cond := subsetCondition(q); cond != "" {
			filters = append(filters, map[string]any{"script": map[string]any{"script": map[string]any{
				"source": subsetScript + cond, "params": params,
			}}})
		}
		primary = map[string]any{"_script": map[string]any{
			"type":   "number",
			"order":  order,
			"script": map[string]any{"source": subsetScript + "return v;", "params": params},
		}}
	} else {
		field := rankField(q.By, q.Field)
		filters = append(filters, map[string]any{"exists": map[string]any{"field": field}})
		if q.By == spec.RankConsensus {
			if q.MinSources > 1 {
				filters = append(filters, rangeClause(rankField(q.By, "sources"), float64(q.MinSources), nil))
			}
			if q.TotalVotes > 0 || q.TotalMax > 0 {
				filters = append(filters, rangeClause(rankField(q.By, "votes"), intBound(q.TotalVotes), intBound(q.TotalMax)))
			}
		}
		if q.Min != nil || q.Max != nil {
			filters = append(filters, rangeClause(field, floatBound(q.Min), floatBound(q.Max)))
		}
		primary = map[string]any{field: map[string]any{"order": order}}
	}
	body := map[string]any{
		"query":            map[string]any{"bool": map[string]any{"filter": filters}},
		"from":             from,
		"size":             size,
		"sort":             []any{primary, map[string]any{"popularity": map[string]any{"order": "desc"}}, map[string]any{"catalog_id": map[string]any{"order": "asc"}}},
		"track_total_hits": true,
		"_source":          false,
	}
	if len(q.FacetAttrs) > 0 {
		aggs := make(map[string]any, len(q.FacetAttrs))
		for _, attr := range q.FacetAttrs {
			aggs[attr] = map[string]any{"terms": map[string]any{"field": attr, "size": 100}}
		}
		body["aggs"] = aggs
	}
	return body, dropHits
}

func rangeClause(field string, gte, lte any) map[string]any {
	rng := map[string]any{}
	if gte != nil {
		rng["gte"] = gte
	}
	if lte != nil {
		rng["lte"] = lte
	}
	return map[string]any{"range": map[string]any{field: rng}}
}

func intBound(n int) any {
	if n <= 0 {
		return nil
	}
	return n
}

func floatBound(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func subsetPresence(sources []string, need int) map[string]any {
	should := make([]any, len(sources))
	for i, src := range sources {
		should[i] = map[string]any{"exists": map[string]any{"field": rankField(src, "num")}}
	}
	return map[string]any{"bool": map[string]any{"should": should, "minimum_should_match": need}}
}

func subsetParams(q spec.RankingQuery) map[string]any {
	num, den, votes, pct, minv := []string{}, []string{}, []string{}, []string{}, []float64{}
	for _, src := range q.Sources {
		num = append(num, rankField(src, "num"))
		den = append(den, rankField(src, "den"))
		votes = append(votes, rankField(src, "votes"))
		pct = append(pct, rankField(src, "pct"))
		minv = append(minv, q.DivergenceVotes[src])
	}
	p := map[string]any{
		"num": num, "den": den, "votes": votes, "pct": pct, "minv": minv,
		"z": q.Z, "field": q.Field, "need": max(q.MinSources, 1), "total": q.TotalVotes, "totalMax": q.TotalMax,
		"hasMin": q.Min != nil, "hasMax": q.Max != nil, "min": 0.0, "max": 0.0,
	}
	if q.Min != nil {
		p["min"] = *q.Min
	}
	if q.Max != nil {
		p["max"] = *q.Max
	}
	return p
}

func subsetCondition(q spec.RankingQuery) string {
	if q.Min == nil && q.Max == nil && q.TotalVotes <= 0 && q.TotalMax <= 0 && q.MinSources <= 1 && q.Field != "divergence" {
		return ""
	}
	return `if (n < params.need) return false;
if (params.total > 0 && votes < params.total) return false;
if (params.totalMax > 0 && votes > params.totalMax) return false;
if (params.field == 'divergence' && heard < 2) return false;
if (params.hasMin && v < params.min) return false;
if (params.hasMax && v > params.max) return false;
return true;`
}

// subsetScript leaves v set to the requested value and n, votes and heard
// in scope for the condition that may follow it.
const subsetScript = `double num = 0; double den = 0; double votes = 0; int n = 0;
double lo = 1000; double hi = -1000; int heard = 0;
for (int i = 0; i < params.num.size(); i++) {
  if (doc[params.num.get(i)].size() == 0) continue;
  num += doc[params.num.get(i)].value; den += doc[params.den.get(i)].value;
  double vc = doc[params.votes.get(i)].value; votes += vc; n++;
  if (vc >= params.minv.get(i)) { double p = doc[params.pct.get(i)].value; lo = Math.min(lo, p); hi = Math.max(hi, p); heard++; }
}
double theta = num / (1 + den);
double v = theta;
if (params.field == 'lower') v = theta - params.z / Math.sqrt(1 + den);
else if (params.field == 'upper') v = theta + params.z / Math.sqrt(1 + den);
else if (params.field == 'votes') v = votes;
else if (params.field == 'sources') v = n;
else if (params.field == 'divergence') v = heard >= 2 ? hi - lo : -1;
`
