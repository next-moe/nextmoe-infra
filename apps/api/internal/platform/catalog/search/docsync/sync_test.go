package docsync

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"api/internal/infrastructure/opensearch"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/platform/editing"
	"api/internal/testsupport/dbtest"
)

func syncTestIndex(t *testing.T, suite string) (*catalogSearch.Indexer, *opensearch.Client) {
	t.Helper()
	prefix := dbtest.SearchIndexPrefix(suite)
	client := dbtest.OpenSearchClient(t, prefix)
	idx := catalogSearch.NewOpenSearchIndexer(client)
	if err := idx.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() { dbtest.SweepSearchIndexes(client, prefix) })
	return idx, client
}

func docSource(t *testing.T, client *opensearch.Client, uid, docID string) (map[string]any, bool) {
	t.Helper()
	var resp struct {
		Source map[string]any `json:"_source"`
	}
	err := client.Do(context.Background(), http.MethodGet, "/"+client.IndexName(uid)+"/_doc/"+docID, nil, &resp)
	if opensearch.IsNotFound(err) {
		return nil, false
	}
	if err != nil {
		t.Fatalf("get %s/%s: %v", uid, docID, err)
	}
	return resp.Source, true
}

func canon(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestScopedSyncBuildsTheSameDocumentAsAFullReindex(t *testing.T) {
	truncateFacetTables(t)
	if err := facetTestDB.Exec("TRUNCATE catalog_work_rating RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate ratings: %v", err)
	}
	target := mkWork(t, "Riddle Joker", model.WorkStatusLive, galgameMedium)
	other := mkWork(t, "Senren Banka", model.WorkStatusLive, galgameMedium)
	mkRelease(t, target, 2018, 3, 30)
	label := &model.CatalogLabel{DisplayName: "ゆずソフト", Lang: "ja", Kind: model.LabelKindGameBrand}
	if err := facetTestDB.Create(label).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	for _, w := range []int64{target, other} {
		if err := facetTestDB.Create(&model.CatalogWorkLabel{WorkID: w, LabelID: label.ID, Kind: model.WorkLabelKindBrand}).Error; err != nil {
			t.Fatalf("attach label: %v", err)
		}
	}
	for w, score := range map[int64]float64{target: 8.4, other: 6.0} {
		if err := facetTestDB.Exec(`INSERT INTO catalog_work_rating (work_id, source_id, score, vote_count, created_at, updated_at)
			SELECT ?, id, ?, 40, now(), now() FROM catalog_source WHERE key = 'bangumi'`, w, score).Error; err != nil {
			t.Fatalf("rate: %v", err)
		}
	}

	ctx := context.Background()
	full, fullClient := syncTestIndex(t, "syncfull")
	scoped, scopedClient := syncTestIndex(t, "syncscoped")
	for _, uid := range []string{catalogSearch.IndexWorks, catalogSearch.IndexLabels} {
		if err := Reindex(ctx, facetTestDB, full, uid, 100); err != nil {
			t.Fatalf("full reindex %s: %v", uid, err)
		}
	}
	if err := Sync(ctx, facetTestDB, scoped, catalogSearch.IndexWorks, []int64{target}); err != nil {
		t.Fatalf("sync work: %v", err)
	}
	if err := Sync(ctx, facetTestDB, scoped, catalogSearch.IndexLabels, []int64{label.ID}); err != nil {
		t.Fatalf("sync label: %v", err)
	}

	for uid, docID := range map[string]string{
		catalogSearch.IndexWorks:  catalogSearch.WorkDocID(target),
		catalogSearch.IndexLabels: "b" + canon(t, label.ID),
	} {
		want, ok := docSource(t, fullClient, uid, docID)
		if !ok {
			t.Fatalf("full reindex did not write %s/%s", uid, docID)
		}
		got, ok := docSource(t, scopedClient, uid, docID)
		if !ok {
			t.Fatalf("scoped sync did not write %s/%s", uid, docID)
		}
		if canon(t, got) != canon(t, want) {
			t.Fatalf("%s/%s differs\nscoped: %s\nfull:   %s", uid, docID, canon(t, got), canon(t, want))
		}
	}
	if _, ok := docSource(t, scopedClient, catalogSearch.IndexWorks, catalogSearch.WorkDocID(other)); ok {
		t.Fatal("a scoped sync wrote a work outside its scope")
	}
}

