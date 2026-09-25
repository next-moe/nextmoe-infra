package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/model"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"

	"github.com/stretchr/testify/require"
)

type liveTraitView struct {
	ID       string  `json:"id"`
	IsSexual bool    `json:"is_sexual"`
	GroupID  *string `json:"group_id"`
	Group    *string `json:"group"`
	Parents  []struct {
		ID string `json:"id"`
	} `json:"parents"`
	RootOrder    *int      `json:"root_order"`
	ChildCount   int       `json:"child_count"`
	Aliases      *[]string `json:"aliases"`
	Description  *string   `json:"description"`
	IsSearchable bool      `json:"is_searchable"`
	IsApplicable bool      `json:"is_applicable"`
}

type liveTraitListPage struct {
	Items      []liveTraitView `json:"items"`
	Missing    *[]string       `json:"missing"`
	NextCursor *string         `json:"next_cursor"`
}

func TestLiveTraitVocabulary(t *testing.T) {
	env := liveCatalog(t)
	db := env.db

	trait := func(tid, name, groupTID string, gorder int16, sexual bool, alias, desc string) *model.CatalogCharacterTrait {
		t.Helper()
		row := &model.CatalogCharacterTrait{
			VndbTID: tid, Name: name, GroupTID: groupTID, GOrder: gorder,
			Sexual: sexual, Searchable: true, Applicable: true,
			Alias: alias, Description: desc,
		}
		require.NoError(t, db.Create(row).Error)
		return row
	}
	parent := func(child, par int64) {
		t.Helper()
		require.NoError(t, db.Create(&model.CatalogCharacterTraitParent{TraitID: child, ParentID: par}).Error)
	}

	r := trait("i66101", "Vocab Root", "", 99, false, "", "")
	sr := trait("i66102", "Vocab Sexual Root", "", 98, true, "", "")
	a := trait("i66103", "Vocab A", r.VndbTID, 0, false, "a\n\n b \na", "Uses [url=https://x.test]magic[/url].\n\n\n\nMore")
	b := trait("i66104", "Vocab B", r.VndbTID, 0, false, "", "")
	a1 := trait("i66105", "Vocab A1", r.VndbTID, 0, false, "", "")
	m := trait("i66106", "Vocab M", r.VndbTID, 0, false, "", "")
	x := trait("i66107", "Vocab X", sr.VndbTID, 0, false, "", "")
	parent(a.ID, r.ID)
	parent(b.ID, r.ID)
	parent(a1.ID, a.ID)
	parent(m.ID, a.ID)
	parent(m.ID, b.ID)
	parent(x.ID, sr.ID)

	traitIDs := []int64{r.ID, sr.ID, a.ID, b.ID, a1.ID, m.ID, x.ID}
	t.Cleanup(func() {
		db.Where("trait_id IN ? OR parent_id IN ?", traitIDs, traitIDs).Delete(&model.CatalogCharacterTraitParent{})
		db.Where("id IN ?", traitIDs).Delete(&model.CatalogCharacterTrait{})
	})

	require.NoError(t, db.Exec(model.TraitSexualFamilySQL).Error)

	get := func(path string) (int, []byte) {
		t.Helper()
		status, _, body := liveDo(t, env, http.MethodGet, path, liveAppKey, "")
		return status, body
	}
	decode := func(body []byte) liveTraitView {
		t.Helper()
		var v liveTraitView
		require.NoError(t, json.Unmarshal(body, &v), string(body))
		return v
	}
	decodeList := func(body []byte) liveTraitListPage {
		t.Helper()
		var page liveTraitListPage
		require.NoError(t, json.Unmarshal(body, &page), string(body))
		return page
	}
	idsOf := func(page liveTraitListPage) []string {
		out := make([]string, 0, len(page.Items))
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}

	status, body := get("/v2/catalog/traits/" + idstr(m.ID))
	require.Equal(t, 200, status, string(body))
	got := decode(body)
	require.NotNil(t, got.GroupID)
	require.Equal(t, idstr(r.ID), *got.GroupID)
	require.NotNil(t, got.Group)
	require.Equal(t, []string{idstr(a.ID), idstr(b.ID)}, parentIDs(got))
	require.Nil(t, got.RootOrder)
	require.Equal(t, 0, got.ChildCount)

	status, body = get("/v2/catalog/traits/" + idstr(r.ID))
	require.Equal(t, 200, status, string(body))
	got = decode(body)
	require.Nil(t, got.GroupID)
	require.Nil(t, got.Group)
	require.Empty(t, got.Parents)
	require.NotNil(t, got.RootOrder)
	require.Equal(t, 99, *got.RootOrder)
	require.Equal(t, 2, got.ChildCount)

	status, body = get("/v2/catalog/traits?parent_id=" + idstr(r.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(a.ID), idstr(b.ID)}, idsOf(decodeList(body)))

	status, body = get("/v2/catalog/traits?group_id=" + idstr(r.ID))
	require.Equal(t, 200, status, string(body))
	require.ElementsMatch(t, []string{idstr(a.ID), idstr(b.ID), idstr(a1.ID), idstr(m.ID)}, idsOf(decodeList(body)))

	status, body = get("/v2/catalog/traits?root=true&ids=" + idstr(r.ID) + "," + idstr(a.ID))
	require.Equal(t, 200, status, string(body))
	page := decodeList(body)
	require.ElementsMatch(t, []string{idstr(r.ID)}, idsOf(page))
	require.NotNil(t, page.Missing)
	require.Contains(t, *page.Missing, idstr(a.ID))

	status, body = get("/v2/catalog/traits/" + idstr(x.ID))
	require.Equal(t, 404, status, string(body))
	require.Equal(t, problem.CodeNotFound, liveProblem(t, body).Code)

	status, body = get("/v2/catalog/traits/" + idstr(x.ID) + "?nsfw=true")
	require.Equal(t, 200, status, string(body))
	got = decode(body)
	require.True(t, got.IsSexual)

	status, body = get("/v2/catalog/traits?ids=" + idstr(x.ID) + "," + idstr(sr.ID) + "," + idstr(r.ID))
	require.Equal(t, 200, status, string(body))
	page = decodeList(body)
	require.ElementsMatch(t, []string{idstr(r.ID)}, idsOf(page))
	require.NotNil(t, page.Missing)
	require.Contains(t, *page.Missing, idstr(x.ID))
	require.Contains(t, *page.Missing, idstr(sr.ID))

	status, body = get("/v2/catalog/traits?parent_id=" + idstr(sr.ID))
	require.Equal(t, 400, status, string(body))
	pbm := liveProblem(t, body)
	require.Equal(t, problem.CodeInvalidParameter, pbm.Code)
	require.NotEmpty(t, pbm.Errors)
	require.Equal(t, "parent_id", pbm.Errors[0].Parameter)
	require.Equal(t, problem.ReasonNotAllowedValue, pbm.Errors[0].Reason)

	status, body = get("/v2/catalog/traits/" + idstr(a.ID) + "?include=aliases,description")
	require.Equal(t, 200, status, string(body))
	got = decode(body)
	require.NotNil(t, got.Aliases)
	require.Equal(t, []string{"a", "b"}, *got.Aliases)
	require.NotNil(t, got.Description)
	require.Equal(t, "Uses magic.\n\nMore", *got.Description)

	status, body = get("/v2/catalog/traits/" + idstr(a.ID))
	require.Equal(t, 200, status, string(body))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(body, &raw), string(body))
	_, hasAliases := raw["aliases"]
	_, hasDesc := raw["description"]
	require.False(t, hasAliases, string(body))
	require.False(t, hasDesc, string(body))
}

