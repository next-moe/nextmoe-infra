package search

import (
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/search/spec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mustJSON(t *testing.T, d EntityDoc) string {
	t.Helper()
	b, err := json.Marshal(d)
	require.NoError(t, err)
	return string(b)
}

func TestSanitizeQueryOperators(t *testing.T) {
	cases := map[string]string{
		"アヘ顔アクメ中毒 －人体改造で狂ってイク私を見ないで－": "アヘ顔アクメ中毒  人体改造で狂ってイク私を見ないで",
		"－人体改造":                "人体改造",
		"＂CLANNAD＂":            "CLANNAD",
		"CRAZY CHA!N -エルピスの鎖-": "CRAZY CHA!N  エルピスの鎖",
		`"CLANNAD"`:            "CLANNAD",
		"色仕掛け学園～思春期男子誘惑作戦～": "色仕掛け学園～思春期男子誘惑作戦～",
		"ソードアート":     "ソードアート",
		"":           "",
		"  spaced  ": "spaced",
	}
	for in, want := range cases {
		assert.Equal(t, want, spec.SanitizeQuery(in), "in=%q", in)
	}
}

func TestLocalesForUIPairsWithTheIndex(t *testing.T) {
	assert.Equal(t, []string{"cmn"}, LocalesForUI(IndexCharacters, "zh"))
	assert.Equal(t, []string{"jpn"}, LocalesForUI(IndexCreditNames, "ja"))
	assert.Nil(t, LocalesForUI(IndexLabels, "en"))
	assert.Nil(t, LocalesForUI(IndexWorks, "zh"))
	assert.Nil(t, LocalesForUI(IndexWorks, "ja"))
}

func TestBuildWorkDoc(t *testing.T) {
	d := BuildWorkDoc(WorkDocInput{
		ID: 42, DisplayName: "いろとりどりのセカイ", OLang: "zh-Hans",
		ContentRating: 2, Claimed: true, ClaimState: "live",
		ReleasedOrd: 20240600, UpdatedTS: 1700000000, Popularity: 3.5,
		Titles: []WorkDocTitle{
			{Lang: "ja", Title: "いろとりどりのセカイ"},
			{Lang: "zh", Title: "五彩斑斓的世界", Latin: "Irotoridori no Sekai"},
			{Lang: "", Title: "别名テスト"},
		},
		TagIDs: []int64{7, 9}, LabelIDs: []int64{3}, EngineIDs: []int64{1}, SeriesIDs: []int64{5},
		Sources: []string{"vndb:v19658"}, SourceKeys: []string{"vndb"},
	})

	assert.Equal(t, "w42", d.ID)
	assert.Equal(t, "work", d.EntityType)
	assert.Equal(t, "いろとりどりのセカイ", d.NameJa, "display_name claims its bucket first")
	assert.Equal(t, "五彩斑斓的世界", d.NameZh)
	assert.Equal(t, "Irotoridori no Sekai", d.Latin, "first non-empty latin wins")
	assert.Equal(t, []string{"别名テスト"}, d.AliasesJa)

	require.NotNil(t, d.ContentRating)
	assert.EqualValues(t, 2, *d.ContentRating)
	require.NotNil(t, d.Claimed)
	assert.True(t, *d.Claimed)
	assert.Equal(t, "zh-Hans", d.OLang, "olang is the registry value verbatim, never re-derived from the title")
	assert.Equal(t, []int64{7, 9}, d.TagIDs)
	assert.Equal(t, []int64{3}, d.LabelIDs)
	assert.Equal(t, []int64{1}, d.EngineIDs)
	assert.Equal(t, []int64{5}, d.SeriesIDs)
	assert.EqualValues(t, 20240600, d.ReleasedOrd)
	assert.EqualValues(t, 1700000000, d.UpdatedTS)

	undated := BuildWorkDoc(WorkDocInput{ID: 1, DisplayName: "Undated"})
	assert.Zero(t, undated.ReleasedOrd)
	assert.NotContains(t, mustJSON(t, undated), `"released_ord"`)

	assert.Contains(t, mustJSON(t, undated), `"claimed":false`)

	assert.Equal(t, "live", d.ClaimState)
	assert.Contains(t, mustJSON(t, d), `"claim_state":"live"`)
	none := BuildWorkDoc(WorkDocInput{ID: 2, DisplayName: "Bodyless", ClaimState: "none"})
	assert.Contains(t, mustJSON(t, none), `"claim_state":"none"`)
	// A document with no claim_state attribute at all is invisible to
	// `claim_state != 'hidden'`, so the read face returned nothing.
	assert.Equal(t, model.ClaimStateKeyNone, undated.ClaimState)
	assert.Contains(t, mustJSON(t, undated), `"claim_state":"none"`)
	assert.Contains(t, WorksFilterableAttributes, "claim_state",
		"a claim_state the index cannot filter on is a gate that silently does nothing")
}

func TestBuildTagDoc(t *testing.T) {
	d := BuildTagDoc(TagDocInput{
		ID: 12, Name: "純愛", Tier: 1, Kind: 1, WorkCount: 6,
		Sources: []string{"galgame_wiki:311"}, SourceKeys: []string{"galgame_wiki"},
	})
	assert.Equal(t, "t12", d.ID)
	assert.Equal(t, "tag", d.EntityType)
	assert.Equal(t, "純愛", d.NameZh, "han-only names bucket to zh")
	require.NotNil(t, d.Tier)
	require.NotNil(t, d.Kind)
	assert.EqualValues(t, 1, *d.Tier)
	assert.EqualValues(t, 1, *d.Kind)
	assert.InDelta(t, 1.9459, d.Popularity, 0.001, "work count is log-damped like a credit count")
	assert.Equal(t, []string{"galgame_wiki"}, d.SourceKeys)
}

func TestGuessLang(t *testing.T) {
	assert.Equal(t, "ja", GuessLang("いろとりどり"))
	assert.Equal(t, "ja", GuessLang("ソード"))
	assert.Equal(t, "zh", GuessLang("純愛"))
	assert.Equal(t, "en", GuessLang("Sekai Project"))
}

func TestWorkDocIDRoundTrip(t *testing.T) {
	id, ok := WorkDocIDToWorkID(WorkDocID(19658))
	require.True(t, ok)
	assert.EqualValues(t, 19658, id)
	for _, bad := range []string{"", "w", "b12", "wabc", "w0", "w-3"} {
		_, ok := WorkDocIDToWorkID(bad)
		assert.Falsef(t, ok, "%q must not parse as a work doc id", bad)
	}
}

func TestTagsIndexSearchable(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexTags)

	docs := []EntityDoc{
		BuildTagDoc(TagDocInput{ID: 1, Name: "純愛", Tier: 0, Kind: 0, WorkCount: 100}),
		BuildTagDoc(TagDocInput{ID: 2, Name: "ファンタジー", Tier: 1, Kind: 0, WorkCount: 5}),
	}
	putDocs(t, IndexTags, docs)

	res, err := idx.SearchEntities(t.Context(), IndexTags, spec.EntityQuery{Q: "ファンタジー", Locales: []string{"jpn"}, Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 1) {
		assert.Equal(t, "t2", res.Hits[0].ID)
		require.NotNil(t, res.Hits[0].Tier)
		assert.EqualValues(t, 1, *res.Hits[0].Tier)
	}
	res, err = idx.SearchEntities(t.Context(), IndexTags, spec.EntityQuery{Q: "", Limit: 20})
	require.NoError(t, err)
	if assert.Len(t, res.Hits, 2) {
		assert.Equal(t, "t1", res.Hits[0].ID)
	}
	uid, ok := IndexForType("tags")
	assert.True(t, ok)
	assert.Equal(t, IndexTags, uid)
}

func TestSearchWorksPaging(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexWorks)

	docs := make([]EntityDoc, 0, 5)
	for i := 1; i <= 5; i++ {
		rating := int16(0)
		if i == 5 {
			rating = 2
		}
		docs = append(docs, BuildWorkDoc(WorkDocInput{
			ID: int64(i), DisplayName: "作品" + string(rune('A'+i-1)),
			ContentRating: rating, Popularity: float64(i), UpdatedTS: int64(i),
		}))
	}
	putDocs(t, IndexWorks, docs)

	ctx := t.Context()
	r18 := int16(model.ContentRatingR18)
	q := spec.WorksQuery{
		Filter: spec.WorksFilter{
			ContentRatingNot: &r18,
			OLang:            spec.OLang{All: true},
		},
		SortLane: "popularity:desc",
		Page:     1,
		Limit:    2,
	}

	p1, err := idx.SearchWorks(ctx, q)
	require.NoError(t, err)
	assert.EqualValues(t, 4, p1.Total, "total counts the filtered set, not the index")
	assert.Equal(t, []int64{4, 3}, p1.IDs)

	q.Page = 2
	p2, err := idx.SearchWorks(ctx, q)
	require.NoError(t, err)
	assert.EqualValues(t, 4, p2.Total)
	assert.Equal(t, []int64{2, 1}, p2.IDs)

	q.Page = 3
	p3, err := idx.SearchWorks(ctx, q)
	require.NoError(t, err)
	assert.Empty(t, p3.IDs, "a page past the end is empty, not an error")
	assert.EqualValues(t, 4, p3.Total)

	q.Page, q.FacetAttrs = 1, []string{"content_rating"}
	withFacets, err := idx.SearchWorks(ctx, q)
	require.NoError(t, err)
	require.Contains(t, withFacets.Facets, "content_rating")
	assert.EqualValues(t, 4, withFacets.Facets["content_rating"]["0"])
	assert.NotContains(t, withFacets.Facets["content_rating"], "2", "the excluded rating must not appear in the distribution")
}

func TestSearchWorksExcludeTags(t *testing.T) {
	idx := ensureSearchIndexes(t, IndexWorks)
	putDocs(t, IndexWorks, []EntityDoc{
		BuildWorkDoc(WorkDocInput{ID: 1, DisplayName: "甲", Popularity: 3, TagIDs: []int64{7}}),
		BuildWorkDoc(WorkDocInput{ID: 2, DisplayName: "乙", Popularity: 2, TagIDs: []int64{9, 11}}),
		BuildWorkDoc(WorkDocInput{ID: 3, DisplayName: "丙", Popularity: 1}),
	})
	ids := func(excl []int64) []int64 {
		t.Helper()
		res, err := idx.SearchWorks(t.Context(), spec.WorksQuery{
			Filter:   spec.WorksFilter{ExcludeTagIDs: excl, OLang: spec.OLang{All: true}},
			SortLane: "popularity:desc", Page: 1, Limit: 10,
		})
		require.NoError(t, err)
		return res.IDs
	}
	assert.Equal(t, []int64{1, 2, 3}, ids(nil))
	assert.Equal(t, []int64{2, 3}, ids([]int64{7}))
	assert.Equal(t, []int64{3}, ids([]int64{7, 11}))
}
