package editspec_test

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func titlesValue(title string) []any {
	return []any{map[string]any{"lang": "ja", "title": title, "kind": float64(0)}}
}

func directEdit(t *testing.T, e *editing.Engine, workID int64, patch map[string]any) error {
	t.Helper()
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: workID, Patch: patch, Actor: realActor(100),
	})
	if err != nil {
		return err
	}
	_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
	return err
}

func TestTitlesReplaceLeavesTheSearchHintLane(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "検索補助")
	if err := testDB.Create(&model.CatalogWorkTitle{
		WorkID: work.ID, Lang: "ja", Title: "かなよみ", Kind: model.WorkTitleKindSearchHint,
	}).Error; err != nil {
		t.Fatal(err)
	}

	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, el := range snap[editspec.FieldWorkTitles].([]any) {
		if el.(map[string]any)["kind"].(int64) == int64(model.WorkTitleKindSearchHint) {
			t.Fatal("search_hint must not appear in the titles value")
		}
	}
	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkTitles: []any{
			map[string]any{"lang": "ja", "title": "題", "kind": float64(0)},
			map[string]any{"lang": "ja", "title": "補助", "kind": float64(model.WorkTitleKindSearchHint)},
		}},
		Actor: realActor(100),
	}); err == nil {
		t.Fatal("kind=3 must be rejected")
	}
	if err := directEdit(t, e, work.ID, map[string]any{
		editspec.FieldWorkTitles: titlesValue("新題"),
	}); err != nil {
		t.Fatal(err)
	}
	var hints int64
	if err := testDB.Model(&model.CatalogWorkTitle{}).
		Where("work_id = ? AND kind = ?", work.ID, model.WorkTitleKindSearchHint).
		Count(&hints).Error; err != nil {
		t.Fatal(err)
	}
	if hints != 1 {
		t.Fatalf("search_hint rows after a full replace: %d, want 1", hints)
	}
}

func TestSeriesNameIsCuratedOnly(t *testing.T) {
	e := newTaxonomyEngine(t)
	dlsiteSource := int16(4)
	upstream := model.CatalogSeries{DisplayName: "上流シリーズ", SourceID: dlsiteSource}
	curated := model.CatalogSeries{DisplayName: "人手シリーズ", SourceID: 12}
	if err := testDB.Create(&[]model.CatalogSeries{upstream, curated}).Error; err != nil {
		t.Fatal(err)
	}
	var rows []model.CatalogSeries
	if err := testDB.Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	rename := func(id int64) error {
		prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeSeries, EntityID: id,
			Patch: map[string]any{editspec.FieldSeriesName: "改名"}, Actor: realActor(100),
		})
		if err != nil {
			return err
		}
		_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
		return err
	}
	for _, r := range rows {
		err := rename(r.ID)
		if r.SourceID == dlsiteSource && err == nil {
			t.Fatal("renaming an upstream series must be refused")
		}
		if r.SourceID != dlsiteSource && err != nil {
			t.Fatalf("renaming a curated series: %v", err)
		}
	}
}
