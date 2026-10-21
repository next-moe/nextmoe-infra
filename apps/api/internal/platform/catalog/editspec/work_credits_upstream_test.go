package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

func upstreamCreditsSnapshot(t *testing.T, e *editing.Engine, workID int64) []map[string]any {
	t.Helper()
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, workID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	raw, ok := snap[editspec.FieldWorkCreditsUpstream].([]any)
	if !ok {
		t.Fatalf("snapshot has no %s: %#v", editspec.FieldWorkCreditsUpstream, snap[editspec.FieldWorkCreditsUpstream])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, el := range raw {
		out = append(out, el.(map[string]any))
	}
	return out
}

func numeric(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func TestUpstreamCreditCorrectionStampsOnlyChangedColumns(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "署名修正")
	va := roleIDByKey(t, "voice-actor")
	scenario := roleIDByKey(t, "scenario")
	actor := createCreditName(t, "声優A")
	writer := createCreditName(t, "脚本B")
	ch := createCharacter(t, "ヒロイン")
	voiced := importCredit(t, work.ID, actor.ID, va, &ch.ID)
	written := importCredit(t, work.ID, writer.ID, scenario, nil)
	execT(t, `UPDATE catalog_credit SET note = '旧注' WHERE id = ?`, written)

	rel := &model.CatalogRelease{WorkID: work.ID, Kind: model.ReleaseKindDefault, Extra: []byte(`{}`)}
	if err := testDB.Create(rel).Error; err != nil {
		t.Fatal(err)
	}
	label := model.CatalogLabel{DisplayName: "ブランド", Kind: model.LabelKindGameBrand}
	if err := testDB.Create(&label).Error; err != nil {
		t.Fatal(err)
	}

	rows := upstreamCreditsSnapshot(t, e, work.ID)
	if len(rows) != 2 {
		t.Fatalf("upstream credits in snapshot = %v", rows)
	}
	for _, row := range rows {
		switch numeric(row["credit_name_id"]) {
		case actor.ID:
			row["note"] = "仅限 FD"
			row["spoiler"] = float64(2)
			row["release_id"] = float64(rel.ID)
			row["label_id"] = float64(label.ID)
		case writer.ID:
			delete(row, "note")
		}
	}
	value := []any{rows[0], rows[1]}
	if err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCreditsUpstream: value}); err != nil {
		t.Fatalf("correct upstream credits: %v", err)
	}

	var got model.CatalogCredit
	if err := testDB.First(&got, voiced).Error; err != nil {
		t.Fatal(err)
	}
	if got.Note != "仅限 FD" || got.Spoiler != model.SpoilerSevere ||
		got.ReleaseID == nil || *got.ReleaseID != rel.ID || got.LabelID == nil || *got.LabelID != label.ID {
		t.Fatalf("corrected row = %+v", got)
	}
	if got.SourceID == nil || *got.SourceID != upstreamSourceID {
		t.Fatalf("the corrected row must stay in the importer's lane, source = %v", got.SourceID)
	}
	for _, column := range []string{"note", "spoiler", "release_id", "label_id"} {
		if head := provenanceHead(t, "catalog_credit", voiced, column); head != "curated" {
			t.Fatalf("%s stamp = %q, want curated", column, head)
		}
	}

	var cleared model.CatalogCredit
	if err := testDB.First(&cleared, written).Error; err != nil {
		t.Fatal(err)
	}
	if cleared.Note != "" {
		t.Fatalf("an absent note must clear the row's note, got %q", cleared.Note)
	}
	if head := provenanceHead(t, "catalog_credit", written, "note"); head != "curated" {
		t.Fatalf("cleared note stamp = %q, want curated", head)
	}
	if head := provenanceHead(t, "catalog_credit", written, "spoiler"); head != "" {
		t.Fatalf("an unchanged column was stamped %q", head)
	}
}

func TestUpstreamCreditCorrectionRefusals(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "署名修正拒否")
	other := createWork(t, "別作品")
	scenario := roleIDByKey(t, "scenario")
	writer := createCreditName(t, "脚本C")
	importCredit(t, work.ID, writer.ID, scenario, nil)
	foreign := &model.CatalogRelease{WorkID: other.ID, Kind: model.ReleaseKindDefault, Extra: []byte(`{}`)}
	if err := testDB.Create(foreign).Error; err != nil {
		t.Fatal(err)
	}

	row := func(nameID int64) map[string]any {
		return map[string]any{"role_id": float64(scenario), "credit_name_id": float64(nameID), "spoiler": float64(0)}
	}
	var ve *editing.ValidationError

	curated := createCreditName(t, "人手")
	if err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCredits: []any{row(curated.ID)}}); err != nil {
		t.Fatalf("add curated credit: %v", err)
	}
	err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCreditsUpstream: []any{row(curated.ID)}})
	if !errors.As(err, &ve) || ve.Key != editspec.FieldWorkCreditsUpstream {
		t.Fatalf("correcting a human row through the upstream field: err = %v, want a 422 on %s", err, editspec.FieldWorkCreditsUpstream)
	}

	withForeign := row(writer.ID)
	withForeign["release_id"] = float64(foreign.ID)
	err = directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCreditsUpstream: []any{withForeign}})
	if !errors.As(err, &ve) || ve.Key != editspec.FieldWorkCreditsUpstream {
		t.Fatalf("a release of another work: err = %v, want a 422", err)
	}

	reg := editing.NewRegistry()
	if err := editspec.RegisterWork(reg, testDB); err != nil {
		t.Fatal(err)
	}
	spec, _ := reg.Type(editspec.TypeWork)
	f, _ := spec.Field(editspec.FieldWorkCreditsUpstream)
	if err := f.Validate([]any{map[string]any{"role_id": float64(scenario), "credit_name_id": float64(writer.ID)}}); err == nil {
		t.Fatal("an element without spoiler must be refused, or the missing level reads as none")
	}
}
