package service

import (
	"testing"

	"api/internal/platform/catalog/model"
)

func releaseWithEngine(t *testing.T, workID, engineID int64, kind int16, extra string) *model.CatalogRelease {
	t.Helper()
	r := &model.CatalogRelease{WorkID: workID, Kind: kind, EngineID: &engineID, Extra: []byte(extra)}
	if err := testDB.Create(r).Error; err != nil {
		t.Fatalf("create release: %v", err)
	}
	return r
}

func TestWorkEnginesAreReleaseAndWorkEdges(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	shipped := createEngine(t, "Shipped")
	patchOnly := createEngine(t, "PatchOnly")
	fanPort := createEngine(t, "FanPort")
	hidden := createEngine(t, "Hidden")
	curated := createEngine(t, "Curated")

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "engines")
	claimLive(t, w.ID, 9401)
	official := releaseWithEngine(t, w.ID, shipped, model.ReleaseKindDefault, `{"official":true}`)
	releaseWithEngine(t, w.ID, shipped, model.ReleaseKindTrial, `{}`)
	releaseWithEngine(t, w.ID, patchOnly, model.ReleaseKindPatch, `{"official":true}`)
	releaseWithEngine(t, w.ID, fanPort, model.ReleaseKindDefault, `{"official":false}`)
	gone := releaseWithEngine(t, w.ID, hidden, model.ReleaseKindDefault, `{}`)
	if err := testDB.Delete(gone).Error; err != nil {
		t.Fatalf("hide release: %v", err)
	}
	if err := testDB.Create(&model.CatalogWorkEngine{WorkID: w.ID, EngineID: curated, SourceID: 12}).Error; err != nil {
		t.Fatalf("curated edge: %v", err)
	}
	if err := testDB.Create(&model.CatalogWorkEngine{WorkID: w.ID, EngineID: shipped, SourceID: 3}).Error; err != nil {
		t.Fatalf("bangumi edge: %v", err)
	}

	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	var names []string
	for _, e := range rec.Engines {
		names = append(names, e.Name)
		if e.WorkCount != 1 {
			t.Fatalf("engine %s work_count = %d, want 1", e.Name, e.WorkCount)
		}
	}
	if len(names) != 2 || names[0] != "Curated" || names[1] != "Shipped" {
		t.Fatalf("work engines = %v, want [Curated Shipped]: patches, fan ports and hidden releases do not speak for the work", names)
	}

	var sawEngine bool
	for _, r := range rec.Releases {
		if r.ID == official.ID {
			if r.Engine == nil || r.Engine.ID != shipped || r.Engine.Name != "Shipped" {
				t.Fatalf("release %d engine = %+v, want Shipped", r.ID, r.Engine)
			}
			sawEngine = true
		}
	}
	if !sawEngine {
		t.Fatalf("release %d missing from the releases block", official.ID)
	}

	for engine, want := range map[int64]int{shipped: 1, curated: 1, patchOnly: 0, fanPort: 0, hidden: 0} {
		detail, found, err := svc.EngineDetail(ctx, engine, false)
		if err != nil || !found {
			t.Fatalf("EngineDetail %d: found=%v err=%v", engine, found, err)
		}
		if detail.WorkCount != want {
			t.Fatalf("engine %s work_count = %d, want %d", detail.Name, detail.WorkCount, want)
		}
		assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", EngineID: engine}, want)
	}
}
