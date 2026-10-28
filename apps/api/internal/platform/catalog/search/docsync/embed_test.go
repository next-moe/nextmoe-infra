package docsync

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
)

type fakeEmbedder struct {
	mu     sync.Mutex
	texts  []string
	failOn string
}

func (f *fakeEmbedder) Model() string { return "fake-embed" }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range texts {
		if f.failOn != "" && strings.Contains(s, f.failOn) {
			return nil, errors.New("upstream 503")
		}
	}
	f.texts = append(f.texts, texts...)
	out := make([][]float32, len(texts))
	for i, s := range texts {
		out[i] = []float32{float32(len([]rune(s))), 1, 0, 0}
	}
	return out, nil
}

func TestEmbedWorksReembedsOnlyChangedCards(t *testing.T) {
	truncateFacetTables(t)
	idx, client := syncTestIndex(t, "embedworks")
	ctx := context.Background()
	if err := idx.EnsureVectorIndex(ctx, catalogSearch.IndexWorkVectors, 4); err != nil {
		t.Fatalf("ensure vector index: %v", err)
	}
	a := mkWork(t, "Riddle Joker", model.WorkStatusLive, galgameMedium)
	b := mkWork(t, "Senren Banka", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(`INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
		VALUES (?, 'ja', '学園の秘密', 2, 0)`, a).Error; err != nil {
		t.Fatalf("seed intro: %v", err)
	}

	emb := &fakeEmbedder{}
	run := func() EmbedStats {
		t.Helper()
		stats, err := EmbedWorks(ctx, facetTestDB, idx, emb, EmbedOptions{Batch: 100, EmbedBatch: 1, Concurrency: 2})
		if err != nil {
			t.Fatalf("embed works: %v", err)
		}
		if err := idx.Refresh(ctx, catalogSearch.IndexWorkVectors); err != nil {
			t.Fatalf("refresh: %v", err)
		}
		return stats
	}

	if got := run(); got.Embedded != 2 || got.FilterOnly != 0 {
		t.Fatalf("first run = %+v, want both works embedded", got)
	}
	if got := run(); got.Embedded != 0 || got.FilterOnly != 2 {
		t.Fatalf("second run = %+v, want filter-only refreshes", got)
	}
	src, ok := docSource(t, client, catalogSearch.IndexWorkVectors, catalogSearch.WorkDocID(a))
	if !ok || src["embed_model"] != "fake-embed" || src["text_hash"] == "" {
		t.Fatalf("filter-only refresh must keep the stamps, got %v", src)
	}

	if err := facetTestDB.Exec(`UPDATE catalog_work_intro SET intro = '学園の新しい秘密' WHERE work_id = ?`, a).Error; err != nil {
		t.Fatalf("edit intro: %v", err)
	}
	emb.texts = nil
	if got := run(); got.Embedded != 1 || got.FilterOnly != 1 {
		t.Fatalf("after intro edit = %+v, want exactly the edited work re-embedded", got)
	}
	if len(emb.texts) != 1 {
		t.Fatalf("embedded texts = %q", emb.texts)
	}

	hits, err := idx.SearchWorkVectors(ctx, catalogSearch.IndexWorkVectors, []float32{1, 0, 0, 0}, 5, spec.WorksFilter{OLang: spec.OLang{All: true}})
	if err != nil {
		t.Fatalf("knn search: %v", err)
	}
	if len(hits) != 2 || (hits[0].CatalogID != a && hits[0].CatalogID != b) {
		t.Fatalf("knn hits = %+v", hits)
	}
}

func TestHybridWorksPinsTheExactTitleAndDegradesToLexical(t *testing.T) {
	truncateFacetTables(t)
	idx, _ := syncTestIndex(t, "hybrid")
	ctx := context.Background()
	if err := idx.EnsureVectorIndex(ctx, catalogSearch.IndexWorkVectors, 4); err != nil {
		t.Fatalf("ensure vector index: %v", err)
	}
	exact := mkWork(t, "Senren Banka", model.WorkStatusLive, galgameMedium)
	mkWork(t, "Senren Banka Fandisc", model.WorkStatusLive, galgameMedium)
	mkWork(t, "Riddle Joker", model.WorkStatusLive, galgameMedium)
	if err := Reindex(ctx, facetTestDB, idx, catalogSearch.IndexWorks, 100); err != nil {
		t.Fatalf("reindex works: %v", err)
	}
	if _, err := EmbedWorks(ctx, facetTestDB, idx, &fakeEmbedder{}, EmbedOptions{Batch: 100, EmbedBatch: 4, Concurrency: 1}); err != nil {
		t.Fatalf("embed works: %v", err)
	}
	for _, uid := range []string{catalogSearch.IndexWorks, catalogSearch.IndexWorkVectors} {
		if err := idx.Refresh(ctx, uid); err != nil {
			t.Fatalf("refresh %s: %v", uid, err)
		}
	}
	filter := spec.WorksFilter{OLang: spec.OLang{All: true}}

	got, err := idx.HybridWorks(ctx, &fakeEmbedder{}, "Senren Banka", filter)
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if !got.Hybrid || len(got.IDs) != 3 || got.IDs[0] != exact {
		t.Fatalf("hybrid = %+v, want all three works fused with the exact title first", got)
	}

	lexical, err := idx.SearchWorks(ctx, spec.WorksQuery{Q: "Senren Banka", Filter: filter, Page: 1, Limit: catalogSearch.HybridDepth, SearchIntro: true})
	if err != nil {
		t.Fatalf("lexical: %v", err)
	}
	got, err = idx.HybridWorks(ctx, &fakeEmbedder{failOn: "Senren"}, "Senren Banka", filter)
	if err != nil {
		t.Fatalf("degraded hybrid: %v", err)
	}
	if got.Hybrid || !slices.Equal(got.IDs, lexical.IDs) {
		t.Fatalf("degraded = %+v, want the lexical ranking %v", got, lexical.IDs)
	}
}

func TestSyncerEmbedsEditedWorksAndDropsDepartedVectors(t *testing.T) {
	truncateFacetTables(t)
	idx, client := syncTestIndex(t, "syncvec")
	ctx := context.Background()
	if err := idx.EnsureVectorIndex(ctx, catalogSearch.IndexWorkVectors, 4); err != nil {
		t.Fatalf("ensure vector index: %v", err)
	}
	kept := mkWork(t, "Riddle Joker", model.WorkStatusLive, galgameMedium)
	gone := mkWork(t, "Senren Banka", model.WorkStatusLive, galgameMedium)
	s := NewSyncer(facetTestDB, idx).WithEmbedder(&fakeEmbedder{})
	s.Enqueue(editspec.TypeWork, kept, gone)
	s.Flush(ctx)
	if err := idx.Refresh(ctx, catalogSearch.IndexWorkVectors); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	for _, id := range []int64{kept, gone} {
		if _, ok := docSource(t, client, catalogSearch.IndexWorkVectors, catalogSearch.WorkDocID(id)); !ok {
			t.Fatalf("work %d has no vector after the edit sync", id)
		}
	}

	if err := facetTestDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, gone).Error; err != nil {
		t.Fatalf("soft-delete: %v", err)
	}
	s.Enqueue(editspec.TypeWork, gone)
	s.Flush(ctx)
	if err := idx.Refresh(ctx, catalogSearch.IndexWorkVectors); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if _, ok := docSource(t, client, catalogSearch.IndexWorkVectors, catalogSearch.WorkDocID(gone)); ok {
		t.Fatal("a work that left the population must leave the vector index too")
	}
	if _, ok := docSource(t, client, catalogSearch.IndexWorkVectors, catalogSearch.WorkDocID(kept)); !ok {
		t.Fatal("the untouched work lost its vector")
	}
}