func TestSyncRemovesDocumentsThatLeftTheIndex(t *testing.T) {
	truncateFacetTables(t)
	ctx := context.Background()
	idx, client := syncTestIndex(t, "syncgone")

	work := mkWork(t, "Doomed", model.WorkStatusLive, galgameMedium)
	label := &model.CatalogLabel{DisplayName: "Doomed Brand", Lang: "en", Kind: model.LabelKindGameBrand}
	if err := facetTestDB.Create(label).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	if err := Sync(ctx, facetTestDB, idx, catalogSearch.IndexWorks, []int64{work}); err != nil {
		t.Fatal(err)
	}
	if err := Sync(ctx, facetTestDB, idx, catalogSearch.IndexLabels, []int64{label.ID}); err != nil {
		t.Fatal(err)
	}
	labelDoc := "b" + canon(t, label.ID)
	if _, ok := docSource(t, client, catalogSearch.IndexWorks, catalogSearch.WorkDocID(work)); !ok {
		t.Fatal("live work not indexed")
	}
	if _, ok := docSource(t, client, catalogSearch.IndexLabels, labelDoc); !ok {
		t.Fatal("live label not indexed")
	}

	if err := facetTestDB.Exec(`UPDATE catalog_work SET status = ? WHERE id = ?`, model.WorkStatusStub, work).Error; err != nil {
		t.Fatal(err)
	}
	if err := facetTestDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id = ?`, label.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := Sync(ctx, facetTestDB, idx, catalogSearch.IndexWorks, []int64{work}); err != nil {
		t.Fatal(err)
	}
	if err := Sync(ctx, facetTestDB, idx, catalogSearch.IndexLabels, []int64{label.ID, label.ID + 1000}); err != nil {
		t.Fatal(err)
	}
	if _, ok := docSource(t, client, catalogSearch.IndexWorks, catalogSearch.WorkDocID(work)); ok {
		t.Fatal("a work that left the population is still indexed")
	}
	if _, ok := docSource(t, client, catalogSearch.IndexLabels, labelDoc); ok {
		t.Fatal("a soft-deleted label is still indexed")
	}
}

func TestSyncerIndexesWhatAnEditTouched(t *testing.T) {
	truncateFacetTables(t)
	for _, tbl := range []string{"catalog_credit_name", "catalog_person"} {
		if err := facetTestDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	ctx := context.Background()
	idx, client := syncTestIndex(t, "syncer")

	engine := &model.CatalogEngine{Name: "GPK2", Aliases: []byte("[]")}
	if err := facetTestDB.Create(engine).Error; err != nil {
		t.Fatalf("create engine: %v", err)
	}
	work := mkWork(t, "Released Work", model.WorkStatusLive, galgameMedium)
	release := &model.CatalogRelease{WorkID: work, Kind: model.ReleaseKindDefault}
	if err := facetTestDB.Create(release).Error; err != nil {
		t.Fatalf("create release: %v", err)
	}
	person := &model.CatalogPerson{DisplayName: "Someone"}
	if err := facetTestDB.Create(person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	name := &model.CatalogCreditName{Name: "Someone", Lang: "en", PersonID: &person.ID}
	if err := facetTestDB.Create(name).Error; err != nil {
		t.Fatalf("create credit name: %v", err)
	}

	s := NewSyncer(facetTestDB, idx)
	s.Observe(ctx, editspec.TypeEngine, editing.MergeEvent{EntityID: engine.ID})
	s.Observe(ctx, editspec.TypeRelease, editing.MergeEvent{EntityID: release.ID})
	s.Observe(ctx, editspec.TypePerson, editing.MergeEvent{EntityID: person.ID})
	s.Observe(ctx, editspec.TypeWalkthrough, editing.MergeEvent{EntityID: 1})
	s.Flush(ctx)

	doc, ok := docSource(t, client, catalogSearch.IndexEngines, "e"+canon(t, engine.ID))
	if !ok || doc["name_other"] != "GPK2" {
		t.Fatalf("engine edit not indexed: %v", doc)
	}
	if _, ok := docSource(t, client, catalogSearch.IndexWorks, catalogSearch.WorkDocID(work)); !ok {
		t.Fatal("a release edit did not reindex its work")
	}
	if _, ok := docSource(t, client, catalogSearch.IndexCreditNames, "n"+canon(t, name.ID)); !ok {
		t.Fatal("a person edit did not reindex its credit names")
	}

	if err := facetTestDB.Model(engine).Update("name", "GPK3").Error; err != nil {
		t.Fatal(err)
	}
	s.Enqueue(editspec.TypeEngine, engine.ID)
	s.Flush(ctx)
	if doc, _ := docSource(t, client, catalogSearch.IndexEngines, "e"+canon(t, engine.ID)); doc["name_other"] != "GPK3" {
		t.Fatalf("engine rename not indexed: %v", doc)
	}
}

func TestHiddenPersonLinkIsNotIndexed(t *testing.T) {
	for _, tbl := range []string{"catalog_credit_name", "catalog_person"} {
		if err := facetTestDB.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	ctx := context.Background()
	idx, client := syncTestIndex(t, "synchidden")
	person := &model.CatalogPerson{DisplayName: "Someone"}
	if err := facetTestDB.Create(person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	public := &model.CatalogCreditName{Name: "Public Name", Lang: "en", PersonID: &person.ID, LinkVisibility: model.LinkVisibilityPublic}
	hidden := &model.CatalogCreditName{Name: "Hidden Name", Lang: "en", PersonID: &person.ID, LinkVisibility: model.LinkVisibilityHidden}
	for _, n := range []*model.CatalogCreditName{public, hidden} {
		if err := facetTestDB.Create(n).Error; err != nil {
			t.Fatalf("create credit name: %v", err)
		}
	}
	if err := Sync(ctx, facetTestDB, idx, catalogSearch.IndexCreditNames, []int64{public.ID, hidden.ID}); err != nil {
		t.Fatal(err)
	}
	doc, _ := docSource(t, client, catalogSearch.IndexCreditNames, "n"+canon(t, public.ID))
	if doc["person_id"] != float64(person.ID) {
		t.Fatalf("public link lost its person: %v", doc)
	}
	doc, ok := docSource(t, client, catalogSearch.IndexCreditNames, "n"+canon(t, hidden.ID))
	if !ok {
		t.Fatal("hidden-link credit name not indexed")
	}
	if _, leaked := doc["person_id"]; leaked {
		t.Fatalf("a hidden person link reached the index: %v", doc)
	}
}
