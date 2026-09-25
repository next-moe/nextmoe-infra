package osengine

import (
	"reflect"
	"testing"

	"api/internal/platform/catalog/search/spec"
)

func TestCharactersSearchBodyFiltersAndSort(t *testing.T) {
	all := spec.CharacterQuery{
		TraitIDs: []int64{10, 20}, Limit: 20, Page: 1, Sort: "id",
	}
	body, _ := charactersSearchBody(all)
	filters := queryFilters(t, body)
	if len(filters) != 2 {
		t.Fatalf("all: %d filters, want 2 term clauses", len(filters))
	}
	if !isTerm(filters[0], "trait_ids_sfw", int64(10)) || !isTerm(filters[1], "trait_ids_sfw", int64(20)) {
		t.Fatalf("all filters: %#v", filters)
	}

	anyQ := spec.CharacterQuery{
		TraitIDs: []int64{10, 20}, TraitMatchAny: true, Limit: 20, Page: 1,
	}
	body, _ = charactersSearchBody(anyQ)
	filters = queryFilters(t, body)
	if len(filters) != 1 || !isTerms(filters[0], "trait_ids_sfw", []int64{10, 20}) {
		t.Fatalf("any filters: %#v", filters)
	}

	nsfw := spec.CharacterQuery{
		TraitIDs: []int64{10}, NSFW: true, Limit: 20, Page: 1,
	}
	body, _ = charactersSearchBody(nsfw)
	filters = queryFilters(t, body)
	if len(filters) != 1 || !isTerm(filters[0], "trait_ids", int64(10)) {
		t.Fatalf("nsfw field: %#v", filters)
	}

	genders := spec.CharacterQuery{
		Genders: []int16{2, 1}, Limit: 20, Page: 1,
	}
	body, _ = charactersSearchBody(genders)
	filters = queryFilters(t, body)
	if len(filters) != 1 || !isTerms(filters[0], "gender", []int16{2, 1}) {
		t.Fatalf("gender filters: %#v", filters)
	}
}

func TestCharactersSearchBodySortClauses(t *testing.T) {
	idAsc := map[string]any{"catalog_id": map[string]any{"order": "asc"}}
	idDesc := map[string]any{"catalog_id": map[string]any{"order": "desc"}}
	popDesc := map[string]any{"popularity": map[string]any{"order": "desc"}}
	cases := []struct {
		sort string
		want []any
	}{
		{"popularity", []any{popDesc, idAsc}},
		{"relevance", []any{"_score", popDesc, idAsc}},
		{"newest", []any{idDesc}},
		{"id", []any{idAsc}},
		{"", []any{idAsc}},
	}
	for _, tc := range cases {
		body, _ := charactersSearchBody(spec.CharacterQuery{Sort: tc.sort, Limit: 20, Page: 1})
		got, _ := body["sort"].([]any)
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("sort=%q got %#v want %#v", tc.sort, got, tc.want)
		}
	}
}

func TestCharactersSearchBodyPageOffset(t *testing.T) {
	body, drop := charactersSearchBody(spec.CharacterQuery{Page: 3, Limit: 20, Sort: "id"})
	if drop {
		t.Fatal("page 3 of 20 must not drop hits")
	}
	if body["from"] != 40 || body["size"] != 20 {
		t.Fatalf("from=%v size=%v", body["from"], body["size"])
	}
	if body["track_total_hits"] != true {
		t.Fatal("track_total_hits")
	}
	if body["_source"] != false {
		t.Fatal("_source")
	}
}

func queryFilters(t *testing.T, body map[string]any) []any {
	t.Helper()
	q, _ := body["query"].(map[string]any)
	bq, _ := q["bool"].(map[string]any)
	raw := bq["filter"]
	switch v := raw.(type) {
	case []any:
		return v
	case nil:
		return nil
	default:
		t.Fatalf("filter type %T", raw)
		return nil
	}
}

func isTerm(clause any, field string, value any) bool {
	m, _ := clause.(map[string]any)
	term, _ := m["term"].(map[string]any)
	got, ok := term[field]
	return ok && reflect.DeepEqual(got, value)
}

func isTerms(clause any, field string, value any) bool {
	m, _ := clause.(map[string]any)
	terms, _ := m["terms"].(map[string]any)
	got, ok := terms[field]
	return ok && reflect.DeepEqual(got, value)
}
