package editspec_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

const (
	srcVNDB    int16 = 2
	srcBangumi int16 = 3
	srcDerived int16 = 18
)

func execT(t *testing.T, sql string, args ...any) {
	t.Helper()
	if err := testDB.Exec(sql, args...).Error; err != nil {
		t.Fatalf("%s: %v", strings.Fields(sql)[0], err)
	}
}

func idsWhere(t *testing.T, sql string, args ...any) []int64 {
	t.Helper()
	var out []int64
	if err := testDB.Raw(sql, args...).Scan(&out).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	return out
}

func stringsWhere(t *testing.T, sql string, args ...any) []string {
	t.Helper()
	var out []string
	if err := testDB.Raw(sql, args...).Scan(&out).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	return out
}

func TestUpstreamListsNameRegisteredFields(t *testing.T) {
	reg := editing.NewRegistry()
	if err := editspec.RegisterAll(reg, testDB); err != nil {
		t.Fatal(err)
	}
	for typ, fields := range map[string][]string{
		editspec.TypeWork: {editspec.FieldWorkLabels, editspec.FieldWorkEngineIDs, editspec.FieldWorkSeriesIDs,
			editspec.FieldWorkLinks, editspec.FieldWorkCovers, editspec.FieldWorkScreenshots, editspec.FieldWorkIntros},
		editspec.TypeLabel:     {editspec.FieldLabelLinks, editspec.FieldLabelIntros},
		editspec.TypeCharacter: {editspec.FieldCharacterIntros},
	} {
		spec, ok := reg.Type(typ)
		if !ok {
			t.Fatalf("%s is not registered", typ)
		}
		for _, key := range fields {
			f, ok := spec.Field(key)
			if !ok || f.Identity == nil {
				t.Fatalf("%s.%s is missing or has no identity", typ, key)
			}
			if _, ok := spec.Field(editing.SuppressedFieldKey(key)); !ok {
				t.Fatalf("%s has no companion for %s", typ, key)
			}
		}
	}
}

func visibleCovers(t *testing.T, workID int64) []string {
	return stringsWhere(t, `SELECT vc.image_hash FROM catalog_work_cover vc
		WHERE vc.work_id = ? AND `+editspec.NotSuppressedWorkCoverSQL("vc")+` ORDER BY vc.image_hash`, workID)
}

func TestUpstreamCoverSuppressionSurvivesTheImporter(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "封面")
	bad := strings.Repeat("a", 64)
	good := strings.Repeat("b", 64)
	for _, h := range []string{bad, good} {
		execT(t, `INSERT INTO catalog_work_cover (work_id, image_hash, source_id) VALUES (?, ?, ?)`, work.ID, h, srcVNDB)
	}
	key := editspec.CoverIdentity(bad)
	if err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCoversSuppr: []any{key}}); err != nil {
		t.Fatalf("suppress cover: %v", err)
	}
	if got := visibleCovers(t, work.ID); len(got) != 1 || got[0] != good {
		t.Fatalf("visible covers = %v", got)
	}
	execT(t, `DELETE FROM catalog_work_cover WHERE work_id = ? AND image_hash = ?`, work.ID, bad)
	execT(t, `INSERT INTO catalog_work_cover (work_id, image_hash, source_id) VALUES (?, ?, ?) ON CONFLICT DO NOTHING`,
		work.ID, bad, srcVNDB)
	if got := visibleCovers(t, work.ID); len(got) != 1 || got[0] != good {
		t.Fatalf("visible covers after the importer re-pushed the row = %v", got)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := snap[editspec.FieldWorkCoversSuppr].([]any); len(got) != 1 || got[0] != key {
		t.Fatalf("snapshot %s = %v", editspec.FieldWorkCoversSuppr, got)
	}
}

func provenanceHead(t *testing.T, table string, id int64, column string) string {
	t.Helper()
	var raw string
	if err := testDB.Raw(`SELECT field_provenance::text FROM `+table+` WHERE id = ?`, id).Scan(&raw).Error; err != nil {
		t.Fatal(err)
	}
	var doc map[string][]struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	if len(doc[column]) == 0 {
		return ""
	}
	return doc[column][0].Source
}

