package editspec_test

import (
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func TestWorkMediaOrderRoundTrip(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "並び順")

	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "cover_order before", snap[editspec.FieldWorkCoverOrder], []any{})
	sameJSON(t, "screenshot_order before", snap[editspec.FieldWorkScreenshotOrder], []any{})

	covers := []any{"c-b", "c-a", "c-c"}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkCoverOrder, covers)
	sameJSON(t, "cover_order", snap[editspec.FieldWorkCoverOrder], covers)
	sameJSON(t, "screenshot_order stays", snap[editspec.FieldWorkScreenshotOrder], []any{})

	shots := []any{"s-2", "s-1"}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkScreenshotOrder, shots)
	sameJSON(t, "screenshot_order", snap[editspec.FieldWorkScreenshotOrder], shots)
	sameJSON(t, "cover_order stays", snap[editspec.FieldWorkCoverOrder], covers)

	snap = mergeField(t, e, work.ID, editspec.FieldWorkCoverOrder, []any{})
	sameJSON(t, "cover_order reset", snap[editspec.FieldWorkCoverOrder], []any{})
	var left int64
	testDB.Model(&model.CatalogWorkMediaOrder{}).
		Where("work_id = ? AND kind = ?", work.ID, model.WorkMediaOrderCover).Count(&left)
	if left != 0 {
		t.Fatalf("an empty order must delete the work's cover order rows, %d left", left)
	}
}

func TestWorkMediaOrderAppliesWithAnUploadInTheSameProposal(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "同時アップロード")
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{
			editspec.FieldWorkCovers:     []any{map[string]any{"image_hash": "fresh"}},
			editspec.FieldWorkCoverOrder: []any{"fresh", "older"},
		},
		Actor: realActor(100),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); err != nil {
		t.Fatalf("merge: %v", err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "cover_order", snap[editspec.FieldWorkCoverOrder], []any{"fresh", "older"})
}

func TestWorkMediaOrderValidation(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "検証")
	for name, value := range map[string]any{
		"duplicate": []any{"a", "a"},
		"blank":     []any{" "},
		"number":    []any{1},
		"object":    map[string]any{"a": 1},
	} {
		if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
			EntityType: editspec.TypeWork, EntityID: work.ID,
			Patch: map[string]any{editspec.FieldWorkCoverOrder: value},
			Actor: realActor(100),
		}); err == nil {
			t.Fatalf("%s: a cover order of %v must fail validation", name, value)
		}
	}
}
