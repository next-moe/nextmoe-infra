package handler

import (
	"context"
	"sort"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/dto"
	catsearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
	catsvc "api/internal/platform/catalog/service"

	"github.com/gofiber/fiber/v3"
)

func fakeWorkItem() dto.PublicWorkListItem {
	return dto.PublicWorkListItem{
		ID: 1, Medium: "galgame", DisplayName: "x",
		ContentRating: "all_ages", OLang: "ja",
		Updated: "2020-01-01T00:00:00Z",
	}
}

func TestFinishPageListOmitsCursorAndAlwaysTotals(t *testing.T) {
	out := finishPageList([]string{"a"}, 40)
	if out.NextCursor != nil {
		t.Fatalf("next_cursor=%v", out.NextCursor)
	}
	if out.Total == nil || *out.Total != 40 {
		t.Fatalf("total=%v", out.Total)
	}
	if out.TotalRelation == nil || *out.TotalRelation != "eq" {
		t.Fatalf("total_relation=%v", out.TotalRelation)
	}
}

func TestFinishListCursorModeHasNoTotalRelation(t *testing.T) {
	next := "4"
	out := finishList([]string{"a"}, &next, 40, collect.Query{Cursor: "3", Limit: 20}, nil)
	if out.NextCursor == nil || *out.NextCursor != collect.EncodeCursor("4") {
		t.Fatalf("cursor=%v", out.NextCursor)
	}
	if out.Total != nil {
		t.Fatalf("total=%v", out.Total)
	}
	if out.TotalRelation != nil {
		t.Fatalf("total_relation=%v", out.TotalRelation)
	}
}

func TestListWorksSearchPageMode(t *testing.T) {
	orig := searchWorks
	t.Cleanup(func() { searchWorks = orig })
	cat := &Catalog{Public: &catsvc.PublicService{}}

	for _, page := range []int{1, 3} {
		var got []catsvc.WorksSearchFilter
		searchWorks = func(_ *Catalog, _ context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
			got = append(got, sf)
			return dto.PublicWorksSearchData{
				Total: 10000, Page: sf.Page, Limit: sf.Limit,
				Items: []dto.PublicWorkListItem{fakeWorkItem()},
			}, nil
		}
		out, err := cat.ListWorksFiltered(t.Context(), collect.Query{Page: page, Limit: 20}, worksFilter{})
		if err != nil {
			t.Fatalf("page=%d: %v", page, err)
		}
		if len(got) != 1 || got[0].Page != page || got[0].Limit != 20 {
			t.Fatalf("page=%d filter %+v", page, got)
		}
		if from := (got[0].Page - 1) * got[0].Limit; from != (page-1)*20 {
			t.Fatalf("page=%d from=%d", page, from)
		}
		if out.NextCursor != nil {
			t.Fatalf("page=%d next_cursor=%v", page, out.NextCursor)
		}
		if out.Total == nil || *out.Total != 10000 {
			t.Fatalf("page=%d total=%v", page, out.Total)
		}
		if out.TotalRelation == nil || *out.TotalRelation != "eq" {
			t.Fatalf("page=%d total_relation=%v", page, out.TotalRelation)
		}
	}
}

