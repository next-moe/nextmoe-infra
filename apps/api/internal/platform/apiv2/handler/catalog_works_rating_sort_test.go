package handler

import (
	"context"
	"slices"
	"strings"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/search/spec"
	catsvc "api/internal/platform/catalog/service"
)

func TestWorkRatingSortsCoverEveryIndexedSource(t *testing.T) {
	var got []string
	for _, s := range collect.WorkSort {
		if src, ok := strings.CutPrefix(s, "rating_"); ok {
			got = append(got, src)
		}
	}
	if !slices.Equal(got, spec.RatingSources) {
		t.Fatalf("rating sorts %v, indexed rating sources %v", got, spec.RatingSources)
	}
	for _, s := range collect.WorkSort {
		if !collect.SearchSort(s) {
			continue
		}
		if _, ok := catsvc.WorksSearchSortRule(s); !ok {
			t.Fatalf("search sort %q has no index sort rule", s)
		}
	}
}

func TestListWorksRatingSortRidesTheSearchLane(t *testing.T) {
	orig := searchWorks
	t.Cleanup(func() { searchWorks = orig })
	var got []catsvc.WorksSearchFilter
	searchWorks = func(_ *Catalog, _ context.Context, sf catsvc.WorksSearchFilter) (dto.PublicWorksSearchData, error) {
		got = append(got, sf)
		return dto.PublicWorksSearchData{Total: 1, Page: sf.Page, Limit: sf.Limit, Items: []dto.PublicWorkListItem{fakeWorkItem()}}, nil
	}
	cat := &Catalog{Public: &catsvc.PublicService{}}

	in := &listWorksInput{
		TagID: "3,5", CompanyID: "7", OLang: "ja,zh-Hans", ReleasedAfter: "2010-01-01", ReleasedBefore: "2020-12-31",
		ContentRating: "sensitive",
	}
	f, perr := parseWorksFilter(in, false)
	if perr != nil {
		t.Fatal(perr)
	}

	for _, sort := range []string{"rating_bangumi", "rating_vndb", "rating_erogamescape", "rating_dlsite", "rating_nextmoe"} {
		for _, facets := range []string{"", "olang,tag_id,company_id"} {
			q, perr := collect.Parse(collect.Raw{Sort: sort, Facets: facets}, collect.WorkListSpec())
			if perr != nil {
				t.Fatalf("sort=%s: %v", sort, perr)
			}
			got = nil
			if _, err := cat.ListWorksFiltered(t.Context(), q, f); err != nil {
				t.Fatalf("sort=%s facets=%q: %v", sort, facets, err)
			}
			if len(got) != 1 {
				t.Fatalf("sort=%s facets=%q did not reach the search lane", sort, facets)
			}
			sf := got[0]
			if sf.Sort != sort || !slices.Equal(sf.TagIDs, []int64{3, 5}) || sf.LabelID != 7 ||
				sf.ReleasedAfter != 20100101 || sf.ReleasedBefore != 20201231 || sf.ContentRating == nil ||
				!slices.Equal(sf.OLang.Values, []string{"ja", "zh-Hans"}) || len(sf.Facets) != len(q.Facets) {
				t.Fatalf("sort=%s facets=%q filter %+v", sort, facets, sf)
			}
		}
	}
}
