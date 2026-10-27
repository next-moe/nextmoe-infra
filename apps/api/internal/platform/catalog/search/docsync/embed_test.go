package docsync

import (
	"context"
	"sync"
	"testing"

	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/catalog/search/spec"
)

type fakeEmbedder struct {
	mu    sync.Mutex
	texts []string
}

func (f *fakeEmbedder) Model() string { return "fake-embed" }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
		stats, err := EmbedWorks(ctx, facetTestDB, idx, emb, 100, 1, 2, nil)
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

	hits, err := idx.SearchWorkVectors(ctx, []float32{1, 0, 0, 0}, 5, spec.WorksFilter{OLang: spec.OLang{All: true}})
	if err != nil {
		t.Fatalf("knn search: %v", err)
	}
	if len(hits) != 2 || (hits[0].CatalogID != a && hits[0].CatalogID != b) {
		t.Fatalf("knn hits = %+v", hits)
	}
}
