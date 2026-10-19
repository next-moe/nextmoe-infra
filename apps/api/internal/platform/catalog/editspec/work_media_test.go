package editspec_test

import (
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func mergeMustReject(t *testing.T, e *editing.Engine, entityType string, entityID int64, key string, value any) *editing.ValidationError {
	t.Helper()
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: entityID,
		Patch: map[string]any{key: value}, Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose %s: %v", key, err)
	}
	var valErr *editing.ValidationError
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); !errors.As(err, &valErr) {
		t.Fatalf("merge %s: %v, want ValidationError (422)", key, err)
	}
	if valErr.Key != key {
		t.Fatalf("422 key = %q, want %q", valErr.Key, key)
	}
	return valErr
}

func TestCoversUpstreamCollisionRejected(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "カバー衝突")
	if err := testDB.Create(&model.CatalogWorkCover{
		WorkID: work.ID, ImageHash: "upstream-other", SourceID: vndbSource,
	}).Error; err != nil {
		t.Fatal(err)
	}

	own := []any{map[string]any{"image_hash": "human-old"}}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkCovers, own)
	sameJSON(t, "covers", snap[editspec.FieldWorkCovers], own)

	// Keeping the curated hash and adding another is not a collision with
	// the row already in source_id = 12.
	own = []any{
		map[string]any{"image_hash": "human-old"},
		map[string]any{"image_hash": "human-new"},
	}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkCovers, own)
	sameJSON(t, "covers own-lane", snap[editspec.FieldWorkCovers], own)

	if err := testDB.Create(&[]model.CatalogWorkCover{
		{WorkID: work.ID, ImageHash: "zzz-hit", SourceID: vndbSource},
		{WorkID: work.ID, ImageHash: "aaa-hit", SourceID: vndbSource},
	}).Error; err != nil {
		t.Fatal(err)
	}

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkCovers, []any{
		map[string]any{"image_hash": "human-old"},
		map[string]any{"image_hash": "zzz-hit"},
		map[string]any{"image_hash": "aaa-hit"},
	})
	if !strings.Contains(valErr.Reason, "aaa-hit") {
		t.Fatalf("the 422 must name the first colliding hash (ordered): %q", valErr.Reason)
	}
	if strings.Contains(valErr.Reason, "suppressed") {
		t.Fatalf("covers have no suppression companion, got %q", valErr.Reason)
	}

	var curated []model.CatalogWorkCover
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).
		Order("image_hash").Find(&curated).Error; err != nil {
		t.Fatal(err)
	}
	if len(curated) != 2 || curated[0].ImageHash != "human-new" || curated[1].ImageHash != "human-old" {
		t.Fatalf("the rejected merge must leave the curated lane: %+v", curated)
	}
}

func TestScreenshotsUpstreamCollisionRejected(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "スクショ衝突")
	if err := testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: work.ID, ImageHash: "upstream-other", SourceID: vndbSource,
	}).Error; err != nil {
		t.Fatal(err)
	}

	own := []any{map[string]any{"image_hash": "human-shot", "caption": "OP"}}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkScreenshots, own)
	sameJSON(t, "screenshots", snap[editspec.FieldWorkScreenshots], own)

	own = []any{
		map[string]any{"image_hash": "human-shot", "caption": "OP"},
		map[string]any{"image_hash": "human-shot-2"},
	}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkScreenshots, own)
	sameJSON(t, "screenshots own-lane", snap[editspec.FieldWorkScreenshots], own)

	if err := testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: work.ID, ImageHash: "upstream-shot", SourceID: vndbSource,
	}).Error; err != nil {
		t.Fatal(err)
	}

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkScreenshots, []any{
		map[string]any{"image_hash": "human-shot", "caption": "OP"},
		map[string]any{"image_hash": "upstream-shot"},
	})
	if !strings.Contains(valErr.Reason, "upstream-shot") {
		t.Fatalf("the 422 must name the colliding hash: %q", valErr.Reason)
	}
	if strings.Contains(valErr.Reason, "suppressed") {
		t.Fatalf("screenshots have no suppression companion, got %q", valErr.Reason)
	}

	var curated []model.CatalogWorkScreenshot
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).
		Find(&curated).Error; err != nil {
		t.Fatal(err)
	}
	if len(curated) != 2 {
		t.Fatalf("the rejected merge must leave the curated lane: %+v", curated)
	}
	seen := map[string]string{}
	for _, r := range curated {
		seen[r.ImageHash] = r.Caption
	}
	if _, ok := seen["human-shot-2"]; !ok || seen["human-shot"] != "OP" {
		t.Fatalf("the rejected merge must leave both curated shots: %+v", curated)
	}
}
