package editspec_test

import (
	"encoding/json"
	"strings"
	"testing"

	"api/internal/platform/catalog/editspec"
	"api/internal/platform/catalog/model"
	"api/internal/platform/editing"
)

const curatedSource int16 = 12

const vndbSource int16 = 2

func mergeField(t *testing.T, e *editing.Engine, workID int64, key string, value any) map[string]any {
	t.Helper()
	prop, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: workID,
		Patch: map[string]any{key: value}, Actor: realActor(100),
	})
	if err != nil {
		t.Fatalf("propose %s: %v", key, err)
	}
	if _, err := e.MergeProposal(testCtx, prop.ID, realActor(200, "ren"), ""); err != nil {
		t.Fatalf("merge %s: %v", key, err)
	}
	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, workID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	return snap
}

func sameJSON(t *testing.T, field string, got, want any) {
	t.Helper()
	g, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal got: %v", err)
	}
	w, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if string(g) != string(w) {
		t.Fatalf("%s did not round-trip:\n got  %s\n want %s", field, g, w)
	}
}

func TestIntrosLaneAndRoundTrip(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	rows := []model.CatalogWorkIntro{
		{WorkID: work.ID, Lang: "en", Intro: "upstream text", SourceID: vndbSource, Provenance: model.IntroProvenanceSource},
		{WorkID: work.ID, Lang: "zh-Hans", Intro: "机翻", SourceID: curatedSource, Provenance: model.IntroProvenanceMachine},
	}
	if err := testDB.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	want := []any{
		map[string]any{"lang": "en", "intro": "人手で書いた紹介"},
		map[string]any{"lang": "zh-Hans", "intro": "人工简介"},
	}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkIntros, want)
	sameJSON(t, "intros", snap[editspec.FieldWorkIntros], want)

	var all []model.CatalogWorkIntro
	if err := testDB.Where("work_id = ?", work.ID).Order("source_id, lang, provenance").Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("expected 2 curated + 1 importer row, got %d: %+v", len(all), all)
	}
	for _, r := range all {
		if r.SourceID == vndbSource && r.Intro != "upstream text" {
			t.Fatalf("the importer row was rewritten: %+v", r)
		}
		if r.Provenance == model.IntroProvenanceMachine {
			t.Fatalf("a machine row survived in a language the human wrote: %+v", r)
		}
	}

	snap = mergeField(t, e, work.ID, editspec.FieldWorkIntros, []any{})
	sameJSON(t, "intros", snap[editspec.FieldWorkIntros], []any{})
	var left int64
	testDB.Model(&model.CatalogWorkIntro{}).Where("work_id = ?", work.ID).Count(&left)
	if left != 1 {
		t.Fatalf("clearing the curated lane left %d rows, want the importer row only", left)
	}
}

func TestTagEdgesCarryCatalogIDs(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	tags := []model.CatalogTag{
		{Name: "純愛", Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: false},
		{Name: "陵辱", Tier: model.TagTierCore, Kind: model.TagKindContent, Sexual: true},
	}
	if err := testDB.Create(&tags).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkTag{
		WorkID: work.ID, Name: "upstream tag", SourceID: vndbSource, Count: 12,
	}).Error; err != nil {
		t.Fatal(err)
	}

	want := []any{tags[0].ID, tags[1].ID}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkTagIDs, want)
	sameJSON(t, "tag_ids", snap[editspec.FieldWorkTagIDs], want)

	var rows []model.CatalogWorkTag
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).
		Order("name").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("curated tag rows: %+v", rows)
	}
	for _, r := range rows {
		if (r.Name == "陵辱") != r.Sexual {
			t.Fatalf("sexual must follow catalog_tag: %+v", r)
		}
	}
	var upstream int64
	testDB.Model(&model.CatalogWorkTag{}).
		Where("work_id = ? AND source_id = ?", work.ID, vndbSource).Count(&upstream)
	if upstream != 1 {
		t.Fatal("the importer tag edge must survive a curated full replace")
	}

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkTagIDs, []any{float64(9999999)})
	if !strings.Contains(valErr.Reason, "do not exist") {
		t.Fatalf("missing tag ids must be a ValidationError, got %q", valErr.Reason)
	}
}

