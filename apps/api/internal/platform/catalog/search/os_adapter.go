package search

import (
	"context"
	"encoding/json"
	"log/slog"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/search/osengine"
	"api/internal/platform/catalog/search/spec"
	"api/pkg/config"
)

type osAdapter struct {
	eng    *osengine.Engine
	client *opensearch.Client
}

func NewOpenSearchIndexer(client *opensearch.Client) *Indexer {
	return &Indexer{engine: &osAdapter{eng: osengine.NewEngine(client), client: client}}
}

func newOpenSearchClient(cfg *config.Config) (*opensearch.Client, error) {
	return opensearch.NewClient(cfg.OpenSearch)
}

func (a *osAdapter) EnsureIndexes(ctx context.Context) error {
	return a.eng.EnsureIndexes(ctx)
}

func (a *osAdapter) UpsertBatch(ctx context.Context, uid string, docs []EntityDoc) error {
	if len(docs) == 0 {
		return nil
	}
	bulk := make([]osengine.BulkDoc, len(docs))
	for i, d := range docs {
		bulk[i] = osengine.BulkDoc{ID: d.ID, Source: d}
	}
	return a.eng.Upsert(ctx, uid, bulk)
}

func (a *osAdapter) DeleteBatch(ctx context.Context, uid string, ids []string) error {
	return a.eng.Delete(ctx, uid, ids)
}

func (a *osAdapter) Count(ctx context.Context, uid string) (int64, error) {
	return a.eng.Count(ctx, uid)
}

func (a *osAdapter) Refresh(ctx context.Context, uid string) error {
	return a.eng.Refresh(ctx, uid)
}

func (a *osAdapter) Health(ctx context.Context) error {
	if err := a.client.Health(ctx); err != nil {
		return err
	}
	return a.eng.CheckPlugins(ctx)
}

func (a *osAdapter) RecreateIndex(ctx context.Context, uid string) error {
	return a.eng.RecreateIndex(ctx, uid)
}

func (a *osAdapter) SearchEntities(ctx context.Context, uid string, q spec.EntityQuery) (SearchResult, error) {
	res, err := a.eng.SearchEntities(ctx, uid, q)
	if err != nil {
		return SearchResult{}, err
	}
	hits := make([]EntityDoc, 0, len(res.Hits))
	for _, raw := range res.Hits {
		var d EntityDoc
		if err := json.Unmarshal(raw, &d); err == nil {
			hits = append(hits, d)
		}
	}
	return SearchResult{Hits: hits, Total: res.Total}, nil
}

func (a *osAdapter) SearchWorks(ctx context.Context, q spec.WorksQuery) (WorksResult, error) {
	res, err := a.eng.SearchWorks(ctx, q)
	if err != nil {
		return WorksResult{}, err
	}
	out := WorksResult{
		IDs:    make([]int64, 0, len(res.DocIDs)),
		Total:  res.Total,
		Facets: res.Facets,
	}
	for _, docID := range res.DocIDs {
		id, ok := WorkDocIDToWorkID(docID)
		if !ok {
			slog.Warn("works search: hit with unparseable doc id dropped", "doc_id", docID)
			continue
		}
		out.IDs = append(out.IDs, id)
	}
	return out, nil
}

func (a *osAdapter) SearchCharacters(ctx context.Context, q spec.CharacterQuery) (CharactersResult, error) {
	res, err := a.eng.SearchCharacters(ctx, q)
	if err != nil {
		return CharactersResult{}, err
	}
	out := CharactersResult{
		IDs:   make([]int64, 0, len(res.DocIDs)),
		Total: res.Total,
	}
	for _, docID := range res.DocIDs {
		id, ok := CharacterDocIDToID(docID)
		if !ok {
			slog.Warn("characters search: hit with unparseable doc id dropped", "doc_id", docID)
			continue
		}
		out.IDs = append(out.IDs, id)
	}
	return out, nil
}

func (a *osAdapter) CharacterTraitCounts(ctx context.Context, nsfw bool) (map[int64]int64, error) {
	return a.eng.CharacterTraitCounts(ctx, nsfw)
}
