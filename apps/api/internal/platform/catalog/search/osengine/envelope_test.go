package osengine

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
	"api/pkg/config"

	"github.com/stretchr/testify/require"
)

func int16ptr(v int16) *int16 { return &v }
func boolptr(v bool) *bool    { return &v }

func defaultOLangClause() any {
	return map[string]any{
		"bool": map[string]any{
			"should": []any{
				map[string]any{"term": map[string]any{"olang": "ja"}},
				map[string]any{"prefix": map[string]any{"olang": "zh"}},
			},
			"minimum_should_match": 1,
		},
	}
}

func TestFilterClausesEmpty(t *testing.T) {
	require.Equal(t, canonJSON(t, []any{defaultOLangClause()}), canonJSON(t, filterClauses(spec.WorksFilter{})))
}

func TestFilterClausesOLangAll(t *testing.T) {
	require.Nil(t, filterClauses(spec.WorksFilter{OLang: spec.OLang{All: true}}))
}

func TestFilterClausesOrder(t *testing.T) {
	got := filterClauses(spec.WorksFilter{
		DocID:            "w15",
		ContentRatingNot: int16ptr(2),
		ContentRating:    int16ptr(1),
		Claimed:          boolptr(true),
		ClaimStates:      []string{"live", "draft"},
		ClaimStateNot:    "hidden",
		DisplayLimits:    []string{"sfw"},
		TagIDs:           []int64{3, 7},
		LabelID:          4,
		EngineID:         5,
		SeriesID:         6,
		ReleasedAfter:    100,
		ReleasedBefore:   200,
		OLang:            spec.OLang{Values: []string{"ja", "zh"}},
	})
	want := []any{
		map[string]any{"term": map[string]any{"id": "w15"}},
		map[string]any{"bool": map[string]any{"must_not": map[string]any{"term": map[string]any{"content_rating": int16(2)}}}},
		map[string]any{"term": map[string]any{"content_rating": int16(1)}},
		map[string]any{"term": map[string]any{"claimed": true}},
		map[string]any{"terms": map[string]any{"claim_state": []string{"live", "draft"}}},
		map[string]any{"bool": map[string]any{"must_not": map[string]any{"term": map[string]any{"claim_state": "hidden"}}}},
		map[string]any{"terms": map[string]any{"content_limit": []string{"sfw"}}},
		map[string]any{"term": map[string]any{"tag_ids": int64(3)}},
		map[string]any{"term": map[string]any{"tag_ids": int64(7)}},
		map[string]any{"term": map[string]any{"label_ids": int64(4)}},
		map[string]any{"term": map[string]any{"engine_ids": int64(5)}},
		map[string]any{"term": map[string]any{"series_ids": int64(6)}},
		map[string]any{"range": map[string]any{"released_ord": map[string]any{"gte": int64(100), "lte": int64(200)}}},
		map[string]any{"terms": map[string]any{"olang": []string{"ja", "zh"}}},
	}
	require.Equal(t, canonJSON(t, want), canonJSON(t, got))
}

func TestFilterClausesMergedRange(t *testing.T) {
	onlyAfter := filterClauses(spec.WorksFilter{ReleasedAfter: 10, OLang: spec.OLang{All: true}})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"range": map[string]any{"released_ord": map[string]any{"gte": int64(10)}}},
	}), canonJSON(t, onlyAfter))

	onlyBefore := filterClauses(spec.WorksFilter{ReleasedBefore: 20, OLang: spec.OLang{All: true}})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"range": map[string]any{"released_ord": map[string]any{"lte": int64(20)}}},
	}), canonJSON(t, onlyBefore))

	require.Nil(t, filterClauses(spec.WorksFilter{LabelID: 0, EngineID: -1, SeriesID: 0, OLang: spec.OLang{All: true}}))
}