func TestLiveTraitCharacterCountFieldsAnd503(t *testing.T) {
	env := liveCatalog(t)
	db := env.db
	row := &model.CatalogCharacterTrait{
		VndbTID: "i66130", Name: "Count Root", GroupTID: "", GOrder: 96,
		Searchable: true, Applicable: true, Alias: "", Description: "",
	}
	require.NoError(t, db.Create(row).Error)
	t.Cleanup(func() {
		db.Where("id = ?", row.ID).Delete(&model.CatalogCharacterTrait{})
	})

	orig := characterTraitCounts
	t.Cleanup(func() { characterTraitCounts = orig })
	characterTraitCounts = func(_ *Catalog, _ context.Context, _ bool) (map[int64]int64, error) {
		return map[int64]int64{row.ID: 7}, nil
	}

	status, _, body := liveDo(t, env, http.MethodGet,
		"/v2/catalog/traits?ids="+idstr(row.ID)+"&include=character_count&fields=character_count",
		liveAppKey, "")
	require.Equal(t, 200, status, string(body))
	var page struct {
		Items []map[string]any `json:"items"`
	}
	require.NoError(t, json.Unmarshal(body, &page), string(body))
	require.Len(t, page.Items, 1)
	require.Equal(t, idstr(row.ID), page.Items[0]["id"])
	require.Equal(t, "trait", page.Items[0]["object"])
	require.EqualValues(t, 7, page.Items[0]["character_count"])
	_, hasName := page.Items[0]["display_name"]
	require.False(t, hasName, string(body))

	characterTraitCounts = func(_ *Catalog, _ context.Context, _ bool) (map[int64]int64, error) {
		return nil, errors.New("engine down")
	}
	status, _, body = liveDo(t, env, http.MethodGet,
		"/v2/catalog/traits?ids="+idstr(row.ID)+"&include=character_count",
		liveAppKey, "")
	require.Equal(t, 503, status, string(body))
	require.Equal(t, problem.CodeServiceUnavailable, liveProblem(t, body).Code)
}

