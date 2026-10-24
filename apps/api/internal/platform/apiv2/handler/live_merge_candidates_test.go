package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	catmodel "api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLiveMergeCandidatesAddSimilarNames(t *testing.T) {
	env := liveCatalog(t)
	name := fmt.Sprintf("脅迫%d", time.Now().UnixNano())
	anchor := catmodel.CatalogLabel{DisplayName: name}
	same := catmodel.CatalogLabel{DisplayName: name}
	similar := catmodel.CatalogLabel{DisplayName: name + "～終わらない明日～"}
	for _, label := range []*catmodel.CatalogLabel{&anchor, &same, &similar} {
		require.NoError(t, env.db.Create(label).Error)
	}
	t.Cleanup(func() {
		env.db.Unscoped().Delete(&catmodel.CatalogLabel{}, []int64{anchor.ID, same.ID, similar.ID})
	})

	orig := searchEntities
	prevSearcher := env.cat.Searcher
	t.Cleanup(func() {
		searchEntities = orig
		env.cat.Searcher = prevSearcher
	})
	env.cat.Searcher = &catsearch.Indexer{}
	var asked spec.EntityQuery
	searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
		asked = eq
		return catsearch.SearchResult{Total: 3, Hits: []catsearch.EntityDoc{
			{ID: "l" + idstr(anchor.ID), NameOther: anchor.DisplayName},
			{ID: "l" + idstr(same.ID), NameOther: same.DisplayName},
			{ID: "l" + idstr(similar.ID), NameOther: similar.DisplayName},
		}}, nil
	}

	status, _, raw := liveDo(t, env, http.MethodGet, fmt.Sprintf("/v2/me/merge-candidates?object=company&id=%d", anchor.ID), livePlainToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var got struct {
		Items []struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, name, asked.Q, "the search runs on the entry's own name")
	reasons := map[string]string{}
	for _, item := range got.Items {
		reasons[item.ID] = item.Reason
	}
	assert.NotContains(t, reasons, idstr(anchor.ID), "the entry is never its own candidate")
	assert.Equal(t, "same_name", reasons[idstr(same.ID)], "an exact match keeps its stronger reason")
	assert.Equal(t, "similar_name", reasons[idstr(similar.ID)])
}

func TestLiveCatalogNamesCarryLangAndLocalized(t *testing.T) {
	env := liveCatalog(t)
	type localized map[string]struct {
		Value string `json:"value"`
	}
	status, _, raw := liveDo(t, env, http.MethodGet,
		fmt.Sprintf("/v2/catalog/names?keys=work:%d,company:%d&nsfw=true", env.fx.Work, env.fx.Company), liveSecondReadToken, "")
	require.Equal(t, http.StatusOK, status, string(raw))
	var names struct {
		Items []struct {
			Key       string    `json:"key"`
			Lang      *string   `json:"lang"`
			Localized localized `json:"localized"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(raw, &names))
	require.Len(t, names.Items, 2)

	lane := func(path string) (string, localized) {
		status, _, raw := liveDo(t, env, http.MethodGet, path, liveSecondReadToken, "")
		require.Equal(t, http.StatusOK, status, string(raw))
		var list struct {
			Items []struct {
				OLang     string    `json:"olang"`
				Lang      *string   `json:"lang"`
				Localized localized `json:"localized"`
			} `json:"items"`
		}
		require.NoError(t, json.Unmarshal(raw, &list))
		require.Len(t, list.Items, 1)
		item := list.Items[0]
		if item.Lang != nil {
			return *item.Lang, item.Localized
		}
		return item.OLang, item.Localized
	}
	workLang, workLocalized := lane(fmt.Sprintf("/v2/catalog/works?ids=%d&include=titles&nsfw=true", env.fx.Work))
	companyLang, companyLocalized := lane(fmt.Sprintf("/v2/catalog/companies?ids=%d&nsfw=true", env.fx.Company))

	for _, item := range names.Items {
		wantLang, wantLocalized := workLang, workLocalized
		if item.Key != fmt.Sprintf("work:%d", env.fx.Work) {
			wantLang, wantLocalized = companyLang, companyLocalized
		}
		if wantLang == "" {
			assert.Nil(t, item.Lang, item.Key)
		} else if assert.NotNil(t, item.Lang, item.Key) {
			assert.Equal(t, wantLang, *item.Lang, item.Key)
		}
		assert.Equal(t, wantLocalized, item.Localized, item.Key)
	}
	assert.NotEmpty(t, workLocalized, "the fixture work needs a localized title, or the work lane is untested")
}