func TestWorksSearchBodyMatchAll(t *testing.T) {
	body, drop := worksSearchBody(spec.WorksQuery{Limit: 8, Filter: spec.WorksFilter{OLang: spec.OLang{All: true}}})
	require.False(t, drop)
	require.Equal(t, canonJSON(t, map[string]any{"match_all": map[string]any{}}), canonJSON(t, body["query"]))
	require.Equal(t, 0, body["from"])
	require.Equal(t, 8, body["size"])
	require.Equal(t, true, body["track_total_hits"])
	require.Equal(t, false, body["_source"])
	require.Nil(t, body["aggs"])
	_, hasMinScore := body["min_score"]
	require.False(t, hasMinScore)
	require.Equal(t, canonJSON(t, []any{map[string]any{"popularity": map[string]any{"order": "desc"}}}), canonJSON(t, body["sort"]))
}

func TestWorksSearchBodyFilterPlacement(t *testing.T) {
	f := spec.WorksFilter{DocID: "w1"}

	withText, _ := worksSearchBody(spec.WorksQuery{Q: "CLANNAD", Filter: f, Limit: 8, Page: 1})
	q, ok := withText["query"].(map[string]any)["function_score"].(map[string]any)["query"].(map[string]any)
	require.True(t, ok)
	bq, ok := q["bool"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, 1, bq["minimum_should_match"])
	require.NotNil(t, bq["should"])
	require.Equal(t, canonJSON(t, filterClauses(f)), canonJSON(t, bq["filter"]))

	emptyText, _ := worksSearchBody(spec.WorksQuery{Filter: f, Limit: 8})
	require.Equal(t, canonJSON(t, map[string]any{
		"bool": map[string]any{"filter": filterClauses(f)},
	}), canonJSON(t, emptyText["query"]))
}

func TestWorksSearchBodyPagination(t *testing.T) {
	page2, drop := worksSearchBody(spec.WorksQuery{Page: 2, Limit: 10})
	require.False(t, drop)
	require.Equal(t, 10, page2["from"])
	require.Equal(t, 10, page2["size"])

	page0, drop := worksSearchBody(spec.WorksQuery{Page: 0, Limit: 20})
	require.False(t, drop)
	require.Equal(t, 0, page0["from"])
	require.Equal(t, 20, page0["size"])

	clamped, drop := worksSearchBody(spec.WorksQuery{Page: 25001, Limit: 20})
	require.True(t, drop)
	require.Equal(t, 499980, clamped["from"])
	require.Equal(t, 20, clamped["size"])
}

func TestWorksSearchBodySortLanes(t *testing.T) {
	released, _ := worksSearchBody(spec.WorksQuery{Q: "x", SortLane: "released_ord:desc", Limit: 8})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"released_ord": map[string]any{"order": "desc"}},
		map[string]any{"popularity": map[string]any{"order": "desc"}},
	}), canonJSON(t, released["sort"]))

	asc, _ := worksSearchBody(spec.WorksQuery{SortLane: "released_ord:asc", Limit: 8})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"released_ord": map[string]any{"order": "asc"}},
		map[string]any{"popularity": map[string]any{"order": "desc"}},
	}), canonJSON(t, asc["sort"]))

	updated, _ := worksSearchBody(spec.WorksQuery{SortLane: "updated_ts:desc", Limit: 8})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"updated_ts": map[string]any{"order": "desc"}},
		map[string]any{"popularity": map[string]any{"order": "desc"}},
	}), canonJSON(t, updated["sort"]))

	pop, _ := worksSearchBody(spec.WorksQuery{SortLane: "popularity:desc", Limit: 8})
	require.Equal(t, canonJSON(t, []any{
		map[string]any{"popularity": map[string]any{"order": "desc"}},
	}), canonJSON(t, pop["sort"]))

	relevance, _ := worksSearchBody(spec.WorksQuery{Q: "CLANNAD", Limit: 8})
	require.Equal(t, canonJSON(t, []any{
		"_score",
		map[string]any{"popularity": map[string]any{"order": "desc"}},
	}), canonJSON(t, relevance["sort"]))

	for _, src := range spec.RatingSources {
		rated, _ := worksSearchBody(spec.WorksQuery{Q: "x", SortLane: "rating." + src + ":desc", Limit: 8})
		require.Equal(t, canonJSON(t, []any{
			map[string]any{"rating." + src: map[string]any{"order": "desc", "missing": "_last"}},
			map[string]any{"catalog_id": map[string]any{"order": "asc"}},
		}), canonJSON(t, rated["sort"]), src)
	}
	for _, lane := range []string{"rating.howlongtobeat:desc", "rating:desc", "rating.:desc"} {
		_, _, ok := parseSortLane(lane)
		require.False(t, ok, lane)
	}
}

