package osengine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
	"api/pkg/config"

	"github.com/stretchr/testify/require"
)

func rankingFilters(t *testing.T, body map[string]any) []any {
	t.Helper()
	return body["query"].(map[string]any)["bool"].(map[string]any)["filter"].([]any)
}

func hasClause(t *testing.T, filters []any, want any) bool {
	t.Helper()
	w := canonJSON(t, want)
	for _, f := range filters {
		if canonJSON(t, f) == w {
			return true
		}
	}
	return false
}

var notVN = map[string]any{"bool": map[string]any{"must_not": []any{map[string]any{"term": map[string]any{"not_vn": true}}}}}

func TestRankingBodyStoredField(t *testing.T) {
	min := 60.0
	body, drop := rankingSearchBody(spec.RankingQuery{
		Filter: spec.WorksFilter{OLang: spec.OLang{All: true}},
		By:     "erogamescape", Field: "median", Min: &min,
		MinVotes:  map[string]int{"erogamescape": 50},
		Platforms: []string{"win", "swi"}, Circulation: "doujin", PlaytimeMax: 600,
		Page: 2, Limit: 10,
	})
	require.False(t, drop)
	require.Equal(t, 10, body["from"])
	filters := rankingFilters(t, body)
	for _, want := range []any{
		map[string]any{"exists": map[string]any{"field": "rank.erogamescape.median"}},
		map[string]any{"range": map[string]any{"rank.erogamescape.votes": map[string]any{"gte": 50.0}}},
		map[string]any{"range": map[string]any{"rank.erogamescape.median": map[string]any{"gte": 60.0}}},
		map[string]any{"terms": map[string]any{"platforms": []string{"win", "swi"}}},
		map[string]any{"term": map[string]any{"circulation": "doujin"}},
		map[string]any{"range": map[string]any{"playtime": map[string]any{"lte": 600}}},
		notVN,
	} {
		require.True(t, hasClause(t, filters, want), canonJSON(t, want))
	}
	sort := body["sort"].([]any)
	require.Equal(t, canonJSON(t, map[string]any{"rank.erogamescape.median": map[string]any{"order": "desc"}}), canonJSON(t, sort[0]))
	require.Equal(t, canonJSON(t, map[string]any{"catalog_id": map[string]any{"order": "asc"}}), canonJSON(t, sort[len(sort)-1]))
}

func TestRankingBodyConsensusDefault(t *testing.T) {
	body, _ := rankingSearchBody(spec.RankingQuery{
		Filter: spec.WorksFilter{OLang: spec.OLang{All: true}},
		By:     spec.RankConsensus, Field: "upper", Asc: true, MinSources: 3, TotalVotes: 100, TotalMax: 900,
		MaxVotes: map[string]int{"vndb": 40}, AllGames: true, Limit: 20,
	})
	filters := rankingFilters(t, body)
	require.False(t, hasClause(t, filters, notVN), "scope=all keeps every game")
	require.True(t, hasClause(t, filters, map[string]any{"range": map[string]any{"rank.consensus.sources": map[string]any{"gte": 3.0}}}))
	require.True(t, hasClause(t, filters, map[string]any{"range": map[string]any{"rank.consensus.votes": map[string]any{"gte": 100, "lte": 900}}}))
	require.True(t, hasClause(t, filters, map[string]any{"range": map[string]any{"rank.vndb.votes": map[string]any{"lte": 40.0}}}))
	require.Equal(t, canonJSON(t, map[string]any{"rank.consensus.upper": map[string]any{"order": "asc"}}), canonJSON(t, body["sort"].([]any)[0]))
}

func TestRankingBodyConsensusSubsetIsScripted(t *testing.T) {
	max := 1.5
	body, _ := rankingSearchBody(spec.RankingQuery{
		Filter: spec.WorksFilter{OLang: spec.OLang{All: true}},
		By:     spec.RankConsensus, Field: "lower", Sources: []string{"erogamescape", "bangumi"}, MinSources: 2,
		Max: &max, Z: 1.64, DivergenceVotes: map[string]float64{"erogamescape": 8, "bangumi": 6}, Limit: 20,
	})
	filters := rankingFilters(t, body)
	require.True(t, hasClause(t, filters, map[string]any{"bool": map[string]any{
		"should": []any{
			map[string]any{"exists": map[string]any{"field": "rank.erogamescape.num"}},
			map[string]any{"exists": map[string]any{"field": "rank.bangumi.num"}},
		},
		"minimum_should_match": 2,
	}}))
	var scripted bool
	for _, f := range filters {
		if s, ok := f.(map[string]any)["script"]; ok {
			src := s.(map[string]any)["script"].(map[string]any)["source"].(string)
			require.Contains(t, src, "params.hasMax && v > params.max")
			scripted = true
		}
	}
	require.True(t, scripted, "a bound on a subset consensus is a script filter")
	primary := body["sort"].([]any)[0].(map[string]any)["_script"].(map[string]any)
	require.Equal(t, "desc", primary["order"])
	params := primary["script"].(map[string]any)["params"].(map[string]any)
	require.Equal(t, []string{"rank.erogamescape.num", "rank.bangumi.num"}, params["num"])
	require.Equal(t, []float64{8, 6}, params["minv"])
	require.True(t, strings.HasSuffix(primary["script"].(map[string]any)["source"].(string), "return v;"))
}

func TestSearchRankingsReadsSortValues(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		require.NoError(t, json.Unmarshal(raw, &body))
		require.Equal(t, false, body["_source"])
		_, _ = w.Write([]byte(`{"hits":{"total":{"value":2},"hits":[
			{"_id":"w7","sort":[1.5,3.2,7]},{"_id":"w9","sort":[0.25,1.0,9]}]}}`))
	}))
	defer srv.Close()
	client, err := opensearch.NewClient(config.OpenSearchConfig{Host: srv.URL})
	require.NoError(t, err)
	res, err := NewEngine(client).SearchRankings(t.Context(), spec.RankingQuery{By: "vndb", Field: "mean", Limit: 2})
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Total)
	require.Equal(t, []string{"w7", "w9"}, res.DocIDs)
	require.Equal(t, []float64{1.5, 0.25}, res.Values)
}