func TestListWorksSearchCursorModeUnchanged(t *testing.T) {
	orig := searchWorks
	t.Cleanup(func() { searchWorks = orig })
	var got []catsvc.WorksSearchFilter
	searchWorks = func(_ *Catalog, _ context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
		got = append(got, sf)
		return dto.PublicWorksSearchData{
			Total: 10000, Page: sf.Page, Limit: sf.Limit,
			Items: []dto.PublicWorkListItem{fakeWorkItem()},
		}, nil
	}
	cat := &Catalog{Public: &catsvc.PublicService{}}
	out, err := cat.ListWorksFiltered(t.Context(), collect.Query{Cursor: "3", Limit: 20}, worksFilter{Q: "kanon"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Page != 3 || got[0].Limit != 20 {
		t.Fatalf("filter %+v", got)
	}
	if out.NextCursor == nil || *out.NextCursor != collect.EncodeCursor("4") {
		t.Fatalf("next_cursor=%v", out.NextCursor)
	}
	if out.TotalRelation != nil {
		t.Fatalf("total_relation=%v", out.TotalRelation)
	}
	if out.Total != nil {
		t.Fatalf("total=%v", out.Total)
	}
}

func TestListWorksFilteredPageSelectsSearchLane(t *testing.T) {
	orig := searchWorks
	t.Cleanup(func() { searchWorks = orig })
	var got []catsvc.WorksSearchFilter
	searchWorks = func(_ *Catalog, _ context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
		got = append(got, sf)
		return dto.PublicWorksSearchData{Total: 1, Page: sf.Page, Limit: sf.Limit}, nil
	}
	cat := &Catalog{Public: &catsvc.PublicService{}}
	_, err := cat.ListWorksFiltered(t.Context(), collect.Query{Page: 1, Limit: 20}, worksFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("page=1 alone must reach WorksSearch")
	}
}

func TestListWorksFilteredRefusesOwnerUIDOnPage(t *testing.T) {
	cat := &Catalog{Public: &catsvc.PublicService{}}
	f, perr := parseWorksFilter(&listWorksInput{OwnerUID: "7", Site: "kungal"}, false)
	if perr != nil {
		t.Fatal(perr)
	}
	_, err := cat.ListWorksFiltered(t.Context(), collect.Query{Page: 1, Limit: 20}, f)
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("owner_uid + page: %v", err)
	}
}

func TestSearchPageMode(t *testing.T) {
	orig := searchEntities
	t.Cleanup(func() { searchEntities = orig })
	c := &Catalog{Searcher: &catsearch.Indexer{}}
	hit := catsearch.EntityDoc{ID: "w1", NameJa: "x", Sources: []string{}}

	for _, page := range []int{1, 3} {
		var got []spec.EntityQuery
		searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
			got = append(got, eq)
			return catsearch.SearchResult{Total: 10000, Hits: []catsearch.EntityDoc{hit}}, nil
		}
		out, err := c.Search(t.Context(), collect.Query{Page: page, Limit: 20}, "work", "foo", "")
		if err != nil {
			t.Fatalf("page=%d: %v", page, err)
		}
		if len(got) != 1 || got[0].Page != page || got[0].Limit != 20 {
			t.Fatalf("page=%d query %+v", page, got)
		}
		if from := (got[0].Page - 1) * got[0].Limit; from != (page-1)*20 {
			t.Fatalf("page=%d from=%d", page, from)
		}
		if out.NextCursor != nil {
			t.Fatalf("page=%d next_cursor=%v", page, out.NextCursor)
		}
		if out.Total == nil || *out.Total != 10000 {
			t.Fatalf("page=%d total=%v", page, out.Total)
		}
		if out.TotalRelation == nil || *out.TotalRelation != "eq" {
			t.Fatalf("page=%d total_relation=%v", page, out.TotalRelation)
		}
	}
}

func TestSearchCursorModeUnchanged(t *testing.T) {
	orig := searchEntities
	t.Cleanup(func() { searchEntities = orig })
	var got []spec.EntityQuery
	searchEntities = func(_ *Catalog, _ context.Context, _ string, eq spec.EntityQuery) (catsearch.SearchResult, error) {
		got = append(got, eq)
		return catsearch.SearchResult{
			Total: 10000,
			Hits:  []catsearch.EntityDoc{{ID: "w1", NameJa: "x", Sources: []string{}}},
		}, nil
	}
	c := &Catalog{Searcher: &catsearch.Indexer{}}
	out, err := c.Search(t.Context(), collect.Query{Cursor: "3", Limit: 20}, "work", "foo", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Page != 3 || got[0].Limit != 20 {
		t.Fatalf("query %+v", got)
	}
	if out.NextCursor == nil || *out.NextCursor != collect.EncodeCursor("4") {
		t.Fatalf("next_cursor=%v", out.NextCursor)
	}
	if out.TotalRelation != nil {
		t.Fatalf("total_relation=%v", out.TotalRelation)
	}
}

func TestPageDeclaredOnPageModeOperations(t *testing.T) {
	doc := Setup(fiber.New()).OpenAPI()
	var ops []string
	for _, item := range doc.Paths {
		if item == nil {
			continue
		}
		for _, op := range pathOps(item) {
			if op == nil {
				continue
			}
			for _, p := range op.Parameters {
				if p != nil && p.In == "query" && p.Name == "page" {
					ops = append(ops, op.OperationID)
					break
				}
			}
		}
	}
	sort.Strings(ops)
	want := []string{"listCatalogCharacters", "listCatalogWorks", "searchCatalog"}
	if len(ops) != len(want) || ops[0] != want[0] || ops[1] != want[1] || ops[2] != want[2] {
		t.Fatalf("page ops=%v want %v", ops, want)
	}
}