func TestEntitySearchBodySexualNot(t *testing.T) {
	body, drop := entitySearchBody(IndexCharacters, spec.EntityQuery{Limit: 10, SexualNot: boolptr(true)})
	require.False(t, drop)
	bq := body["query"].(map[string]any)["bool"].(map[string]any)
	require.NotNil(t, bq["filter"])
	require.Equal(t, canonJSON(t, []any{mustNotTerm("sexual", true)}), canonJSON(t, bq["filter"]))

	plain, _ := entitySearchBody(IndexCharacters, spec.EntityQuery{Limit: 10})
	_, hasBool := plain["query"].(map[string]any)["bool"]
	require.False(t, hasBool, "no sexual filter when SexualNot is unset: %v", plain["query"])
}

func TestEntitySearchBodyPopularityRanking(t *testing.T) {
	exact := map[string]any{"constant_score": map[string]any{
		"filter": map[string]any{"term": map[string]any{"titles_kw": "ゆずソフト"}},
		"boost":  exactNameTier,
	}}
	want := map[string]any{"function_score": map[string]any{
		"query": map[string]any{"bool": map[string]any{
			"should":               append(titleShould("ゆずソフト", false, true), exact),
			"minimum_should_match": 1,
		}},
		"functions": []any{
			map[string]any{"weight": 1},
			map[string]any{"field_value_factor": map[string]any{"field": "popularity", "missing": 0}},
		},
		"score_mode": "sum",
		"boost_mode": "multiply",
	}}
	for _, uid := range []string{IndexLabels, IndexEngines, IndexSeries} {
		body, _ := entitySearchBody(uid, spec.EntityQuery{Q: "ゆずソフト", Limit: 10})
		require.Equal(t, canonJSON(t, want), canonJSON(t, body["query"]), uid)
		require.Equal(t, canonJSON(t, searchSort("", true)), canonJSON(t, body["sort"]), uid)

		browse, _ := entitySearchBody(uid, spec.EntityQuery{Limit: 10})
		require.Equal(t, canonJSON(t, map[string]any{"match_all": map[string]any{}}), canonJSON(t, browse["query"]), uid)
	}
	for _, uid := range []string{IndexCreditNames, IndexCharacters, IndexTags, IndexTraits} {
		body, _ := entitySearchBody(uid, spec.EntityQuery{Q: "ゆずソフト", Limit: 10})
		require.Equal(t, canonJSON(t, buildQuery("ゆずソフト", false, true, nil)), canonJSON(t, body["query"]), uid)
	}
	require.Len(t, popularityRanked, 3)

	punct, _ := entitySearchBody(IndexLabels, spec.EntityQuery{Q: "★", Limit: 10})
	should := punct["query"].(map[string]any)["function_score"].(map[string]any)["query"].(map[string]any)["bool"].(map[string]any)["should"]
	require.NotContains(t, canonJSON(t, should), "constant_score", "no exact tier without a compact keyword")
}

func TestEntitySearchBodyPagination(t *testing.T) {
	page2, drop := entitySearchBody(IndexCharacters, spec.EntityQuery{Page: 2, Limit: 10})
	require.False(t, drop)
	require.Equal(t, 10, page2["from"])
	require.Equal(t, 10, page2["size"])

	page0, drop := entitySearchBody(IndexCharacters, spec.EntityQuery{Page: 0, Limit: 20})
	require.False(t, drop)
	require.Equal(t, 0, page0["from"])
	require.Equal(t, 20, page0["size"])

	clamped, drop := entitySearchBody(IndexCharacters, spec.EntityQuery{Page: 25001, Limit: 20})
	require.True(t, drop)
	require.Equal(t, 499980, clamped["from"])
	require.Equal(t, 20, clamped["size"])
}

