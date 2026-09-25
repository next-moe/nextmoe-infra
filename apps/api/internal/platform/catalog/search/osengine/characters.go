package osengine

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"api/internal/platform/catalog/search/spec"
)

func (e *Engine) SearchCharacters(ctx context.Context, q spec.CharacterQuery) (CharacterSearchResult, error) {
	body, dropHits := charactersSearchBody(q)
	var resp osSearchResponse
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(IndexCharacters)+"/_search", body, &resp); err != nil {
		return CharacterSearchResult{}, err
	}
	out := CharacterSearchResult{
		DocIDs: []string{},
		Total:  parseTotal(resp.Hits.Total),
	}
	if dropHits {
		return out, nil
	}
	out.DocIDs = make([]string, 0, len(resp.Hits.Hits))
	for _, h := range resp.Hits.Hits {
		out.DocIDs = append(out.DocIDs, h.ID)
	}
	return out, nil
}

type CharacterSearchResult struct {
	DocIDs []string
	Total  int64
}

func (e *Engine) CharacterTraitCounts(ctx context.Context, nsfw bool) (map[int64]int64, error) {
	field := "trait_ids_sfw"
	if nsfw {
		field = "trait_ids"
	}
	body := map[string]any{
		"size": 0,
		"aggs": map[string]any{
			"traits": map[string]any{
				"terms": map[string]any{"field": field, "size": 10000},
			},
		},
	}
	var resp osSearchResponse
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(IndexCharacters)+"/_search", body, &resp); err != nil {
		return nil, err
	}
	agg := resp.Aggregations["traits"]
	if agg.SumOtherDocCount > 0 {
		return nil, fmt.Errorf("character trait counts truncated: sum_other_doc_count=%d", agg.SumOtherDocCount)
	}
	out := make(map[int64]int64, len(agg.Buckets))
	for _, b := range agg.Buckets {
		id, ok := bucketInt64(b.Key)
		if !ok {
			continue
		}
		out[id] = b.DocCount
	}
	return out, nil
}

func charactersSearchBody(q spec.CharacterQuery) (map[string]any, bool) {
	text := spec.SanitizeQuery(q.Q)
	page := q.Page
	if page < 1 {
		page = 1
	}
	size := q.Limit
	from := (page - 1) * size
	dropHits := false
	if from+size > maxResultWindow {
		from = maxResultWindow - size
		if from < 0 {
			from = 0
		}
		dropHits = true
	}
	return map[string]any{
		"query":            buildQuery(text, false, true, characterFilterClauses(q)),
		"from":             from,
		"size":             size,
		"sort":             characterSearchSort(q.Sort),
		"track_total_hits": true,
		"_source":          false,
	}, dropHits
}

func characterFilterClauses(q spec.CharacterQuery) []any {
	var filters []any
	field := "trait_ids_sfw"
	if q.NSFW {
		field = "trait_ids"
	}
	if len(q.TraitIDs) > 0 {
		if q.TraitMatchAny {
			filters = append(filters, termsClause(field, q.TraitIDs))
		} else {
			for _, id := range q.TraitIDs {
				filters = append(filters, termClause(field, id))
			}
		}
	}
	if len(q.Genders) > 0 {
		filters = append(filters, termsClause("gender", q.Genders))
	}
	return filters
}

func characterSearchSort(sort string) []any {
	idAsc := map[string]any{"catalog_id": map[string]any{"order": "asc"}}
	idDesc := map[string]any{"catalog_id": map[string]any{"order": "desc"}}
	popDesc := map[string]any{"popularity": map[string]any{"order": "desc"}}
	switch sort {
	case "popularity":
		return []any{popDesc, idAsc}
	case "relevance":
		return []any{"_score", popDesc, idAsc}
	case "newest":
		return []any{idDesc}
	default:
		return []any{idAsc}
	}
}

func bucketInt64(key any) (int64, bool) {
	switch v := key.(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case int:
		return int64(v), true
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		return n, err == nil
	default:
		return 0, false
	}
}