func TestUpstreamCoverRegradeStampsOnlyTheChangedColumns(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "改分级")
	h := strings.Repeat("c", 64)
	execT(t, `INSERT INTO catalog_work_cover (work_id, image_hash, source_id, kind, sexual) VALUES (?, ?, ?, 'main', 0)`,
		work.ID, h, srcVNDB)
	id := idsWhere(t, `SELECT id FROM catalog_work_cover WHERE work_id = ?`, work.ID)[0]

	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows := snap[editspec.FieldWorkCoversUpstream].([]any)
	if len(rows) != 1 {
		t.Fatalf("upstream covers in snapshot = %v", rows)
	}
	row := rows[0].(map[string]any)
	row["sexual"] = float64(2)
	if err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCoversUpstream: []any{row}}); err != nil {
		t.Fatalf("regrade: %v", err)
	}
	var c model.CatalogWorkCover
	if err := testDB.First(&c, id).Error; err != nil {
		t.Fatal(err)
	}
	if c.Sexual != 2 || c.Kind != "main" || c.SourceID != srcVNDB {
		t.Fatalf("regraded row = %+v", c)
	}
	if got := provenanceHead(t, "catalog_work_cover", id, "sexual"); got != "curated" {
		t.Fatalf("sexual stamp = %q, want curated", got)
	}
	if got := provenanceHead(t, "catalog_work_cover", id, "kind"); got != "" {
		t.Fatalf("an unchanged column was stamped %q", got)
	}

	err = directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkCoversUpstream: []any{map[string]any{
		"image_hash": strings.Repeat("d", 64), "kind": "", "portrait_pinned": false,
		"sexual": float64(0), "violence": float64(0),
	}}})
	var ve *editing.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("regrading a row the work does not have: err = %v, want a validation error", err)
	}
	reg := editing.NewRegistry()
	if err := editspec.RegisterWork(reg, testDB); err != nil {
		t.Fatal(err)
	}
	spec, _ := reg.Type(editspec.TypeWork)
	f, _ := spec.Field(editspec.FieldWorkCoversUpstream)
	if err := f.Validate([]any{map[string]any{"image_hash": h}}); err == nil {
		t.Fatal("a regrade without its grades must be refused, or the missing grade reads as 0")
	}
}

func TestLinkSuppressionHidesRelatedLinksAndRefusesAnchors(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "外链")
	execT(t, `INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?, ?, ?, 'v17', ?, 'rule:x'), (?, ?, ?, 'https://example.com/', ?, 'rule:y')`,
		model.EntityTypeWork, work.ID, srcVNDB, model.LinkKindExact,
		model.EntityTypeWork, work.ID, int16(16), model.LinkKindRelated)
	related := editspec.ExternalRefIdentity(16, "https://example.com/")
	if err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkLinksSuppr: []any{related}}); err != nil {
		t.Fatalf("suppress related link: %v", err)
	}
	got := stringsWhere(t, `SELECT r.external_id FROM catalog_external_ref r
		WHERE r.entity_type = ? AND r.entity_id = ? AND `+editspec.NotSuppressedExternalRefSQL("r"),
		model.EntityTypeWork, work.ID)
	if len(got) != 1 || got[0] != "v17" {
		t.Fatalf("visible links = %v", got)
	}
	anchor := editspec.ExternalRefIdentity(srcVNDB, "v17")
	keys := []any{anchor, related}
	if anchor > related {
		keys = []any{related, anchor}
	}
	err := directEdit(t, e, work.ID, map[string]any{editspec.FieldWorkLinksSuppr: keys})
	var ve *editing.ValidationError
	if !errors.As(err, &ve) || !strings.Contains(ve.Reason, "anchor") {
		t.Fatalf("suppressing an identity anchor: err = %v, want a validation error naming the anchor", err)
	}
}

