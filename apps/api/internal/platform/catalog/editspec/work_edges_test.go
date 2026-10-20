package editspec_test

import (
	"fmt"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
)

func createLabelNamed(t *testing.T, name string) *model.CatalogLabel {
	t.Helper()
	l := &model.CatalogLabel{DisplayName: name, Kind: model.LabelKindDoujinCircle}
	if err := testDB.Create(l).Error; err != nil {
		t.Fatalf("create label %q: %v", name, err)
	}
	return l
}

func TestLabelsSameCompanyInTwoRolesIsAccepted(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "開発兼発売")
	label := createLabelNamed(t, "開発兼発売ブランド")

	both := []any{
		map[string]any{"label_id": label.ID, "kind": int64(model.WorkLabelKindPublisher)},
		map[string]any{"label_id": label.ID, "kind": int64(model.WorkLabelKindDeveloper)},
	}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkLabels, both)
	sameJSON(t, "labels", snap[editspec.FieldWorkLabels], both)
}

func TestLabelsUpstreamCollisionRejected(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "レーベル衝突")
	ownLabel := createLabelNamed(t, "人手サークル")
	upLabel := createLabelNamed(t, "上流サークル")

	own := []any{map[string]any{"label_id": ownLabel.ID, "kind": int64(model.WorkLabelKindCircle)}}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkLabels, own)
	sameJSON(t, "labels", snap[editspec.FieldWorkLabels], own)

	own2 := createLabelNamed(t, "人手サークル2")
	own = []any{
		map[string]any{"label_id": ownLabel.ID, "kind": int64(model.WorkLabelKindCircle)},
		map[string]any{"label_id": own2.ID, "kind": int64(model.WorkLabelKindCircle)},
	}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkLabels, own)
	sameJSON(t, "labels own-lane", snap[editspec.FieldWorkLabels], own)

	src := vndbSource
	if err := testDB.Create(&model.CatalogWorkLabel{
		WorkID: work.ID, LabelID: upLabel.ID, Kind: model.WorkLabelKindCircle, SourceID: &src,
	}).Error; err != nil {
		t.Fatal(err)
	}

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkLabels, []any{
		map[string]any{"label_id": ownLabel.ID, "kind": int64(model.WorkLabelKindCircle)},
		map[string]any{"label_id": upLabel.ID, "kind": int64(model.WorkLabelKindCircle)},
	})
	want := fmt.Sprintf("label %d (kind %d)", upLabel.ID, model.WorkLabelKindCircle)
	if !strings.Contains(valErr.Reason, want) {
		t.Fatalf("the 422 must name the colliding pair %q, got %q", want, valErr.Reason)
	}

	var curated []model.CatalogWorkLabel
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).
		Find(&curated).Error; err != nil {
		t.Fatal(err)
	}
	if len(curated) != 2 {
		t.Fatalf("the rejected merge must leave the curated lane: %+v", curated)
	}
}

func TestLabelsNullSourceCollisionRejected(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "NULLソース衝突")
	ownLabel := createLabelNamed(t, "人手")
	machine := createLabelNamed(t, "機械")

	own := []any{map[string]any{"label_id": ownLabel.ID, "kind": int64(model.WorkLabelKindCircle)}}
	mergeField(t, e, work.ID, editspec.FieldWorkLabels, own)

	if err := testDB.Create(&model.CatalogWorkLabel{
		WorkID: work.ID, LabelID: machine.ID, Kind: model.WorkLabelKindPublisher,
	}).Error; err != nil {
		t.Fatal(err)
	}

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkLabels, []any{
		map[string]any{"label_id": ownLabel.ID, "kind": int64(model.WorkLabelKindCircle)},
		map[string]any{"label_id": machine.ID, "kind": int64(model.WorkLabelKindPublisher)},
	})
	want := fmt.Sprintf("label %d (kind %d)", machine.ID, model.WorkLabelKindPublisher)
	if !strings.Contains(valErr.Reason, want) {
		t.Fatalf("a NULL source_id is a machine row and must 422: want %q in %q", want, valErr.Reason)
	}

	var curated int64
	if err := testDB.Model(&model.CatalogWorkLabel{}).
		Where("work_id = ? AND source_id = ?", work.ID, curatedSource).Count(&curated).Error; err != nil {
		t.Fatal(err)
	}
	if curated != 1 {
		t.Fatalf("the rejected merge must leave the curated lane, found %d rows", curated)
	}
}
