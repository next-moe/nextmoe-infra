package editspec_test

import (
	"errors"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
	"api/internal/platform/provenance"
)

func newLabelEngine(t *testing.T) *editing.Engine {
	t.Helper()
	e := newEngine(t)
	for _, table := range []string{"catalog_label_alias", "catalog_label_relation", "catalog_label_intro"} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	if err := editspec.RegisterTaxonomy(e.Registry(), testDB); err != nil {
		t.Fatalf("register taxonomy: %v", err)
	}
	return e
}

func createLabel(t *testing.T, name string) model.CatalogLabel {
	t.Helper()
	l := model.CatalogLabel{DisplayName: name, Kind: model.LabelKindGameBrand}
	if err := testDB.Create(&l).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	return l
}

func reloadLabel(t *testing.T, id int64) model.CatalogLabel {
	t.Helper()
	var l model.CatalogLabel
	if err := testDB.First(&l, id).Error; err != nil {
		t.Fatal(err)
	}
	return l
}

func TestLabelScalarEditsStampProvenance(t *testing.T) {
	e := newLabelEngine(t)
	l := createLabel(t, "Key")
	if err := mergeEdit(t, e, editspec.TypeLabel, l.ID, map[string]any{
		editspec.FieldLabelLatin: "Ki",
		editspec.FieldLabelLang:  "ja",
		editspec.FieldLabelKind:  float64(model.LabelKindPublisher),
		editspec.FieldLabelNote:  "ビジュアルアーツのブランド",
		editspec.FieldLabelLogo:  "logohash",
	}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got := reloadLabel(t, l.ID)
	if got.Latin == nil || *got.Latin != "Ki" || got.Lang != "ja" || got.Kind != model.LabelKindPublisher ||
		got.Note == "" || got.LogoHash != "logohash" {
		t.Fatalf("label = %+v", got)
	}
	for _, col := range []string{"latin", "lang", "kind", "note", "logo_hash"} {
		if src := provenance.FirstSource(got.FieldProvenance, col); !provenance.IsHuman(src) {
			t.Errorf("%s stamp = %q", col, src)
		}
	}
	if err := mergeEdit(t, e, editspec.TypeLabel, l.ID, map[string]any{
		editspec.FieldLabelLatin: nil, editspec.FieldLabelLogo: nil,
	}); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if got := reloadLabel(t, l.ID); got.Latin != nil || got.LogoHash != "" {
		t.Fatalf("clear did not reach the columns: %+v", got)
	}
}

func TestLabelAliasSuppressionAndCollision(t *testing.T) {
	e := newLabelEngine(t)
	l := createLabel(t, "Key")
	if err := testDB.Exec(`INSERT INTO catalog_label_alias (label_id, name, lang, kind, is_primary_for_locale, source_id, provenance)
		VALUES (?, 'キー', 'ja', 1, false, 3, 0), (?, '誤り', 'ja', 1, false, 3, 0)`, l.ID, l.ID).Error; err != nil {
		t.Fatal(err)
	}
	live := func() []string {
		var names []string
		if err := testDB.Raw(`SELECT a.name FROM catalog_label_alias a WHERE a.label_id = ? AND `+
			editspec.NotSuppressedLabelAliasSQL("a")+` ORDER BY a.name`, l.ID).Scan(&names).Error; err != nil {
			t.Fatal(err)
		}
		return names
	}
	key := editspec.LabelAliasIdentity(1, "ja", "誤り")
	if err := mergeEdit(t, e, editspec.TypeLabel, l.ID, map[string]any{
		editspec.FieldLabelAliasesSuppr: suppressList(key),
	}); err != nil {
		t.Fatalf("suppress: %v", err)
	}
	if got := live(); len(got) != 1 || got[0] != "キー" {
		t.Fatalf("live aliases = %v", got)
	}
	err := mergeEdit(t, e, editspec.TypeLabel, l.ID, map[string]any{
		editspec.FieldLabelAliases: []any{map[string]any{"name": "キー", "lang": "ja", "kind": float64(1)}},
	})
	var ve *editing.ValidationError
	if !errors.As(err, &ve) || ve.Key != editspec.FieldLabelAliases {
		t.Fatalf("re-adding an upstream alias must be a 422, got %v", err)
	}
	if err := mergeEdit(t, e, editspec.TypeLabel, l.ID, map[string]any{
		editspec.FieldLabelAliases: []any{map[string]any{"name": "鍵", "lang": "zh-Hans", "kind": float64(0)}},
	}); err != nil {
		t.Fatalf("curated alias: %v", err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeLabel, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "aliases", snap[editspec.FieldLabelAliases], []any{
		map[string]any{"kind": 0, "lang": "zh-Hans", "name": "鍵"},
	})
}

func relationRows(t *testing.T) map[[3]int64]int16 {
	t.Helper()
	var rows []model.CatalogLabelRelation
	if err := testDB.Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	out := map[[3]int64]int16{}
	for _, r := range rows {
		out[[3]int64{r.LabelID, r.OtherLabelID, int64(r.Relation)}] = r.SourceID
	}
	return out
}

func TestLabelRelationsAreMirroredPairs(t *testing.T) {
	e := newLabelEngine(t)
	parent := createLabel(t, "VisualArts")
	brand := createLabel(t, "Key")
	if err := mergeEdit(t, e, editspec.TypeLabel, brand.ID, map[string]any{
		editspec.FieldLabelRelations: []any{map[string]any{
			"label_id": float64(parent.ID), "relation": float64(model.LabelRelationParent),
		}},
	}); err != nil {
		t.Fatalf("merge relation: %v", err)
	}
	rows := relationRows(t)
	if rows[[3]int64{brand.ID, parent.ID, int64(model.LabelRelationParent)}] != 12 ||
		rows[[3]int64{parent.ID, brand.ID, int64(model.LabelRelationSubsidiary)}] != 12 || len(rows) != 2 {
		t.Fatalf("a curated fact is the edge plus its mirror: %v", rows)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeLabel, parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, "mirror in the other label's snapshot", snap[editspec.FieldLabelRelations], []any{
		map[string]any{"label_id": brand.ID, "relation": int64(model.LabelRelationSubsidiary)},
	})
	if err := mergeEdit(t, e, editspec.TypeLabel, brand.ID, map[string]any{
		editspec.FieldLabelRelations: []any{},
	}); err != nil {
		t.Fatalf("drop relation: %v", err)
	}
	if rows := relationRows(t); len(rows) != 0 {
		t.Fatalf("dropping a fact removes both rows: %v", rows)
	}

	err = mergeEdit(t, e, editspec.TypeLabel, brand.ID, map[string]any{
		editspec.FieldLabelRelations: []any{map[string]any{"label_id": float64(brand.ID), "relation": float64(1)}},
	})
	var ve *editing.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("a self relation must be a 422, got %v", err)
	}
}

func TestLabelRelationSuppressionPurgesBothDirections(t *testing.T) {
	e := newLabelEngine(t)
	a := createLabel(t, "A")
	b := createLabel(t, "B")
	if err := testDB.Exec(`INSERT INTO catalog_label_relation (label_id, other_label_id, relation, source_id, matched_by, created_at)
		VALUES (?, ?, 3, 2, 'rule:test', now()), (?, ?, 4, 2, 'rule:test', now())`, a.ID, b.ID, b.ID, a.ID).Error; err != nil {
		t.Fatal(err)
	}
	key := editspec.LabelRelationIdentity(model.LabelRelationImprintOf, a.ID)
	if err := mergeEdit(t, e, editspec.TypeLabel, b.ID, map[string]any{
		editspec.FieldLabelRelationsSuppr: suppressList(key),
	}); err != nil {
		t.Fatalf("suppress from the mirrored side: %v", err)
	}
	if rows := relationRows(t); len(rows) != 0 {
		t.Fatalf("both halves of the upstream fact must go: %v", rows)
	}
	suppressed, err := editspec.LoadSuppressedLabelRelations(testCtx, testDB)
	if err != nil {
		t.Fatal(err)
	}
	if !suppressed.Has(a.ID, b.ID, model.LabelRelationImprint) || !suppressed.Has(b.ID, a.ID, model.LabelRelationImprintOf) {
		t.Fatal("an importer must see both directions of a suppressed fact")
	}
	if suppressed.Has(a.ID, b.ID, model.LabelRelationParent) {
		t.Fatal("other relations between the pair stay importable")
	}
}
