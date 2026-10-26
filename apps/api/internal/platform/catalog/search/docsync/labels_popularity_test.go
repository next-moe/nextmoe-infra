package docsync

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"testing"

	"api/internal/platform/catalog/model"
	catalogSearch "api/internal/platform/catalog/search"
	"api/internal/testsupport/dbtest"
)

func TestReindexLabelsPopularityCountsLiveWorks(t *testing.T) {
	truncateFacetTables(t)

	brand := &model.CatalogLabel{DisplayName: "ゆずソフト", Lang: "ja", Kind: model.LabelKindGameBrand}
	agency := &model.CatalogLabel{DisplayName: "Angel Note", Lang: "ja", Kind: model.LabelKindGroup}
	for _, l := range []*model.CatalogLabel{brand, agency} {
		if err := facetTestDB.Create(l).Error; err != nil {
			t.Fatalf("create label %s: %v", l.DisplayName, err)
		}
	}

	first := mkWork(t, "first", model.WorkStatusLive, galgameMedium)
	second := mkWork(t, "second", model.WorkStatusLive, galgameMedium)
	stub := mkWork(t, "stub", model.WorkStatusStub, galgameMedium)
	gone := mkWork(t, "gone", model.WorkStatusLive, galgameMedium)
	if err := facetTestDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, gone).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	for _, w := range []int64{first, second, stub, gone} {
		if err := facetTestDB.Create(&model.CatalogWorkLabel{
			WorkID: w, LabelID: brand.ID, Kind: model.WorkLabelKindBrand,
		}).Error; err != nil {
			t.Fatalf("attach brand to %d: %v", w, err)
		}
	}
	if err := facetTestDB.Create(&model.CatalogWorkLabel{
		WorkID: first, LabelID: brand.ID, Kind: model.WorkLabelKindDeveloper,
	}).Error; err != nil {
		t.Fatalf("attach brand as developer: %v", err)
	}

	var role int64
	if err := facetTestDB.Raw(`SELECT id FROM catalog_role WHERE key = 'voice-actor'`).Scan(&role).Error; err != nil || role == 0 {
		t.Fatalf("voice-actor role: id=%d err=%v", role, err)
	}
	for _, name := range []string{"声優A", "声優B", "声優C"} {
		cn := &model.CatalogCreditName{Name: name, Lang: "ja"}
		if err := facetTestDB.Create(cn).Error; err != nil {
			t.Fatalf("create credit name: %v", err)
		}
		if err := facetTestDB.Create(&model.CatalogCredit{
			WorkID: first, CreditNameID: cn.ID, RoleID: role, LabelID: &agency.ID,
		}).Error; err != nil {
			t.Fatalf("create agency credit: %v", err)
		}
	}

	prefix := dbtest.SearchIndexPrefix("reindexlabels")
	client := dbtest.OpenSearchClient(t, prefix)
	idx := catalogSearch.NewOpenSearchIndexer(client)
	if err := idx.EnsureIndexes(context.Background()); err != nil {
		t.Fatalf("ensure indexes: %v", err)
	}
	t.Cleanup(func() { dbtest.SweepSearchIndexes(client, prefix) })

	if _, err := reindexLabels(context.Background(), facetTestDB, idx, 100, nil); err != nil {
		t.Fatalf("reindexLabels: %v", err)
	}
	if err := idx.Refresh(context.Background(), catalogSearch.IndexLabels); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	popularity := func(id int64) float64 {
		t.Helper()
		var doc struct {
			Source catalogSearch.EntityDoc `json:"_source"`
		}
		path := "/" + client.IndexName(catalogSearch.IndexLabels) + "/_doc/b" + strconv.FormatInt(id, 10)
		if err := client.Do(t.Context(), http.MethodGet, path, nil, &doc); err != nil {
			t.Fatalf("get label doc %d: %v", id, err)
		}
		return doc.Source.Popularity
	}
	if got, want := popularity(brand.ID), math.Log1p(2); math.Abs(got-want) > 1e-9 {
		t.Fatalf("brand popularity = %v, want log1p(2) = %v: two live galgame works, the stub and the deleted work excluded, one work under two kinds counted once", got, want)
	}
	if got := popularity(agency.ID); got != 0 {
		t.Fatalf("agency popularity = %v, want 0: credits carrying label_id are not works the company made", got)
	}
}
