package osengine

import (
	"context"
	"net/http"

	"api/internal/platform/catalog/search/spec"
)

type PersonSearchResult struct {
	IDs   []int64
	Total int64
}

func (e *Engine) SearchPersons(ctx context.Context, q spec.EntityQuery) (PersonSearchResult, error) {
	var resp struct {
		Hits struct {
			Hits []struct {
				Source struct {
					PersonID int64 `json:"person_id"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
		Aggregations struct {
			Persons struct {
				Value int64 `json:"value"`
			} `json:"persons"`
		} `json:"aggregations"`
	}
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(IndexCreditNames)+"/_search", personsSearchBody(q), &resp); err != nil {
		return PersonSearchResult{}, err
	}
	out := PersonSearchResult{IDs: make([]int64, 0, len(resp.Hits.Hits)), Total: resp.Aggregations.Persons.Value}
	for _, h := range resp.Hits.Hits {
		out.IDs = append(out.IDs, h.Source.PersonID)
	}
	return out, nil
}

func personsSearchBody(q spec.EntityQuery) map[string]any {
	page := max(q.Page, 1)
	text := spec.SanitizeQuery(q.Q)
	linked := []any{map[string]any{"exists": map[string]any{"field": "person_id"}}}
	return map[string]any{
		"query":            buildQuery(text, false, true, linked),
		"collapse":         map[string]any{"field": "person_id"},
		"from":             (page - 1) * q.Limit,
		"size":             q.Limit,
		"sort":             searchSort("", text != ""),
		"_source":          []string{"person_id"},
		"track_total_hits": false,
		"aggs": map[string]any{
			"persons": map[string]any{"cardinality": map[string]any{"field": "person_id"}},
		},
	}
}
