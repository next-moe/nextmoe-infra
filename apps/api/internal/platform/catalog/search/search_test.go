package search

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testOS *opensearch.Client

var testIdx *Indexer

var testPrefix = dbtest.SearchIndexPrefix("search")

func TestMain(m *testing.M) {
	host := dbtest.SearchHost()
	if host == "" {
		dbtest.SkipSearchMain("catalog/search", "KUN_OPENSEARCH_TEST_HOST unset")
	}
	client, err := opensearch.NewClient(config.OpenSearchConfig{
		Host: host, IndexPrefix: testPrefix,
	})
	if err != nil {
		dbtest.SkipSearchMain("catalog/search", "opensearch client: %v", err)
	}
	if err := client.Health(context.Background()); err != nil {
		dbtest.SkipSearchMain("catalog/search", "opensearch unreachable: %v", err)
	}
	testOS = client
	testIdx = NewOpenSearchIndexer(client)
	code := m.Run()
	dbtest.SweepSearchIndexes(client, testPrefix)
	os.Exit(code)
}

func ensureSearchIndexes(t *testing.T, uids ...string) *Indexer {
	t.Helper()
	require.NoError(t, testIdx.EnsureIndexes(t.Context()))
	t.Cleanup(func() {
		for _, uid := range uids {
			_ = testOS.Do(context.Background(), http.MethodDelete, "/"+testOS.IndexName(uid), nil, nil)
		}
	})
	return testIdx
}

func putDocs(t *testing.T, uid string, docs []EntityDoc) {
	t.Helper()
	require.NoError(t, testIdx.UpsertBatch(t.Context(), uid, docs))
	require.NoError(t, testIdx.Refresh(t.Context(), uid))
}

func TestWorksNSFWFilter(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexWorks)

	r18, safe := int16(2), int16(0)
	docA := EntityDoc{ID: "w1", EntityType: "work", ContentRating: &r18, Popularity: 3}
	docA.SetNameOrAlias("ja", "いろとりどりのセカイ")
	docA.SetNameOrAlias("zh", "五彩斑斓的世界")
	docB := EntityDoc{ID: "w2", EntityType: "work", ContentRating: &safe, Popularity: 1}
	docB.SetNameOrAlias("ja", "全年龄作品")
	putDocs(t, IndexWorks, []EntityDoc{docA, docB})

	ctx := t.Context()
	res, err := idx.SearchEntities(ctx, IndexWorks, spec.EntityQuery{Q: "いろとりどり", Locales: []string{"jpn"}, Limit: 20, ContentRatingNot: &r18})
	require.NoError(t, err)
	assert.Len(t, res.Hits, 0, "r18 filtered")
	res, err = idx.SearchEntities(ctx, IndexWorks, spec.EntityQuery{Q: "五彩斑斓", Locales: []string{"cmn"}, Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "w1", res.Hits[0].ID)
		require.NotNil(t, res.Hits[0].ContentRating)
		assert.EqualValues(t, 2, *res.Hits[0].ContentRating)
	}
	res, err = idx.SearchEntities(ctx, IndexWorks, spec.EntityQuery{Q: "", Limit: 20, ContentRatingNot: &r18})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "w2", res.Hits[0].ID)
	}
}

func TestLabelAliasIsSearchable(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexLabels)

	kind := int16(4)
	doc := EntityDoc{ID: "b5147", EntityType: "label", Kind: &kind}
	doc.SetName("ja", "ゆずソフト")
	for _, a := range []string{"Yuzu-Soft", "Yuzusoft", "ユズソフト"} {
		doc.AddAlias("en", a)
	}
	doc.AddAlias("zh", "柚子社")
	putDocs(t, IndexLabels, []EntityDoc{doc})

	for _, q := range []string{"Yuzusoft", "yuzusoft", "柚子社", "ゆずソフト"} {
		res, err := idx.SearchEntities(t.Context(), IndexLabels, spec.EntityQuery{Q: q, Limit: 20})
		require.NoError(t, err, q)
		if assert.Len(t, res.Hits, 1, "query %q found nothing", q) {
			assert.Equal(t, "b5147", res.Hits[0].ID, q)
		}
	}
}

func TestDeleteBatchRemovesTombstones(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexLabels)

	live := EntityDoc{ID: "b5147", EntityType: "label"}
	live.SetName("ja", "ゆずソフト")
	dead := EntityDoc{ID: "b96", EntityType: "label"}
	dead.SetName("ja", "ゆずソフト")
	putDocs(t, IndexLabels, []EntityDoc{live, dead})

	ctx := t.Context()
	res, err := idx.SearchEntities(ctx, IndexLabels, spec.EntityQuery{Q: "ゆずソフト", Locales: []string{"jpn"}, Limit: 20})
	require.NoError(t, err)
	require.Len(t, res.Hits, 2, "both are indexed before the purge")

	require.NoError(t, idx.DeleteBatch(ctx, IndexLabels, []string{"b96"}))
	require.NoError(t, idx.Refresh(ctx, IndexLabels))
	require.Eventually(t, func() bool {
		res, err := idx.SearchEntities(ctx, IndexLabels, spec.EntityQuery{Q: "ゆずソフト", Locales: []string{"jpn"}, Limit: 20})
		return err == nil && len(res.Hits) == 1 && res.Hits[0].ID == "b5147"
	}, 10*time.Second, 100*time.Millisecond, "the merged label is gone, the survivor stays")

	require.NoError(t, idx.DeleteBatch(ctx, IndexLabels, []string{"b96"}))
	require.NoError(t, idx.DeleteBatch(ctx, IndexLabels, nil))
}

