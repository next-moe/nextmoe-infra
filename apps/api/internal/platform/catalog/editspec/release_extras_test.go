package editspec_test

import (
	"encoding/json"
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func releaseExtraDoc(t *testing.T, id int64) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(reloadRelease(t, id).Extra, &doc); err != nil {
		t.Fatalf("decode extra of release %d: %v", id, err)
	}
	return doc
}

func mergeOnErr(t *testing.T, e *editing.Engine, entityType string, id int64, patch map[string]any) error {
	t.Helper()
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: entityType, EntityID: id, Patch: patch, Actor: realActor(100),
	})
	if err != nil {
		return err
	}
	_, err = e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), "")
	return err
}

func TestReleasePlatformsWriteTheListAndItsHead(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "マルチの主")
	rel := createReleaseOn(t, work.ID)
	if err := testDB.Exec(`UPDATE catalog_release SET extra = '{"vndb_id":"r1","freeware":false}' WHERE id = ?`, rel.ID).Error; err != nil {
		t.Fatal(err)
	}

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleasePlatforms: []any{"ps4", "win"},
	})
	if got := snap[editspec.FieldReleasePlatforms]; !jsonEqual(got, []any{"ps4", "win"}) {
		t.Fatalf("snapshot platforms = %#v", got)
	}
	got := reloadRelease(t, rel.ID)
	if got.Platform == nil || *got.Platform != "ps4" {
		t.Fatalf("platform head = %v, want ps4", got.Platform)
	}
	doc := releaseExtraDoc(t, rel.ID)
	if !jsonEqual(doc["platforms"], []any{"ps4", "win"}) || doc["vndb_id"] != "r1" || doc["freeware"] != false {
		t.Fatalf("extra after platforms edit = %#v", doc)
	}
	for _, key := range []string{"platform", "platforms"} {
		if head := provenance.FirstSource(got.FieldProvenance, key); head != provenance.SourceCurated {
			t.Errorf("field_provenance[%s] head = %q, want curated", key, head)
		}
	}

	mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{editspec.FieldReleasePlatforms: []any{}})
	cleared := reloadRelease(t, rel.ID)
	if cleared.Platform != nil {
		t.Fatalf("emptied platforms left head %q", *cleared.Platform)
	}
	if _, present := releaseExtraDoc(t, rel.ID)["platforms"]; present {
		t.Fatal("emptied platforms must drop the extra key, not store []")
	}
}

func TestReleasePlatformHeadWithoutAListReadsAsAOneElementList(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "頭だけの主")
	rel := createReleaseOn(t, work.ID)
	if err := testDB.Exec(`UPDATE catalog_release SET platform = 'swi' WHERE id = ?`, rel.ID).Error; err != nil {
		t.Fatal(err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeRelease, rel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := snap[editspec.FieldReleasePlatforms]; !jsonEqual(got, []any{"swi"}) {
		t.Fatalf("snapshot platforms = %#v, want [swi]", got)
	}
	if _, present := snap[editspec.FieldReleasePlatform]; present {
		t.Fatal("the deprecated head must not be a snapshot key of its own: it changes whenever platforms does")
	}
	err = mergeOnErr(t, e, editspec.TypeRelease, rel.ID, map[string]any{editspec.FieldReleasePlatforms: []any{"swi"}})
	if !errors.Is(err, editing.ErrNoEffectiveChanges) {
		t.Fatalf("re-saving the folded value must be a no-op, got %v", err)
	}
}

func TestReleaseExtrasEditAndClear(t *testing.T) {
	e := newReleaseEngine(t)
	work := createWork(t, "属性の主")
	rel := createReleaseOn(t, work.ID)

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLanguages: []any{"ja", "zh-Hans"},
		editspec.FieldReleaseFreeware:  true,
		editspec.FieldReleaseOfficial:  false,
		editspec.FieldReleaseMinAge:    float64(18),
	})
	if !jsonEqual(snap[editspec.FieldReleaseLanguages], []any{"ja", "zh-Hans"}) ||
		snap[editspec.FieldReleaseFreeware] != true || snap[editspec.FieldReleaseOfficial] != false ||
		snap[editspec.FieldReleaseMinAge] != int64(18) {
		t.Fatalf("snapshot extras = %#v", snap)
	}
	doc := releaseExtraDoc(t, rel.ID)
	if !jsonEqual(doc["languages"], []any{"ja", "zh-Hans"}) || doc["freeware"] != true ||
		doc["official"] != false || doc["minage"] != float64(18) {
		t.Fatalf("extra = %#v", doc)
	}
	prov := reloadRelease(t, rel.ID).FieldProvenance
	for _, key := range []string{"languages", "freeware", "official", "minage"} {
		if head := provenance.FirstSource(prov, key); head != provenance.SourceCurated {
			t.Errorf("field_provenance[%s] head = %q, want curated", key, head)
		}
	}

	snap = mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLanguages: []any{},
		editspec.FieldReleaseFreeware:  nil,
		editspec.FieldReleaseOfficial:  nil,
		editspec.FieldReleaseMinAge:    nil,
	})
	if snap[editspec.FieldReleaseFreeware] != nil || snap[editspec.FieldReleaseOfficial] != nil ||
		snap[editspec.FieldReleaseMinAge] != nil || !jsonEqual(snap[editspec.FieldReleaseLanguages], []any{}) {
		t.Fatalf("cleared snapshot = %#v", snap)
	}
	if doc := releaseExtraDoc(t, rel.ID); len(doc) != 0 {
		t.Fatalf("clearing every extra must leave {}, got %#v", doc)
	}
}