func TestCuratedTagEdgeResolvesThroughTheSourceMap(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	renamed := model.CatalogTag{Name: "非处女", Tier: model.TagTierLongtail, Kind: model.TagKindContent, Sexual: true}
	if err := testDB.Create(&renamed).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogTagSourceMap{
		SourceID: curatedSource, SourceName: "破鞋", TagID: renamed.ID,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Create(&model.CatalogWorkTag{
		WorkID: work.ID, Name: "破鞋", SourceID: curatedSource, Sexual: true,
	}).Error; err != nil {
		t.Fatal(err)
	}

	snap, err := e.CurrentSnapshot(testCtx, editspec.TypeWork, work.ID)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	sameJSON(t, "tag_ids", snap[editspec.FieldWorkTagIDs], []any{renamed.ID})

	unmapped := model.CatalogTag{Name: "甜作", Tier: model.TagTierLongtail, Kind: model.TagKindContent}
	if err := testDB.Create(&unmapped).Error; err != nil {
		t.Fatal(err)
	}
	want := []any{renamed.ID, unmapped.ID}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkTagIDs, want)
	sameJSON(t, "tag_ids", snap[editspec.FieldWorkTagIDs], want)

	var mapped int64
	testDB.Model(&model.CatalogTagSourceMap{}).
		Where("source_id = ? AND source_name = ? AND tag_id = ?", curatedSource, "甜作", unmapped.ID).
		Count(&mapped)
	if mapped != 1 {
		t.Fatal("assigning a canonical must register its curated identity map row")
	}
}

func TestLabelEngineSeriesEdges(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	label := model.CatalogLabel{DisplayName: "サークル", Kind: model.LabelKindDoujinCircle}
	engine := model.CatalogEngine{Name: "KiriKiri", Description: "", Aliases: []byte(`[]`)}
	curatedSeries := model.CatalogSeries{DisplayName: "curated series", SourceID: curatedSource, ExternalID: "c1"}
	upstreamSeries := model.CatalogSeries{DisplayName: "dlsite series", SourceID: 4, ExternalID: "s1"}
	for _, row := range []any{&label, &engine, &curatedSeries, &upstreamSeries} {
		if err := testDB.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}

	labels := []any{map[string]any{"label_id": label.ID, "kind": int64(model.WorkLabelKindCircle)}}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkLabels, labels)
	sameJSON(t, "labels", snap[editspec.FieldWorkLabels], labels)

	engines := []any{engine.ID}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkEngineIDs, engines)
	sameJSON(t, "engine_ids", snap[editspec.FieldWorkEngineIDs], engines)

	series := []any{curatedSeries.ID}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkSeriesIDs, series)
	sameJSON(t, "series_ids", snap[editspec.FieldWorkSeriesIDs], series)

	valErr := mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkSeriesIDs, []any{upstreamSeries.ID})
	if !strings.Contains(valErr.Reason, "CURATED") {
		t.Fatalf("an upstream series must be a ValidationError naming the curated lane, got %q", valErr.Reason)
	}
}

func TestMissingLabelAndEngineIDsAreValidationErrors(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "欠番参照")
	mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkLabels, []any{
		map[string]any{"label_id": float64(9_000_001), "kind": float64(0)},
	})
	mergeMustReject(t, e, editspec.TypeWork, work.ID, editspec.FieldWorkEngineIDs, []any{float64(9_000_002)})
}

