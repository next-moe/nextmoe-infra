package osengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
)

var requiredPlugins = []string{
	"analysis-icu",
	"analysis-kuromoji",
	"analysis-pinyin",
}

type Engine struct {
	client *opensearch.Client
}

func NewEngine(client *opensearch.Client) *Engine {
	return &Engine{client: client}
}

type BulkDoc struct {
	ID     string
	Source any
}

type WorksResult struct {
	DocIDs []string
	Total  int64
	Facets map[string]map[string]int64
}

type EntityResult struct {
	Hits  []json.RawMessage
	Total int64
}

func (e *Engine) CheckPlugins(ctx context.Context) error {
	installed, err := e.client.Plugins(ctx)
	if err != nil {
		return err
	}
	have := make(map[string]struct{}, len(installed))
	for _, name := range installed {
		have[name] = struct{}{}
	}
	var missing []string
	for _, name := range requiredPlugins {
		if _, ok := have[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("opensearch missing required plugin(s): %s", strings.Join(missing, ", "))
	}
	return nil
}

func (e *Engine) EnsureIndexes(ctx context.Context) error {
	for _, uid := range IndexUIDs {
		if err := e.ensureIndex(ctx, uid); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) ensureIndex(ctx context.Context, uid string) error {
	name := e.client.IndexName(uid)
	var got map[string]any
	err := e.client.Do(ctx, http.MethodGet, "/"+name, nil, &got)
	if opensearch.IsNotFound(err) {
		body, err := IndexBody(uid)
		if err != nil {
			return err
		}
		if err := e.client.Do(ctx, http.MethodPut, "/"+name, body, nil); err != nil {
			if !isResourceAlreadyExists(err) {
				return fmt.Errorf("create index %s: %w", name, err)
			}
			if err := e.client.Do(ctx, http.MethodGet, "/"+name, nil, &got); err != nil {
				return fmt.Errorf("get index %s: %w", name, err)
			}
		} else {
			return nil
		}
	} else if err != nil {
		return fmt.Errorf("get index %s: %w", name, err)
	}
	gotVer, ok := mappingSchemaVersion(got)
	if !ok || gotVer != SchemaVersion {
		return fmt.Errorf("index %s schema_version mismatch (got %d, want %d); run reindex-catalog -recreate", name, gotVer, SchemaVersion)
	}
	return nil
}

func isResourceAlreadyExists(err error) bool {
	var ae *opensearch.APIError
	return errors.As(err, &ae) && strings.Contains(ae.Body, "resource_already_exists_exception")
}

func (e *Engine) RecreateIndex(ctx context.Context, uid string) error {
	body, err := IndexBody(uid)
	if err != nil {
		return err
	}
	name := e.client.IndexName(uid)
	if err := e.client.Do(ctx, http.MethodDelete, "/"+name, nil, nil); err != nil && !opensearch.IsNotFound(err) {
		return fmt.Errorf("delete index %s: %w", name, err)
	}
	if err := e.client.Do(ctx, http.MethodPut, "/"+name, body, nil); err != nil {
		return fmt.Errorf("create index %s: %w", name, err)
	}
	return nil
}

func (e *Engine) Upsert(ctx context.Context, uid string, docs []BulkDoc) error {
	if len(docs) == 0 {
		return nil
	}
	name := e.client.IndexName(uid)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, d := range docs {
		if err := enc.Encode(map[string]any{
			"index": map[string]any{"_index": name, "_id": d.ID},
		}); err != nil {
			return err
		}
		if err := enc.Encode(d.Source); err != nil {
			return err
		}
	}
	return e.client.BulkNDJSON(ctx, buf.Bytes())
}

func (e *Engine) Delete(ctx context.Context, uid string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	name := e.client.IndexName(uid)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, id := range ids {
		if err := enc.Encode(map[string]any{
			"delete": map[string]any{"_index": name, "_id": id},
		}); err != nil {
			return err
		}
	}
	return e.client.BulkNDJSON(ctx, buf.Bytes())
}

func (e *Engine) Count(ctx context.Context, uid string) (int64, error) {
	var out struct {
		Count int64 `json:"count"`
	}
	if err := e.client.Do(ctx, http.MethodGet, "/"+e.client.IndexName(uid)+"/_count", nil, &out); err != nil {
		return 0, err
	}
	return out.Count, nil
}

func (e *Engine) Refresh(ctx context.Context, uid string) error {
	return e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(uid)+"/_refresh", nil, nil)
}

func (e *Engine) SearchWorks(ctx context.Context, q spec.WorksQuery) (WorksResult, error) {
	body, dropHits := worksSearchBody(q)
	var resp osSearchResponse
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(IndexWorks)+"/_search", body, &resp); err != nil {
		return WorksResult{}, err
	}
	out := WorksResult{
		DocIDs: []string{},
		Total:  parseTotal(resp.Hits.Total),
		Facets: parseFacets(resp.Aggregations),
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

func (e *Engine) SearchEntities(ctx context.Context, uid string, q spec.EntityQuery) (EntityResult, error) {
	body, dropHits := entitySearchBody(q)
	var resp osSearchResponse
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(uid)+"/_search", body, &resp); err != nil {
		return EntityResult{}, err
	}
	out := EntityResult{
		Hits:  []json.RawMessage{},
		Total: parseTotal(resp.Hits.Total),
	}
	if dropHits {
		return out, nil
	}
	out.Hits = make([]json.RawMessage, 0, len(resp.Hits.Hits))
	for _, h := range resp.Hits.Hits {
		if len(h.Source) == 0 || string(h.Source) == "null" {
			continue
		}
		out.Hits = append(out.Hits, h.Source)
	}
	return out, nil
}

func worksSearchBody(q spec.WorksQuery) (map[string]any, bool) {
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
	filters := filterClauses(q.Filter)
	body := map[string]any{
		"query":            buildQuery(text, q.SearchIntro, false, filters),
		"from":             from,
		"size":             size,
		"sort":             searchSort(q.SortLane, text != ""),
		"track_total_hits": true,
		"_source":          false,
	}
	if len(q.FacetAttrs) > 0 {
		aggs := make(map[string]any, len(q.FacetAttrs))
		for _, attr := range q.FacetAttrs {
			aggs[attr] = map[string]any{
				"terms": map[string]any{"field": attr, "size": 100},
			}
		}
		body["aggs"] = aggs
	}
	return body, dropHits
}

func entitySearchBody(q spec.EntityQuery) (map[string]any, bool) {
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
	var filters []any
	if q.ContentRatingNot != nil {
		filters = append(filters, mustNotTerm("content_rating", *q.ContentRatingNot))
	}
	if q.SexualNot != nil {
		filters = append(filters, mustNotTerm("sexual", *q.SexualNot))
	}
	return map[string]any{
		"query":            buildQuery(text, false, true, filters),
		"from":             from,
		"size":             size,
		"sort":             searchSort("", text != ""),
		"track_total_hits": true,
	}, dropHits
}

func buildQuery(text string, searchIntro, entity bool, filters []any) any {
	if text != "" {
		bq := map[string]any{
			"should":               titleShould(text, searchIntro, entity),
			"minimum_should_match": 1,
		}
		if len(filters) > 0 {
			bq["filter"] = filters
		}
		return map[string]any{"bool": bq}
	}
	if len(filters) > 0 {
		return map[string]any{"bool": map[string]any{"filter": filters}}
	}
	return map[string]any{"match_all": map[string]any{}}
}

func searchSort(lane string, hasText bool) []any {
	if field, order, ok := parseSortLane(lane); ok {
		clause := map[string]any{field: map[string]any{"order": order}}
		if field == "popularity" {
			return []any{clause}
		}
		return []any{clause, map[string]any{"popularity": map[string]any{"order": "desc"}}}
	}
	if hasText {
		return []any{"_score", map[string]any{"popularity": map[string]any{"order": "desc"}}}
	}
	return []any{map[string]any{"popularity": map[string]any{"order": "desc"}}}
}

func parseSortLane(lane string) (field, order string, ok bool) {
	field, order, found := strings.Cut(lane, ":")
	if !found {
		return "", "", false
	}
	switch field {
	case "released_ord", "updated_ts", "popularity":
	default:
		return "", "", false
	}
	if order != "asc" && order != "desc" {
		return "", "", false
	}
	return field, order, true
}

type osSearchResponse struct {
	Hits         osHits                `json:"hits"`
	Aggregations map[string]osTermsAgg `json:"aggregations"`
}

type osHits struct {
	Total json.RawMessage `json:"total"`
	Hits  []osHit         `json:"hits"`
}

type osHit struct {
	ID     string          `json:"_id"`
	Source json.RawMessage `json:"_source"`
}

type osTermsAgg struct {
	Buckets []osBucket `json:"buckets"`
}

type osBucket struct {
	Key         any    `json:"key"`
	KeyAsString string `json:"key_as_string"`
	DocCount    int64  `json:"doc_count"`
}

func parseTotal(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var obj struct {
		Value int64 `json:"value"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Value
	}
	return 0
}

func parseFacets(aggs map[string]osTermsAgg) map[string]map[string]int64 {
	if len(aggs) == 0 {
		return nil
	}
	out := make(map[string]map[string]int64, len(aggs))
	for attr, agg := range aggs {
		buckets := make(map[string]int64, len(agg.Buckets))
		for _, b := range agg.Buckets {
			buckets[facetKey(b.KeyAsString, b.Key)] = b.DocCount
		}
		out[attr] = buckets
	}
	return out
}

func facetKey(keyAsString string, key any) string {
	if keyAsString != "" {
		return keyAsString
	}
	switch v := key.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		if v == float64(int64(v)) {
			return strconv.FormatInt(int64(v), 10)
		}
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func mappingSchemaVersion(got map[string]any) (int, bool) {
	for _, v := range got {
		idx, ok := v.(map[string]any)
		if !ok {
			continue
		}
		mappings, ok := idx["mappings"].(map[string]any)
		if !ok {
			return 0, false
		}
		meta, _ := mappings["_meta"].(map[string]any)
		if meta == nil {
			return 0, false
		}
		return anyInt(meta["schema_version"])
	}
	return 0, false
}

func anyInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}