func TestWorksSearchBodyAggs(t *testing.T) {
	body, _ := worksSearchBody(spec.WorksQuery{
		Limit:      8,
		FacetAttrs: []string{"content_rating", "olang", "claimed"},
	})
	require.Equal(t, canonJSON(t, map[string]any{
		"content_rating": map[string]any{"terms": map[string]any{"field": "content_rating", "size": 100}},
		"olang":          map[string]any{"terms": map[string]any{"field": "olang", "size": 100}},
		"claimed":        map[string]any{"terms": map[string]any{"field": "claimed", "size": 100}},
	}), canonJSON(t, body["aggs"]))
}

func TestWorksSearchBodyUsesTitleShouldNotWorksBody(t *testing.T) {
	canon := WorksBody("千原万神", 8, false)
	body, _ := worksSearchBody(spec.WorksQuery{Q: "千原万神", Limit: 8, Page: 1, Filter: spec.WorksFilter{DocID: "w1"}})
	fs := body["query"].(map[string]any)["function_score"].(map[string]any)
	require.Equal(t, "multiply", fs["boost_mode"], "relevance blends popularity into the score")
	bq := fs["query"].(map[string]any)["bool"].(map[string]any)
	canonShould := canon["query"].(map[string]any)["bool"].(map[string]any)["should"].([]any)
	wantShould := append(append([]any{}, canonShould...), exactTitleClause(CompactKeyword("千原万神"), worksExactTitleTier))
	require.Equal(t, canonJSON(t, wantShould), canonJSON(t, bq["should"]))
	require.NotNil(t, bq["filter"])

	sorted, _ := worksSearchBody(spec.WorksQuery{Q: "千原万神", Limit: 8, Page: 1, SortLane: "popularity:desc"})
	_, boosted := sorted["query"].(map[string]any)["function_score"]
	require.False(t, boosted, "an explicit sort lane is not a relevance query")
	_, hasFrom := canon["from"]
	require.False(t, hasFrom)
	require.Equal(t, 0, body["from"])
	require.Equal(t, false, body["_source"])
}

func testEngine(t *testing.T, h http.HandlerFunc) *Engine {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cli, err := opensearch.NewClient(config.OpenSearchConfig{Host: srv.URL})
	require.NoError(t, err)
	return NewEngine(cli)
}

func TestSearchWorksHitsAndFacets(t *testing.T) {
	var gotBody map[string]any
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/catalog_works/_search", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"hits": {
				"total": {"value": 2, "relation": "eq"},
				"hits": [{"_id": "w1"}, {"_id": "w2"}]
			},
			"aggregations": {
				"claimed": {"buckets": [
					{"key": 1, "key_as_string": "true", "doc_count": 5},
					{"key": false, "doc_count": 3}
				]},
				"content_rating": {"buckets": [{"key": 0, "doc_count": 10}]}
			}
		}`)
	})
	res, err := eng.SearchWorks(t.Context(), spec.WorksQuery{
		Q:          "CLANNAD",
		Limit:      8,
		Page:       1,
		FacetAttrs: []string{"claimed", "content_rating"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"w1", "w2"}, res.DocIDs)
	require.Equal(t, int64(2), res.Total)
	require.Equal(t, int64(5), res.Facets["claimed"]["true"])
	require.Equal(t, int64(3), res.Facets["claimed"]["false"])
	require.Equal(t, int64(10), res.Facets["content_rating"]["0"])
	require.Equal(t, false, gotBody["_source"])
	require.Equal(t, float64(0), gotBody["from"])
	require.Equal(t, float64(8), gotBody["size"])
}

func TestSearchWorksAbsurdPageEmptyDocIDs(t *testing.T) {
	var from any
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		from = body["from"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"hits": {
				"total": {"value": 99, "relation": "eq"},
				"hits": [{"_id": "w9"}]
			},
			"aggregations": {
				"olang": {"buckets": [{"key": "ja", "doc_count": 4}]}
			}
		}`)
	})
	res, err := eng.SearchWorks(t.Context(), spec.WorksQuery{
		Page:       25001,
		Limit:      20,
		FacetAttrs: []string{"olang"},
	})
	require.NoError(t, err)
	require.Equal(t, float64(499980), from)
	require.Empty(t, res.DocIDs)
	require.Equal(t, int64(99), res.Total)
	require.Equal(t, int64(4), res.Facets["olang"]["ja"])
}

