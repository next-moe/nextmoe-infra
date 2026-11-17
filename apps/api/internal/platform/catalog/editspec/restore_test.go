package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func TestRevertSkipsALabelEdgeAnImporterNowHolds(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "復元レーベル")
	own := createLabelNamed(t, "人手ブランド")
	later := createLabelNamed(t, "後から上流")

	both := []any{
		map[string]any{"label_id": own.ID, "kind": int64(model.WorkLabelKindBrand)},
		map[string]any{"label_id": later.ID, "kind": int64(model.WorkLabelKindBrand)},
	}
	mergeField(t, e, work.ID, editspec.FieldWorkLabels, both)
	mergeField(t, e, work.ID, editspec.FieldWorkLabels, both[:1])
	if err := testDB.Create(&model.CatalogWorkLabel{
		WorkID: work.ID, LabelID: later.ID, Kind: model.WorkLabelKindBrand,
	}).Error; err != nil {
		t.Fatal(err)
	}

	_, _, err := e.Revert(testCtx, editing.RevertInput{
		EntityType: editspec.TypeWork, EntityID: work.ID, ToSeq: 1, Actor: realActor(200, "ren"),
	})
	if !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("revert to a list naming an upstream-held edge: err = %v, want ErrNoEffectiveChanges (the edge already shows)", err)
	}
}

func TestUndoRestoresACoverWithoutTheOnesAnImporterNowHolds(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "復元カバー")
	mergeField(t, e, work.ID, editspec.FieldWorkCovers, []any{
		map[string]any{"image_hash": "kept-by-hand"},
		map[string]any{"image_hash": "taken-upstream"},
	})
	mergeField(t, e, work.ID, editspec.FieldWorkCovers, []any{})
	if err := testDB.Create(&model.CatalogWorkCover{
		WorkID: work.ID, ImageHash: "taken-upstream", SourceID: vndbSource,
	}).Error; err != nil {
		t.Fatal(err)
	}

	if _, _, err := e.Undo(testCtx, editing.UndoInput{
		EntityType: editspec.TypeWork, EntityID: work.ID, Seq: 2, Actor: realActor(200, "ren"),
	}); err != nil {
		t.Fatalf("undo: %v", err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "covers", snap[editspec.FieldWorkCovers], []any{map[string]any{"image_hash": "kept-by-hand"}})
}

func TestAnEditorUploadIsStampedAndAKeptLegacyRowIsNot(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "スタンプ")
	if err := testDB.Create(&model.CatalogWorkCover{
		WorkID: work.ID, ImageHash: "legacy-wiki", SourceID: curatedSource,
	}).Error; err != nil {
		t.Fatal(err)
	}
	mergeField(t, e, work.ID, editspec.FieldWorkCovers, []any{
		map[string]any{"image_hash": "legacy-wiki"},
		map[string]any{"image_hash": "fresh-upload"},
	})

	var rows []struct {
		ImageHash string
		Source    *string
	}
	if err := testDB.Raw(`SELECT image_hash, field_provenance -> ? -> 0 ->> 'source' AS source
		FROM catalog_work_cover WHERE work_id = ? ORDER BY image_hash`,
		model.CoverUploadStampColumn, work.ID).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ImageHash != "fresh-upload" || rows[0].Source == nil || *rows[0].Source != "curated" {
		t.Fatalf("the inserted upload must carry the curated stamp: %+v", rows)
	}
	if rows[1].Source != nil {
		t.Fatalf("a legacy row an editor merely kept must stay unstamped, got %q", *rows[1].Source)
	}
}
