package docsync

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	catalogSearch "api/internal/platform/catalog/search"

	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type EmbedStats struct {
	Scanned    int
	Embedded   int
	FilterOnly int
}

func loadTagNames(db *gorm.DB) (map[int64]string, error) {
	var rows []struct {
		ID   int64  `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if err := db.Raw(`SELECT id, name FROM catalog_tag`).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("tag names: %w", err)
	}
	out := make(map[int64]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out, nil
}

func EmbedWorks(ctx context.Context, db *gorm.DB, idx *catalogSearch.Indexer, emb catalogSearch.Embedder, batch, embedBatch, concurrency int, ids []int64) (EmbedStats, error) {
	var stats EmbedStats
	tagNames, err := loadTagNames(db)
	if err != nil {
		return stats, err
	}
	var s scope
	if ids != nil {
		s = scope(slices.Compact(slices.Sorted(slices.Values(ids))))
		batch = max(len(s), 1)
	}
	model := emb.Model()
	err = forEachWorkInput(db, s, batch, func(ins []catalogSearch.WorkDocInput) error {
		docIDs := make([]string, len(ins))
		for i, in := range ins {
			docIDs[i] = catalogSearch.WorkDocID(in.ID)
		}
		stamps, err := idx.VectorStamps(ctx, catalogSearch.IndexWorkVectors, docIDs)
		if err != nil {
			return err
		}
		var fresh, stale []catalogSearch.VectorDoc
		var texts []string
		for _, in := range ins {
			doc := catalogSearch.NewWorkVectorDoc(catalogSearch.BuildWorkDoc(in))
			var names []string
			for _, id := range in.TagIDs {
				if n := tagNames[id]; n != "" {
					names = append(names, n)
				}
			}
			text := catalogSearch.WorkCardText(in, names)
			doc.EmbedModel, doc.TextHash = model, catalogSearch.CardHash(model, text)
			if st, ok := stamps[doc.ID]; ok && st.EmbedModel == model && st.TextHash == doc.TextHash {
				fresh = append(fresh, doc)
				continue
			}
			stale = append(stale, doc)
			texts = append(texts, text)
		}
		stats.Scanned += len(ins)
		if err := idx.UpdateVectorFilters(ctx, catalogSearch.IndexWorkVectors, fresh); err != nil {
			return err
		}
		stats.FilterOnly += len(fresh)
		g, gctx := errgroup.WithContext(ctx)
		g.SetLimit(concurrency)
		for start := 0; start < len(stale); start += embedBatch {
			end := min(start+embedBatch, len(stale))
			chunk, chunkTexts := stale[start:end], texts[start:end]
			g.Go(func() error {
				vecs, err := emb.Embed(gctx, chunkTexts)
				if err != nil {
					return fmt.Errorf("embed works %d..%d: %w", chunk[0].CatalogID, chunk[len(chunk)-1].CatalogID, err)
				}
				for i := range chunk {
					chunk[i].Vector = vecs[i]
				}
				return idx.UpsertVectors(gctx, catalogSearch.IndexWorkVectors, chunk)
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		stats.Embedded += len(stale)
		slog.Info("embed works page", "scanned", stats.Scanned, "embedded", stats.Embedded, "filter_only", stats.FilterOnly)
		return nil
	})
	return stats, err
}
