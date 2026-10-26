package docsync

import (
	"context"
	"net/http"
	"testing"
	"time"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/testsupport/dbtest"
)

func TestReindexWorksPurgesSoftDeleted(t *testing.T) {
	truncateFacetTables(t)
	live := mkWork(t, "purge-live", 0, galgameMedium)
	dead := mkWork(t, "purge-dead", 0, galgameMedium)
	if err := facetTestDB.Delete(&model.CatalogWork{}, dead).Error; err != nil {
		t.Fatalf("soft-delete work %d: %v", dead, err)
	}

	prefix := dbtest.SearchIndexPrefix("reindex")
	client := dbtest.OpenSearchClient(t, prefix)
	idx := catalogSearch.NewOpenSearchIndexer(client)
	if err := idx.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() { dbtest.SweepSearchIndexes(client, prefix) })
	docs := []catalogSearch.EntityDoc{
		catalogSearch.BuildWorkDoc(catalogSearch.WorkDocInput{ID: live, DisplayName: "purge-live", OLang: "ja"}),
		catalogSearch.BuildWorkDoc(catalogSearch.WorkDocInput{ID: dead, DisplayName: "purge-dead", OLang: "ja"}),
	}
	if err := idx.UpsertBatch(context.Background(), catalogSearch.IndexWorks, docs); err != nil {
		t.Fatalf("seed index: %v", err)
	}
	waitWorkDocs(t, idx, 2)

	if _, err := reindexWorks(context.Background(), facetTestDB, idx, 100, nil); err != nil {
		t.Fatalf("reindexWorks: %v", err)
	}
	if err := idx.Refresh(context.Background(), catalogSearch.IndexWorks); err != nil {
		t.Fatalf("refresh after reindex: %v", err)
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		deadErr := client.Do(context.Background(), http.MethodGet,
			"/"+client.IndexName(catalogSearch.IndexWorks)+"/_doc/"+catalogSearch.WorkDocID(dead), nil, nil)
		liveErr := client.Do(context.Background(), http.MethodGet,
			"/"+client.IndexName(catalogSearch.IndexWorks)+"/_doc/"+catalogSearch.WorkDocID(live), nil, nil)
		if opensearch.IsNotFound(deadErr) && liveErr == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("purge did not converge: dead doc err=%v (want not found), live doc err=%v (want nil)",
				deadErr, liveErr)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func waitWorkDocs(t *testing.T, idx *catalogSearch.Indexer, want int64) {
	t.Helper()
	if err := idx.Refresh(t.Context(), catalogSearch.IndexWorks); err != nil {
		t.Fatalf("refresh works index: %v", err)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		n, err := idx.Count(t.Context(), catalogSearch.IndexWorks)
		if err == nil && n == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("works index did not reach %d docs (last err %v)", want, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