func TestSearchEntitiesKeepsSource(t *testing.T) {
	var gotBody map[string]any
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/catalog_characters/_search", r.URL.Path)
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(raw, &gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"hits": {
				"total": {"value": 1, "relation": "eq"},
				"hits": [{"_id": "c1", "_source": {"id": "c1", "name_ja": "遠野秋葉"}}]
			}
		}`)
	})
	cr := int16(2)
	res, err := eng.SearchEntities(t.Context(), IndexCharacters, spec.EntityQuery{
		Q:                "遠野秋葉",
		Limit:            8,
		ContentRatingNot: &cr,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), res.Total)
	require.Len(t, res.Hits, 1)
	require.Contains(t, string(res.Hits[0]), "遠野秋葉")
	_, hasSource := gotBody["_source"]
	require.False(t, hasSource)
	bq := gotBody["query"].(map[string]any)["bool"].(map[string]any)
	require.NotNil(t, bq["filter"])
	require.NotNil(t, bq["should"])
	require.Equal(t, float64(0), gotBody["from"])
	require.Equal(t, float64(8), gotBody["size"])
}

func TestSearchEntitiesAbsurdPageEmptyHits(t *testing.T) {
	var from any
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		from = body["from"]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"hits": {
				"total": {"value": 99, "relation": "eq"},
				"hits": [{"_id": "c9", "_source": {"id": "c9"}}]
			}
		}`)
	})
	res, err := eng.SearchEntities(t.Context(), IndexCharacters, spec.EntityQuery{
		Page:  25001,
		Limit: 20,
	})
	require.NoError(t, err)
	require.Equal(t, float64(499980), from)
	require.Empty(t, res.Hits)
	require.Equal(t, int64(99), res.Total)
}

func TestCheckPluginsNamesMissing(t *testing.T) {
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/_cat/plugins", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[{"name":"n1","component":"analysis-icu","version":"2.19.6"}]`)
	})
	err := eng.CheckPlugins(t.Context())
	require.Error(t, err)
	require.Contains(t, err.Error(), "analysis-kuromoji")
	require.Contains(t, err.Error(), "analysis-pinyin")
}

func TestAPIErrorCapsBody(t *testing.T) {
	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'x'
	}
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write(long)
	})
	_, err := eng.Count(t.Context(), IndexWorks)
	require.Error(t, err)
	var ae *opensearch.APIError
	require.ErrorAs(t, err, &ae)
	require.Equal(t, http.StatusBadRequest, ae.Status)
	require.Len(t, ae.Body, 2048)
}