func TestEmbedWorksSkipsAFailingBatchAndFinishesTheRest(t *testing.T) {
	truncateFacetTables(t)
	idx, _ := syncTestIndex(t, "embedskip")
	ctx := context.Background()
	if err := idx.EnsureVectorIndex(ctx, catalogSearch.IndexWorkVectors, 4); err != nil {
		t.Fatalf("ensure vector index: %v", err)
	}
	mkWork(t, "Riddle Joker", model.WorkStatusLive, galgameMedium)
	mkWork(t, "BROKEN card", model.WorkStatusLive, galgameMedium)
	mkWork(t, "Senren Banka", model.WorkStatusLive, galgameMedium)

	emb := &fakeEmbedder{failOn: "BROKEN"}
	stats, err := EmbedWorks(ctx, facetTestDB, idx, emb, EmbedOptions{Batch: 100, EmbedBatch: 1, Concurrency: 1})
	if err == nil || stats.Failed != 1 || stats.Embedded != 2 {
		t.Fatalf("stats = %+v, err = %v; want the broken card skipped, the rest embedded and an error", stats, err)
	}

	emb.failOn = ""
	stats, err = EmbedWorks(ctx, facetTestDB, idx, emb, EmbedOptions{Batch: 100, EmbedBatch: 1, Concurrency: 1})
	if err != nil || stats.Embedded != 1 || stats.FilterOnly != 2 {
		t.Fatalf("rerun stats = %+v, err = %v; want only the skipped card embedded", stats, err)
	}
}