func seedReleaseLabel(t *testing.T, releaseID, labelID int64, kind int16, source int16) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO catalog_release_label (release_id, label_id, kind, source_id, created_at)
		VALUES (?, ?, ?, ?, now())`, releaseID, labelID, kind, source).Error; err != nil {
		t.Fatalf("seed release label: %v", err)
	}
}

func visibleReleaseLabels(t *testing.T, releaseID int64) [][2]int64 {
	t.Helper()
	var rows []struct {
		LabelID int64
		Kind    int16
	}
	if err := testDB.Raw(`SELECT rl.label_id, rl.kind FROM catalog_release_label rl
		WHERE rl.release_id = ? AND `+editspec.NotSuppressedReleaseLabelSQL("rl")+`
		ORDER BY rl.label_id, rl.kind`, releaseID).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := make([][2]int64, 0, len(rows))
	for _, r := range rows {
		out = append(out, [2]int64{r.LabelID, int64(r.Kind)})
	}
	return out
}

func TestReleaseLabelsCuratedLaneAndSuppression(t *testing.T) {
	e := newReleaseEngine(t)
	if err := testDB.Exec("TRUNCATE catalog_label RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec("TRUNCATE edit_suppressed_row RESTART IDENTITY").Error; err != nil {
		t.Fatal(err)
	}
	upstream := createLabel(t, "上流ブランド")
	human := createLabel(t, "手入力パブリッシャー")
	work := createWork(t, "厂商の主")
	rel := createReleaseOn(t, work.ID)
	seedReleaseLabel(t, rel.ID, upstream.ID, model.WorkLabelKindDeveloper, 2)

	snap := mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLabels: []any{
			map[string]any{"label_id": float64(human.ID), "kind": float64(model.WorkLabelKindPublisher)},
			map[string]any{"label_id": float64(human.ID), "kind": float64(model.WorkLabelKindDeveloper)},
		},
	})
	if got := snap[editspec.FieldReleaseLabels].([]any); len(got) != 2 {
		t.Fatalf("snapshot labels = %#v, want only the two curated rows", got)
	}
	if got := visibleReleaseLabels(t, rel.ID); len(got) != 3 {
		t.Fatalf("visible labels = %v, want upstream + 2 curated", got)
	}

	var valErr *editing.ValidationError
	err := mergeOnErr(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLabels: []any{
			map[string]any{"label_id": float64(upstream.ID), "kind": float64(model.WorkLabelKindDeveloper)},
		},
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldReleaseLabels {
		t.Fatalf("re-adding an upstream attribution must 422 on labels, got %v", err)
	}

	hideKey := editspec.ReleaseLabelIdentity(upstream.ID, model.WorkLabelKindDeveloper)
	mergeOn(t, e, editspec.TypeRelease, rel.ID, map[string]any{editspec.FieldReleaseLabelsSuppr: []any{hideKey}})
	if got := visibleReleaseLabels(t, rel.ID); len(got) != 2 || got[0][0] != human.ID {
		t.Fatalf("visible labels after hiding upstream = %v, want the two curated rows", got)
	}
	var upstreamRows int64
	testDB.Raw(`SELECT count(*) FROM catalog_release_label WHERE release_id = ? AND label_id = ?`, rel.ID, upstream.ID).Scan(&upstreamRows)
	if upstreamRows != 1 {
		t.Fatalf("suppression must hide the upstream row, not delete it (rows = %d)", upstreamRows)
	}

	err = mergeOnErr(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLabelsSuppr: []any{hideKey, editspec.ReleaseLabelIdentity(human.ID, model.WorkLabelKindPublisher)},
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldReleaseLabelsSuppr {
		t.Fatalf("suppressing a curated attribution must 422 on .suppressed, got %v", err)
	}

	if err := testDB.Exec(`DELETE FROM catalog_release_label WHERE release_id = ? AND label_id = ?`, rel.ID, upstream.ID).Error; err != nil {
		t.Fatal(err)
	}
	err = mergeOnErr(t, e, editspec.TypeRelease, rel.ID, map[string]any{
		editspec.FieldReleaseLabels: []any{
			map[string]any{"label_id": float64(upstream.ID), "kind": float64(model.WorkLabelKindDeveloper)},
		},
	})
	if !errors.As(err, &valErr) || valErr.Key != editspec.FieldReleaseLabels {
		t.Fatalf("adding an attribution that is still suppressed must 422, got %v", err)
	}
}

func jsonEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