func TestSeriesEngineTraitSearchable(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexSeries, IndexEngines, IndexTraits)

	s1 := EntityDoc{ID: "s1", EntityType: "series", NameOther: "Clannad", NameZh: "CLANNAD", Popularity: 2}
	s2 := EntityDoc{ID: "s2", EntityType: "series", NameOther: "Kanon", Popularity: 1}
	putDocs(t, IndexSeries, []EntityDoc{s1, s2})
	res, err := idx.SearchEntities(t.Context(), IndexSeries, spec.EntityQuery{Q: "Clannad", Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "s1", res.Hits[0].ID)
	}

	e1 := EntityDoc{ID: "e1", EntityType: "engine", NameOther: "KiriKiri", AliasesOther: []string{"吉里吉里"}, Popularity: 2}
	e2 := EntityDoc{ID: "e2", EntityType: "engine", NameOther: "Ren'Py", Popularity: 1}
	putDocs(t, IndexEngines, []EntityDoc{e1, e2})
	res, err = idx.SearchEntities(t.Context(), IndexEngines, spec.EntityQuery{Q: "吉里吉里", Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "e1", res.Hits[0].ID)
	}

	sexual := true
	tr1 := EntityDoc{ID: "f1", EntityType: "trait", NameOther: "Loli", NameZh: "萝莉", Sexual: &sexual, Popularity: 2}
	tr2 := EntityDoc{ID: "f2", EntityType: "trait", NameOther: "Tsundere", NameZh: "傲娇", Popularity: 1}
	putDocs(t, IndexTraits, []EntityDoc{tr1, tr2})
	res, err = idx.SearchEntities(t.Context(), IndexTraits, spec.EntityQuery{Q: "萝莉", Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "f1", res.Hits[0].ID)
		require.NotNil(t, res.Hits[0].Sexual)
		assert.True(t, *res.Hits[0].Sexual)
	}

	uid, ok := IndexForType("series")
	assert.True(t, ok)
	assert.Equal(t, IndexSeries, uid)
	uid, ok = IndexForType("engines")
	assert.True(t, ok)
	assert.Equal(t, IndexEngines, uid)
	uid, ok = IndexForType("traits")
	assert.True(t, ok)
	assert.Equal(t, IndexTraits, uid)
}

func TestTraitSearchNSFWFilter(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexTraits)
	sexual := true
	putDocs(t, IndexTraits, []EntityDoc{
		{ID: "f10", EntityType: "trait", NameOther: "SafeTrait", Popularity: 2},
		{ID: "f11", EntityType: "trait", NameOther: "SexTrait", Sexual: &sexual, Popularity: 1},
	})
	exclude := true
	res, err := idx.SearchEntities(t.Context(), IndexTraits, spec.EntityQuery{Limit: 20, SexualNot: &exclude})
	require.NoError(t, err)
	ids := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	assert.Contains(t, ids, "f10")
	assert.NotContains(t, ids, "f11")

	res, err = idx.SearchEntities(t.Context(), IndexTraits, spec.EntityQuery{Limit: 20})
	require.NoError(t, err)
	ids = ids[:0]
	for _, h := range res.Hits {
		ids = append(ids, h.ID)
	}
	assert.Contains(t, ids, "f10")
	assert.Contains(t, ids, "f11")
}

func TestEntitySearchPagination(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexSeries)

	docs := make([]EntityDoc, 0, 5)
	for i := 1; i <= 5; i++ {
		docs = append(docs, EntityDoc{
			ID: "s" + string(rune('0'+i)), EntityType: "series",
			NameOther: "Series" + string(rune('A'+i-1)), Popularity: float64(6 - i),
		})
	}
	putDocs(t, IndexSeries, docs)

	p1, err := idx.SearchEntities(t.Context(), IndexSeries, spec.EntityQuery{Q: "", Limit: 2, Page: 1})
	require.NoError(t, err)
	assert.EqualValues(t, 5, p1.Total)
	require.Len(t, p1.Hits, 2)
	p2, err := idx.SearchEntities(t.Context(), IndexSeries, spec.EntityQuery{Q: "", Limit: 2, Page: 2})
	require.NoError(t, err)
	assert.EqualValues(t, 5, p2.Total)
	require.Len(t, p2.Hits, 2)
	assert.NotEqual(t, p1.Hits[0].ID, p2.Hits[0].ID)
	assert.NotEqual(t, p1.Hits[1].ID, p2.Hits[1].ID)
	seen := map[string]bool{p1.Hits[0].ID: true, p1.Hits[1].ID: true}
	assert.False(t, seen[p2.Hits[0].ID])
	assert.False(t, seen[p2.Hits[1].ID])
}
