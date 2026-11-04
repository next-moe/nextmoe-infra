package handler

import (
	"context"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/catalog/dto"
	catsvc "api/internal/platform/catalog/service"
)

func TestListWorksHybridRefusesWhatItCannotRank(t *testing.T) {
	cat := &Catalog{Public: &catsvc.PublicService{}}
	for _, tc := range []struct {
		name  string
		raw   collect.Raw
		in    listWorksInput
		param string
	}{
		{"no q", collect.Raw{}, listWorksInput{SearchMode: "hybrid"}, "q"},
		{"facets", collect.Raw{Facets: "olang"}, listWorksInput{SearchMode: "hybrid", Q: "猫耳"}, "facets"},
		{"non-relevance sort", collect.Raw{Sort: "popularity"}, listWorksInput{SearchMode: "hybrid", Q: "猫耳"}, "sort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, perr := collect.Parse(tc.raw, collect.WorkListSpec())
			if perr != nil {
				t.Fatal(perr)
			}
			f, ferr := parseWorksFilter(&tc.in, false)
			if ferr != nil {
				t.Fatal(ferr)
			}
			_, err := cat.ListWorksFiltered(context.Background(), q, f)
			p, ok := err.(*problem.Problem)
			if !ok || len(p.Errors) == 0 || p.Errors[0].Parameter != tc.param {
				t.Fatalf("err = %v, want a problem naming %s=", err, tc.param)
			}
		})
	}
	if _, ferr := parseWorksFilter(&listWorksInput{SearchMode: "semantic"}, false); ferr == nil {
		t.Fatal("search_mode is a closed vocabulary")
	}
}

func TestListWorksHybridPassesTheModeAndReportsIt(t *testing.T) {
	orig := searchWorks
	t.Cleanup(func() { searchWorks = orig })
	var got catsvc.WorksSearchFilter
	searchWorks = func(_ *Catalog, _ context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
		got = sf
		return dto.PublicWorksSearchData{Total: 1, Page: sf.Page, Limit: sf.Limit, Items: []dto.PublicWorkListItem{fakeWorkItem()}, SearchMode: catsvc.SearchModeLexical}, nil
	}
	cat := &Catalog{Public: &catsvc.PublicService{}}
	q, perr := collect.Parse(collect.Raw{}, collect.WorkListSpec())
	if perr != nil {
		t.Fatal(perr)
	}
	f, ferr := parseWorksFilter(&listWorksInput{SearchMode: "hybrid", Q: "有猫耳妹妹的催泪作品", TagID: "3"}, false)
	if ferr != nil {
		t.Fatal(ferr)
	}
	out, err := cat.ListWorksFiltered(context.Background(), q, f)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchMode != catsvc.SearchModeHybrid || got.Sort != "relevance" || len(got.TagIDs) != 1 {
		t.Fatalf("search filter = %+v", got)
	}
	if out.SearchMode == nil || *out.SearchMode != catsvc.SearchModeLexical {
		t.Fatalf("the list must report the mode that ranked it, got %v", out.SearchMode)
	}

	f, _ = parseWorksFilter(&listWorksInput{Q: "千恋"}, false)
	out, err = cat.ListWorksFiltered(context.Background(), q, f)
	if err != nil {
		t.Fatal(err)
	}
	if got.SearchMode != "" {
		t.Fatalf("an absent search_mode must stay absent, got %q", got.SearchMode)
	}
}