func TestLinksCanonicalizeAndGrade(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	if err := testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: work.ID,
		SourceID: vndbSource, ExternalID: "v999", LinkKind: model.LinkKindExact,
		MatchedBy: "importer",
	}).Error; err != nil {
		t.Fatal(err)
	}

	snap := mergeField(t, e, work.ID, editspec.FieldWorkLinks, []any{
		"https://twitter.com/studio_x?ref=1",
		"https://vndb.org/v123/chars",
		"https://example.com/product/kimi",
	})
	sameJSON(t, "links", snap[editspec.FieldWorkLinks], []any{
		"https://vndb.org/v123",
		"https://x.com/studio_x",
		"https://example.com/product/kimi",
	})

	var refs []model.CatalogExternalRef
	if err := testDB.Where("entity_type = ? AND entity_id = ?", model.EntityTypeWork, work.ID).
		Order("source_id, external_id").Find(&refs).Error; err != nil {
		t.Fatal(err)
	}
	var exact, probable, related int
	for _, r := range refs {
		switch r.LinkKind {
		case model.LinkKindExact:
			exact++
			if r.ExternalID != "v999" || r.MatchedBy != "importer" {
				t.Fatalf("the importer anchor was rewritten: %+v", r)
			}
		case model.LinkKindProbable:
			probable++
			if r.MatchedBy != "curated" {
				t.Fatalf("candidate must be stamped curated: %+v", r)
			}
		case model.LinkKindRelated:
			related++
		}
	}
	if exact != 1 || probable != 1 || related != 2 {
		t.Fatalf("grades: exact=%d probable=%d related=%d (%+v)", exact, probable, related, refs)
	}
}

func TestCoversAndScreenshotsOrderAndLane(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	if err := testDB.Create(&model.CatalogWorkCover{
		WorkID: work.ID, ImageHash: "upstream-cover", SourceID: vndbSource,
	}).Error; err != nil {
		t.Fatal(err)
	}

	covers := []any{
		map[string]any{"image_hash": "hash-a", "portrait_pinned": true},
		map[string]any{"image_hash": "hash-b", "sexual": int64(2)},
	}
	snap := mergeField(t, e, work.ID, editspec.FieldWorkCovers, covers)
	sameJSON(t, "covers", snap[editspec.FieldWorkCovers], covers)

	var rows []model.CatalogWorkCover
	if err := testDB.Where("work_id = ? AND source_id = ?", work.ID, curatedSource).
		Order("sort_order").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].SortOrder != 0 || rows[1].SortOrder != 1 ||
		rows[0].ImageHash != "hash-a" || !rows[0].PortraitPinned || rows[1].Sexual != 2 {
		t.Fatalf("cover rows: %+v", rows)
	}
	var upstream int64
	testDB.Model(&model.CatalogWorkCover{}).
		Where("work_id = ? AND source_id = ?", work.ID, vndbSource).Count(&upstream)
	if upstream != 1 {
		t.Fatal("the importer cover must survive a curated full replace")
	}

	if _, _, err := e.CreateProposal(testCtx, editing.CreateProposalInput{
		EntityType: editspec.TypeWork, EntityID: work.ID,
		Patch: map[string]any{editspec.FieldWorkCovers: []any{
			map[string]any{"image_hash": "a", "portrait_pinned": true},
			map[string]any{"image_hash": "b", "portrait_pinned": true},
		}},
		Actor: realActor(100),
	}); err == nil {
		t.Fatal("two portrait_pinned covers must fail validation")
	}

	shots := []any{
		map[string]any{"image_hash": "s1", "caption": "OP"},
		map[string]any{"image_hash": "s2"},
	}
	snap = mergeField(t, e, work.ID, editspec.FieldWorkScreenshots, shots)
	sameJSON(t, "screenshots", snap[editspec.FieldWorkScreenshots], shots)
}

func TestDisplayNSFWScalar(t *testing.T) {
	e := newEngine(t)
	work := createWork(t, "作品")

	snap := mergeField(t, e, work.ID, editspec.FieldWorkDisplayNSFW, true)
	if snap[editspec.FieldWorkDisplayNSFW] != true {
		t.Fatalf("display_nsfw snapshot: %#v", snap[editspec.FieldWorkDisplayNSFW])
	}
	var w model.CatalogWork
	if err := testDB.First(&w, work.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !w.DisplayNSFW {
		t.Fatal("display_nsfw column was not written")
	}
	if w.ContentRating != model.ContentRatingAllAges {
		t.Fatalf("content_rating moved with display_nsfw: %d", w.ContentRating)
	}
}
