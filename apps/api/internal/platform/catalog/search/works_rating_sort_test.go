package search

import (
	"net/http"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/search/osengine"
	"api/internal/platform/catalog/search/spec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRatingSortScore(t *testing.T) {
	c := float64(model.RatingPriorVotes)
	assert.InDelta(t, (c*6+1*10)/(c+1), RatingSortScore("bangumi", 10, 1, 6), 1e-9)
	assert.InDelta(t, (c*62+500*90)/(c+500), RatingSortScore("erogamescape", 90, 500, 62), 1e-9)
	assert.InDelta(t, 4.1, RatingSortScore("dlsite", 5, 0, 4.1), 1e-9, "no votes is the source mean")
	assert.Less(t, RatingSortScore("bangumi", 10, 2, 6), RatingSortScore("bangumi", 8.5, 400, 6),
		"two perfect votes must not outrank four hundred votes at 8.5")
	assert.Equal(t, 7.25, RatingSortScore("vndb", 7.25, 3, 6.4), "vndb publishes its own Bayesian rating")
	assert.Equal(t, 8.1, RatingSortScore("nextmoe", 8.1, 2, 7.6), "the central aggregate is already shrunk")
}

func TestBuildWorkDocCarriesSortKeys(t *testing.T) {
	d := BuildWorkDoc(WorkDocInput{ID: 42, DisplayName: "x", Ratings: map[string]float64{"bangumi": 7.5}})
	assert.EqualValues(t, 42, d.CatalogID)
	assert.Equal(t, map[string]float64{"bangumi": 7.5}, d.Rating)
	bare := BuildWorkDoc(WorkDocInput{ID: 43, DisplayName: "y"})
	assert.NotContains(t, mustJSON(t, bare), `"rating"`, "an unrated work must have no rating object, so it sorts as missing")
}

func TestSearchWorksRatingSort(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexWorks)
	doc := func(id int64, pop float64, tags []int64, ratings map[string]float64) EntityDoc {
		return BuildWorkDoc(WorkDocInput{ID: id, DisplayName: "作品", Popularity: pop, TagIDs: tags, Ratings: ratings})
	}
	putDocs(t, IndexWorks, []EntityDoc{
		doc(1, 1, []int64{7}, map[string]float64{"bangumi": 7.0}),
		doc(2, 2, []int64{7}, map[string]float64{"bangumi": 8.0, "vndb": 6.0}),
		doc(3, 1, []int64{7}, nil),
		doc(4, 50, nil, map[string]float64{"bangumi": 7.0}),
		doc(5, 100, []int64{7}, map[string]float64{"vndb": 7.5}),
	})
	ids := func(lane string, tags []int64, page, limit int) []int64 {
		t.Helper()
		res, err := idx.SearchWorks(t.Context(), spec.WorksQuery{
			Filter:   spec.WorksFilter{TagIDs: tags, OLang: spec.OLang{All: true}},
			SortLane: lane, Page: page, Limit: limit,
		})
		require.NoError(t, err)
		return res.IDs
	}

	assert.Equal(t, []int64{2, 1, 4, 3, 5}, ids("rating.bangumi:desc", nil, 1, 10),
		"highest first, the 7.0 tie by id, unrated last by id and not by popularity")
	assert.Equal(t, []int64{5, 2, 1, 3, 4}, ids("rating.vndb:desc", nil, 1, 10), "each source ranks alone")
	assert.Equal(t, []int64{2, 1, 3, 5}, ids("rating.bangumi:desc", []int64{7}, 1, 10), "filters still apply")

	var paged []int64
	for page := 1; page <= 3; page++ {
		paged = append(paged, ids("rating.bangumi:desc", nil, page, 2)...)
	}
	assert.Equal(t, []int64{2, 1, 4, 3, 5}, paged, "pages tile the order with no repeats or gaps")
}

func TestEnsureIndexesAddsWorkSortFieldsInPlace(t *testing.T) {
	body, err := osengine.IndexBody(IndexWorks)
	require.NoError(t, err)
	props := body["mappings"].(map[string]any)["properties"].(map[string]any)
	delete(props, "rating")
	delete(props, "catalog_id")
	name := testOS.IndexName(IndexWorks)
	_ = testOS.Do(t.Context(), http.MethodDelete, "/"+name, nil, nil)
	require.NoError(t, testOS.Do(t.Context(), http.MethodPut, "/"+name, body, nil))
	t.Cleanup(func() { _ = testOS.Do(t.Context(), http.MethodDelete, "/"+name, nil, nil) })
	putDocs(t, IndexWorks, []EntityDoc{
		BuildWorkDoc(WorkDocInput{ID: 1, DisplayName: "甲", Ratings: map[string]float64{"dlsite": 4.2}}),
		BuildWorkDoc(WorkDocInput{ID: 2, DisplayName: "乙", Ratings: map[string]float64{"dlsite": 4.8}}),
	})

	require.NoError(t, testIdx.EnsureIndexes(t.Context()))

	n, err := testIdx.Count(t.Context(), IndexWorks)
	require.NoError(t, err)
	assert.EqualValues(t, 2, n, "the fields are added in place, not by dropping the index")
	var got map[string]struct {
		Mappings struct {
			Properties map[string]map[string]any `json:"properties"`
		} `json:"mappings"`
	}
	require.NoError(t, testOS.Do(t.Context(), http.MethodGet, "/"+name+"/_mapping", nil, &got))
	assert.Contains(t, got[name].Mappings.Properties, "rating")
	assert.Contains(t, got[name].Mappings.Properties, "catalog_id")

	putDocs(t, IndexWorks, []EntityDoc{
		BuildWorkDoc(WorkDocInput{ID: 1, DisplayName: "甲", Ratings: map[string]float64{"dlsite": 4.2}}),
		BuildWorkDoc(WorkDocInput{ID: 2, DisplayName: "乙", Ratings: map[string]float64{"dlsite": 4.8}}),
	})
	res, err := testIdx.SearchWorks(t.Context(), spec.WorksQuery{
		Filter: spec.WorksFilter{OLang: spec.OLang{All: true}}, SortLane: "rating.dlsite:desc", Page: 1, Limit: 10,
	})
	require.NoError(t, err)
	assert.Equal(t, []int64{2, 1}, res.IDs, "a plain reindex fills the new fields")
}