func parentIDs(v liveTraitView) []string {
	out := make([]string, 0, len(v.Parents))
	for _, p := range v.Parents {
		out = append(out, p.ID)
	}
	return out
}

func TestLiveSearchTraitPath(t *testing.T) {
	env := liveCatalog(t)
	db := env.db

	r := &model.CatalogCharacterTrait{
		VndbTID: "i66121", Name: "Search Root", GroupTID: "", GOrder: 97, Searchable: true, Applicable: true,
		Alias: "", Description: "",
	}
	require.NoError(t, db.Create(r).Error)
	child := &model.CatalogCharacterTrait{
		VndbTID: "i66122", Name: "Search Child", GroupTID: r.VndbTID, Searchable: true, Applicable: true,
		Alias: "", Description: "",
	}
	require.NoError(t, db.Create(child).Error)
	require.NoError(t, db.Create(&model.CatalogCharacterTraitParent{TraitID: child.ID, ParentID: r.ID}).Error)
	t.Cleanup(func() {
		db.Where("trait_id IN ? OR parent_id IN ?", []int64{r.ID, child.ID}, []int64{r.ID, child.ID}).
			Delete(&model.CatalogCharacterTraitParent{})
		db.Where("id IN ?", []int64{r.ID, child.ID}).Delete(&model.CatalogCharacterTrait{})
	})
	require.NoError(t, db.Exec(model.TraitSexualFamilySQL).Error)

	orig := searchEntities
	prevSearcher := env.cat.Searcher
	t.Cleanup(func() {
		searchEntities = orig
		env.cat.Searcher = prevSearcher
	})
	env.cat.Searcher = &catsearch.Indexer{}

	var gotEQ spec.EntityQuery
	sexual := false
	searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
		gotEQ = eq
		return catsearch.SearchResult{
			Total: 1,
			Hits: []catsearch.EntityDoc{{
				ID: "f" + idstr(child.ID), EntityType: "trait", NameOther: "Search Child", Sexual: &sexual,
			}},
		}, nil
	}

	page, err := env.cat.Search(t.Context(), collect.Query{Limit: 20}, "trait", "child", "")
	require.NoError(t, err)
	require.NotNil(t, gotEQ.SexualNot)
	require.True(t, *gotEQ.SexualNot)
	require.Len(t, page.Items, 1)
	require.NotNil(t, page.Items[0].TraitPath)
	require.NotNil(t, page.Items[0].TraitPath.GroupID)
	require.Equal(t, idstr(r.ID), *page.Items[0].TraitPath.GroupID)
	require.NotNil(t, page.Items[0].TraitPath.Group)
	require.Len(t, page.Items[0].TraitPath.Parents, 1)
	require.Equal(t, idstr(r.ID), page.Items[0].TraitPath.Parents[0].ID)

	searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
		gotEQ = eq
		return catsearch.SearchResult{
			Total: 1,
			Hits: []catsearch.EntityDoc{{
				ID: "w1", EntityType: "work", NameOther: "A Work",
			}},
		}, nil
	}
	page, err = env.cat.Search(t.Context(), collect.Query{Limit: 20, NSFW: true}, "work", "work", "")
	require.NoError(t, err)
	require.Nil(t, gotEQ.SexualNot)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.Items[0].TraitPath)

	searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
		gotEQ = eq
		return catsearch.SearchResult{
			Total: 1,
			Hits: []catsearch.EntityDoc{{
				ID: "f" + idstr(child.ID), EntityType: "trait", NameOther: "Search Child", Sexual: &sexual,
			}},
		}, nil
	}
	_, err = env.cat.Search(t.Context(), collect.Query{Limit: 20, NSFW: true}, "trait", "child", "")
	require.NoError(t, err)
	require.Nil(t, gotEQ.SexualNot)
}
