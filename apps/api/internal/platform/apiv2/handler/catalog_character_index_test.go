package handler

import (
	"context"
	"testing"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	catsvc "api/internal/platform/catalog/service"
)

func TestCharacterIndexLane(t *testing.T) {
	if characterIndexLane(collect.Query{Sort: "id"}, "") {
		t.Fatal("plain")
	}
	if !characterIndexLane(collect.Query{Sort: "id"}, "aya") {
		t.Fatal("q")
	}
	if !characterIndexLane(collect.Query{Page: 1, Sort: "id"}, "") {
		t.Fatal("page")
	}
	if !characterIndexLane(collect.Query{Sort: "popularity"}, "") {
		t.Fatal("popularity")
	}
	if !characterIndexLane(collect.Query{Sort: "newest"}, "") {
		t.Fatal("newest")
	}
	if !characterIndexLane(collect.Query{Sort: "relevance"}, "") {
		t.Fatal("relevance")
	}
}

func TestListCharactersRelevanceNeedsQ(t *testing.T) {
	c := &Catalog{Public: &catsvc.PublicService{}}
	_, err := c.ListCharacters(t.Context(), collect.Query{Sort: "relevance", Limit: 20}, characterFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeInvalidParameter {
		t.Fatalf("%v", err)
	}
}

func TestListCharactersQAndIDsExclusive(t *testing.T) {
	c := &Catalog{Public: &catsvc.PublicService{}}
	_, err := c.ListCharacters(t.Context(), collect.Query{Batch: true, IDs: []string{"1"}, Sort: "id"}, characterFilter{Q: "x"})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("%v", err)
	}
}

func TestListCharactersSearchSortAndIDsExclusive(t *testing.T) {
	c := &Catalog{Public: &catsvc.PublicService{}}
	_, err := c.ListCharacters(t.Context(), collect.Query{Batch: true, IDs: []string{"1"}, Sort: "popularity"}, characterFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeMutuallyExclusiveParameters {
		t.Fatalf("%v", err)
	}
}

func TestListCharactersRegistryCursorOnIndexLane(t *testing.T) {
	c := &Catalog{Public: &catsvc.PublicService{}}
	_, err := c.ListCharacters(t.Context(), collect.Query{
		Sort: "popularity", Limit: 20, Cursor: `{"s":"characters","id":12}`,
	}, characterFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeInvalidCursor {
		t.Fatalf("%v", err)
	}
}

func TestListCharactersIndexLaneFromQAndPageAndPopularity(t *testing.T) {
	orig := searchCharacters
	t.Cleanup(func() { searchCharacters = orig })
	cat := &Catalog{Public: &catsvc.PublicService{}}

	cases := []struct {
		name string
		q    collect.Query
		f    characterFilter
		want string
		page int
	}{
		{name: "q", q: collect.Query{Sort: "id", Limit: 20}, f: characterFilter{Q: "aya"}, want: "relevance", page: 1},
		{name: "page", q: collect.Query{Sort: "id", Limit: 20, Page: 2}, want: "id", page: 2},
		{name: "popularity", q: collect.Query{Sort: "popularity", Limit: 20}, want: "popularity", page: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []catsvc.CharactersSearchFilter
			searchCharacters = func(_ *Catalog, _ context.Context, f catsvc.CharactersSearchFilter) (catsvc.CharactersSearchPage, error) {
				got = append(got, f)
				return catsvc.CharactersSearchPage{Total: 1, Page: f.Page, Limit: f.Limit}, nil
			}
			_, err := cat.ListCharacters(t.Context(), tc.q, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("calls %d", len(got))
			}
			if got[0].Sort != tc.want || got[0].Page != tc.page {
				t.Fatalf("%+v", got[0])
			}
		})
	}
}

func TestListTraitsCharacterCountUnavailable(t *testing.T) {
	orig := characterTraitCounts
	t.Cleanup(func() { characterTraitCounts = orig })
	characterTraitCounts = func(_ *Catalog, _ context.Context, _ bool) (map[int64]int64, error) {
		return nil, catsvc.ErrSearchUnavailable
	}
	c := &Catalog{Public: &catsvc.PublicService{}}
	_, err := c.ListTraits(t.Context(), collect.Query{Include: []string{"character_count"}, Limit: 20}, traitFilter{})
	p, ok := err.(*problem.Problem)
	if !ok || p.Code != problem.CodeServiceUnavailable {
		t.Fatalf("%v", err)
	}
}