func TestIntroSuppressionPromotesTheNextRowOfTheLang(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "简介")
	execT(t, `INSERT INTO catalog_work_intro (work_id, lang, intro, source_id, provenance)
		VALUES (?, 'en', 'wrong text', ?, 0), (?, 'en', 'right text', ?, 0)`, work.ID, srcVNDB, work.ID, srcBangumi)
	fold := func() string {
		got := stringsWhere(t, `SELECT intro FROM catalog_work_intro i WHERE i.work_id = ? AND i.lang = 'en' AND `+
			editspec.NotSuppressedWorkIntroSQL("i")+` ORDER BY `+editspec.IntroFoldOrderSQL("i.source_id", "i.provenance")+` LIMIT 1`,
			work.ID)
		if len(got) == 0 {
			return ""
		}
		return got[0]
	}
	if got := fold(); got != "wrong text" {
		t.Fatalf("fold before suppression = %q", got)
	}
	if err := directEdit(t, e, work.ID, map[string]any{
		editspec.FieldWorkIntrosSuppr: []any{editspec.IntroIdentity(srcVNDB, "en")},
	}); err != nil {
		t.Fatalf("suppress intro: %v", err)
	}
	if got := fold(); got != "right text" {
		t.Fatalf("fold after suppression = %q, want the bangumi row", got)
	}
}

func TestEngineSeriesAndLabelSuppressionSurviveRebuilds(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "边")
	engineID := idsWhere(t, `INSERT INTO catalog_engine (name, description, aliases) VALUES ('KiriKiri', '', '[]') RETURNING id`)[0]
	execT(t, `INSERT INTO catalog_work_engine (work_id, engine_id, source_id) VALUES (?, ?, ?)`, work.ID, engineID, srcVNDB)
	execT(t, `INSERT INTO catalog_release (work_id, kind, engine_id) VALUES (?, 0, ?)`, work.ID, engineID)
	seriesID := idsWhere(t, `INSERT INTO catalog_series (display_name, source_id, external_id) VALUES ('S', ?, 'grp:1') RETURNING id`, srcDerived)[0]
	execT(t, `INSERT INTO catalog_series_member (series_id, work_id) VALUES (?, ?)`, seriesID, work.ID)
	labelID := idsWhere(t, `INSERT INTO catalog_label (display_name, kind) VALUES ('Brand', 0) RETURNING id`)[0]
	execT(t, `INSERT INTO catalog_work_label (work_id, label_id, kind, source_id) VALUES (?, ?, 0, ?)`, work.ID, labelID, srcVNDB)

	if err := directEdit(t, e, work.ID, map[string]any{
		editspec.FieldWorkEngineIDsSuppr: []any{editspec.WorkEngineIdentity(engineID)},
		editspec.FieldWorkSeriesIDsSuppr: []any{editspec.SeriesIdentity(srcDerived, "grp:1")},
		editspec.FieldWorkLabelsSuppr:    []any{editspec.WorkLabelIdentity(labelID, 0)},
	}); err != nil {
		t.Fatalf("suppress edges: %v", err)
	}
	if got := idsWhere(t, `SELECT engine_id FROM `+editspec.VisibleWorkEnginesSQL+` x WHERE x.work_id = ?`, work.ID); len(got) != 0 {
		t.Fatalf("engines still visible through %v (work or release path)", got)
	}
	if got := idsWhere(t, `SELECT label_id FROM catalog_work_label wl WHERE wl.work_id = ? AND `+
		editspec.NotSuppressedWorkLabelSQL("wl"), work.ID); len(got) != 0 {
		t.Fatalf("labels still visible: %v", got)
	}

	// derivedseries deletes and re-creates a series under a new id; the key is
	// the (source, external_id) anchor, so the edge stays hidden.
	execT(t, `DELETE FROM catalog_series_member WHERE series_id = ?`, seriesID)
	execT(t, `DELETE FROM catalog_series WHERE id = ?`, seriesID)
	rebuilt := idsWhere(t, `INSERT INTO catalog_series (display_name, source_id, external_id) VALUES ('S', ?, 'grp:1') RETURNING id`, srcDerived)[0]
	if rebuilt == seriesID {
		t.Fatal("the rebuild reused the series id; id-independence is untested")
	}
	execT(t, `INSERT INTO catalog_series_member (series_id, work_id) VALUES (?, ?) ON CONFLICT DO NOTHING`, rebuilt, work.ID)
	if got := idsWhere(t, `SELECT m.series_id FROM catalog_series_member m WHERE m.work_id = ? AND `+
		editspec.NotSuppressedWorkSeriesSQL("m"), work.ID); len(got) != 0 {
		t.Fatalf("series membership visible after the rebuild: %v", got)
	}
}