func TestBulkNDJSONSurfacesFirstItemError(t *testing.T) {
	eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/_bulk", r.URL.Path)
		require.Equal(t, "application/x-ndjson", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"errors": true,
			"items": [
				{"index": {"status": 400, "error": {"type": "mapper_parsing_exception", "reason": "failed"}}},
				{"index": {"status": 201}}
			]
		}`)
	})
	err := eng.Upsert(t.Context(), IndexWorks, []BulkDoc{{ID: "w1", Source: map[string]any{"id": "w1"}}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mapper_parsing_exception")
}

func TestEnsureIndexesCreateAndSchemaMismatch(t *testing.T) {
	t.Run("create on 404", func(t *testing.T) {
		var putPath string
		eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":"no such index"}`)
			case http.MethodDelete:
				w.WriteHeader(http.StatusNotFound)
				_, _ = io.WriteString(w, `{"error":"no such index"}`)
			case http.MethodPut:
				putPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"acknowledged":true}`)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})
		require.NoError(t, eng.EnsureIndexes(t.Context()))
		require.NoError(t, eng.RecreateIndex(t.Context(), IndexWorks))
		require.Equal(t, "/catalog_works", putPath)
	})
	t.Run("mismatch names recreate", func(t *testing.T) {
		eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{
				"catalog_works": {
					"mappings": {"_meta": {"schema_version": 1}}
				}
			}`)
		})
		err := eng.ensureIndex(t.Context(), IndexWorks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reindex-catalog -recreate")
	})
	t.Run("create race already exists", func(t *testing.T) {
		var gets int
		eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				gets++
				if gets == 1 {
					w.WriteHeader(http.StatusNotFound)
					_, _ = io.WriteString(w, `{"error":{"type":"index_not_found_exception"}}`)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{
					"catalog_works": {
						"mappings": {"_meta": {"schema_version": 5}}
					}
				}`)
			case http.MethodPut:
				if r.URL.Path == "/catalog_works/_mapping" {
					_, _ = io.WriteString(w, `{"acknowledged":true}`)
					return
				}
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":{"type":"resource_already_exists_exception","reason":"index [catalog_works] already exists"}}`)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})
		require.NoError(t, eng.ensureIndex(t.Context(), IndexWorks))
		require.Equal(t, 2, gets)
	})
	t.Run("existing index takes new fields in place", func(t *testing.T) {
		var calls []string
		var mapping map[string]any
		eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
			calls = append(calls, r.Method+" "+r.URL.Path)
			switch r.Method {
			case http.MethodGet:
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"catalog_works": {"mappings": {"_meta": {"schema_version": 5}}}}`)
			case http.MethodPut:
				require.NoError(t, json.NewDecoder(r.Body).Decode(&mapping))
				_, _ = io.WriteString(w, `{"acknowledged":true}`)
			default:
				w.WriteHeader(http.StatusMethodNotAllowed)
			}
		})
		require.NoError(t, eng.ensureIndex(t.Context(), IndexWorks))
		require.Equal(t, []string{"GET /catalog_works", "PUT /catalog_works/_mapping"}, calls)
		want, err := propertiesFor(IndexWorks)
		require.NoError(t, err)
		require.Equal(t, canonJSON(t, map[string]any{"properties": want}), canonJSON(t, mapping))
	})
	t.Run("refused mapping names recreate", func(t *testing.T) {
		eng := testEngine(t, func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"catalog_works": {"mappings": {"_meta": {"schema_version": 5}}}}`)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"type":"illegal_argument_exception","reason":"mapper [popularity] cannot be changed from type [float] to [long]"}}`)
		})
		err := eng.ensureIndex(t.Context(), IndexWorks)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reindex-catalog -recreate")
	})
}

func TestFilterClausesExcludeTags(t *testing.T) {
	got := filterClauses(spec.WorksFilter{TagIDs: []int64{3}, ExcludeTagIDs: []int64{7, 9}, OLang: spec.OLang{All: true}})
	require.Equal(t, canonJSON(t, []any{
		termClause("tag_ids", int64(3)),
		map[string]any{"bool": map[string]any{"must_not": map[string]any{"terms": map[string]any{"tag_ids": []int64{7, 9}}}}},
	}), canonJSON(t, got))
	require.Nil(t, filterClauses(spec.WorksFilter{ExcludeTagIDs: []int64{}, OLang: spec.OLang{All: true}}))
}

func TestEntitySearchBodyRanksWorksLikeTheWorksLane(t *testing.T) {
	body, _ := entitySearchBody(IndexWorks, spec.EntityQuery{Q: "clan", Limit: 8})
	require.Equal(t, canonJSON(t, worksRelevanceQuery("clan", false, nil)), canonJSON(t, body["query"]),
		"object=work on the search face must rank like works?q=")

	tags, _ := entitySearchBody(IndexTags, spec.EntityQuery{Q: "clan", Limit: 8})
	_, boosted := tags["query"].(map[string]any)["function_score"]
	require.False(t, boosted, "other families keep their own ranking")
}
