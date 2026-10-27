package osengine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/spec"
)

const (
	VectorSchemaVersion   = 1
	IndexWorkVectors      = "catalog_work_vectors"
	IndexCharacterVectors = "catalog_character_vectors"
)

func vectorFilterProperties(uid string) (map[string]any, error) {
	long := map[string]any{"type": "long"}
	integer := map[string]any{"type": "integer"}
	switch uid {
	case IndexWorkVectors:
		return map[string]any{
			"content_rating": integer,
			"claimed":        map[string]any{"type": "boolean"},
			"claim_state":    keywordField(),
			"content_limit":  keywordField(),
			"olang":          keywordField(),
			"tag_ids":        long,
			"label_ids":      long,
			"engine_ids":     long,
			"series_ids":     long,
			"released_ord":   integer,
		}, nil
	case IndexCharacterVectors:
		return map[string]any{
			"gender":        integer,
			"trait_ids":     long,
			"trait_ids_sfw": long,
		}, nil
	default:
		return nil, fmt.Errorf("unknown vector index %q", uid)
	}
}

func VectorIndexBody(uid string, dims int) (map[string]any, error) {
	props, err := vectorFilterProperties(uid)
	if err != nil {
		return nil, err
	}
	props["vector"] = map[string]any{
		"type":              "knn_vector",
		"dimension":         dims,
		"space_type":        "cosinesimil",
		"mode":              "on_disk",
		"compression_level": "16x",
		"method": map[string]any{
			"name": "hnsw", "engine": "faiss",
			"parameters": map[string]any{"m": 16, "ef_construction": 128},
		},
	}
	props["id"] = keywordField()
	props["catalog_id"] = map[string]any{"type": "long"}
	props["embed_model"] = keywordField()
	props["text_hash"] = keywordField()
	return map[string]any{
		"settings": map[string]any{
			"index": map[string]any{
				"knn":                true,
				"number_of_shards":   1,
				"number_of_replicas": 0,
			},
		},
		"mappings": map[string]any{
			"dynamic":    false,
			"_meta":      map[string]any{"schema_version": VectorSchemaVersion, "dims": dims},
			"properties": props,
		},
	}, nil
}

func (e *Engine) EnsureVectorIndex(ctx context.Context, uid string, dims int) error {
	body, err := VectorIndexBody(uid, dims)
	if err != nil {
		return err
	}
	name := e.client.IndexName(uid)
	var got map[string]any
	err = e.client.Do(ctx, http.MethodGet, "/"+name, nil, &got)
	if opensearch.IsNotFound(err) {
		if err := e.client.Do(ctx, http.MethodPut, "/"+name, body, nil); err != nil && !isResourceAlreadyExists(err) {
			return fmt.Errorf("create index %s: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get index %s: %w", name, err)
	}
	if ver, ok := mappingSchemaVersion(got); !ok || ver != VectorSchemaVersion {
		return fmt.Errorf("index %s schema_version mismatch (got %d, want %d); recreate it and rerun embed-catalog", name, ver, VectorSchemaVersion)
	}
	return nil
}

func (e *Engine) UpdatePartial(ctx context.Context, uid string, docs []BulkDoc) error {
	if len(docs) == 0 {
		return nil
	}
	name := e.client.IndexName(uid)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	for _, d := range docs {
		if err := enc.Encode(map[string]any{"update": map[string]any{"_index": name, "_id": d.ID}}); err != nil {
			return err
		}
		if err := enc.Encode(map[string]any{"doc": d.Source}); err != nil {
			return err
		}
	}
	return e.client.BulkNDJSON(ctx, buf.Bytes())
}

type VectorStamp struct {
	EmbedModel string `json:"embed_model"`
	TextHash   string `json:"text_hash"`
}

func (e *Engine) VectorStamps(ctx context.Context, uid string, ids []string) (map[string]VectorStamp, error) {
	out := make(map[string]VectorStamp, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var resp struct {
		Docs []struct {
			ID     string      `json:"_id"`
			Found  bool        `json:"found"`
			Source VectorStamp `json:"_source"`
		} `json:"docs"`
	}
	body := map[string]any{"ids": ids}
	path := "/" + e.client.IndexName(uid) + "/_mget?_source_includes=embed_model,text_hash"
	if err := e.client.Do(ctx, http.MethodPost, path, body, &resp); err != nil {
		if opensearch.IsNotFound(err) {
			return out, nil
		}
		return nil, err
	}
	for _, d := range resp.Docs {
		if d.Found {
			out[d.ID] = d.Source
		}
	}
	return out, nil
}

type VectorHit struct {
	CatalogID int64
	Score     float64
}

func (e *Engine) SearchWorkVectors(ctx context.Context, uid string, vector []float32, k int, f spec.WorksFilter) ([]VectorHit, error) {
	return e.searchVectors(ctx, uid, vector, k, filterClauses(f))
}

func (e *Engine) SearchCharacterVectors(ctx context.Context, uid string, vector []float32, k int, q spec.CharacterQuery) ([]VectorHit, error) {
	return e.searchVectors(ctx, uid, vector, k, characterFilterClauses(q))
}

func (e *Engine) searchVectors(ctx context.Context, uid string, vector []float32, k int, clauses []any) ([]VectorHit, error) {
	knn := map[string]any{"vector": vector, "k": k}
	if len(clauses) > 0 {
		knn["filter"] = map[string]any{"bool": map[string]any{"filter": clauses}}
	}
	body := map[string]any{
		"size":    k,
		"_source": []string{"catalog_id"},
		"query":   map[string]any{"knn": map[string]any{"vector": knn}},
	}
	var resp struct {
		Hits struct {
			Hits []struct {
				Score  float64 `json:"_score"`
				Source struct {
					CatalogID int64 `json:"catalog_id"`
				} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := e.client.Do(ctx, http.MethodPost, "/"+e.client.IndexName(uid)+"/_search", body, &resp); err != nil {
		return nil, err
	}
	hits := make([]VectorHit, len(resp.Hits.Hits))
	for i, h := range resp.Hits.Hits {
		hits[i] = VectorHit{CatalogID: h.Source.CatalogID, Score: h.Score}
	}
	return hits, nil
}
