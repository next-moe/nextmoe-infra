package search

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"sync"
	"time"

	"api/internal/platform/catalog/search/spec"
)

const (
	HybridDepth        = 100
	hybridExactLimit   = 5
	hybridEmbedTimeout = 3 * time.Second
)

var errNoEmbedder = errors.New("no embedder configured")

type HybridWorksResult struct {
	IDs    []int64
	Hybrid bool
}

func (i *Indexer) HybridWorks(ctx context.Context, emb Embedder, text string, f spec.WorksFilter) (HybridWorksResult, error) {
	var (
		lex              WorksResult
		exact            []int64
		knn              []VectorHit
		lexErr, exactErr error
		vecErr           = errNoEmbedder
		wg               sync.WaitGroup
	)
	wg.Go(func() {
		lex, lexErr = i.SearchWorks(ctx, spec.WorksQuery{Q: text, Filter: f, Page: 1, Limit: HybridDepth, SearchIntro: true})
	})
	wg.Go(func() { exact, exactErr = i.engine.ExactTitleWorks(ctx, text, hybridExactLimit, f) })
	if emb != nil {
		wg.Go(func() {
			ectx, cancel := context.WithTimeout(ctx, hybridEmbedTimeout)
			defer cancel()
			vecs, err := emb.Embed(ectx, []string{WorkQueryText(text)})
			if err != nil {
				vecErr = err
				return
			}
			knn, vecErr = i.SearchWorkVectors(ctx, IndexWorkVectors, vecs[0], HybridDepth, f)
		})
	}
	wg.Wait()
	if lexErr != nil {
		return HybridWorksResult{}, lexErr
	}
	if exactErr != nil {
		slog.Warn("hybrid works: exact-title lane failed", "error", exactErr)
		exact = nil
	}
	if vecErr != nil {
		slog.Warn("hybrid works: semantic lane unavailable, serving lexical", "error", vecErr)
		return HybridWorksResult{IDs: pinFirst(exact, lex.IDs)}, nil
	}
	knnIDs := make([]int64, len(knn))
	for j, h := range knn {
		knnIDs[j] = h.CatalogID
	}
	fused := FuseRRF(DefaultRRFK, RankedList{IDs: lex.IDs, Weight: 1}, RankedList{IDs: knnIDs, Weight: 1})
	return HybridWorksResult{IDs: pinFirst(exact, fused), Hybrid: true}, nil
}

func pinFirst(pinned, ranked []int64) []int64 {
	out := slices.Clone(pinned)
	for _, id := range ranked {
		if !slices.Contains(pinned, id) {
			out = append(out, id)
		}
	}
	if len(out) > HybridDepth {
		out = out[:HybridDepth]
	}
	return out
}
