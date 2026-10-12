package service

import (
	"context"
	"testing"

	"api/internal/platform/catalog/model"
)

func cleanTaxonomyTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_work_engine", "catalog_engine", "catalog_tag_intro",
		"catalog_work_cover", "catalog_work_screenshot",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func createEngine(t *testing.T, name string) int64 {
	t.Helper()
	e := &model.CatalogEngine{Name: name, Description: "", Aliases: []byte("[]")}
	if err := testDB.Create(e).Error; err != nil {
		t.Fatalf("create engine %s: %v", name, err)
	}
	return e.ID
}

func attachEngine(t *testing.T, workID, engineID int64) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkEngine{
		WorkID: workID, EngineID: engineID, SourceID: srcVNDB,
	}).Error; err != nil {
		t.Fatalf("attach engine: %v", err)
	}
}

func createCanonicalTag(t *testing.T, name string, tier, kind int16) int64 {
	t.Helper()
	tag := &model.CatalogTag{Name: name, Tier: tier, Kind: kind}
	if err := testDB.Create(tag).Error; err != nil {
		t.Fatalf("create tag %s: %v", name, err)
	}
	return tag.ID
}

func TestTaxonomyListsKeysetAndCounts(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	wSafe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "SafeWork")
	wR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "R18Work")
	wStub := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusStub, "StubWork")
	wASMR := createWorkX(t, 5, model.ContentRatingAllAges, model.WorkStatusLive, "AsmrWork")
	for i, id := range []int64{wSafe.ID, wR18.ID, wStub.ID, wASMR.ID} {
		claimLive(t, id, int64(9300+i))
	}

	brandID := addWorkLabel(t, wSafe.ID, "Alcot", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	for _, w := range []int64{wR18.ID, wStub.ID, wASMR.ID} {
		if err := testDB.Create(&model.CatalogWorkLabel{WorkID: w, LabelID: brandID, Kind: model.WorkLabelKindBrand}).Error; err != nil {
			t.Fatalf("extra label edge: %v", err)
		}
	}
	if err := testDB.Create(&model.CatalogWorkLabel{WorkID: wSafe.ID, LabelID: brandID, Kind: model.WorkLabelKindPublisher}).Error; err != nil {
		t.Fatalf("second-kind label edge: %v", err)
	}
	circleID := addWorkLabel(t, wSafe.ID, "Circle Zero", model.LabelKindDoujinCircle, model.WorkLabelKindCircle)
	if err := testDB.Exec(`DELETE FROM catalog_work_label WHERE label_id = ?`, circleID).Error; err != nil {
		t.Fatalf("detach circle: %v", err)
	}

	labels, err := svc.LabelsList(ctx, LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList sfw: %v", err)
	}
	if len(labels.Items) != 2 || labels.NextCursor != nil {
		t.Fatalf("labels page = %d items cursor=%v, want 2 + terminal", len(labels.Items), labels.NextCursor)
	}
	if labels.Items[0].ID != brandID || labels.Items[0].Kind != "game_brand" {
		t.Fatalf("labels[0] = %+v", labels.Items[0])
	}
	if labels.Items[0].WorkCount != 1 {
		t.Fatalf("sfw brand work_count = %d, want 1 (r18/stub/asmr excluded, double edge counted once)", labels.Items[0].WorkCount)
	}
	if labels.Items[1].WorkCount != 0 {
		t.Fatalf("unattached label work_count = %d, want 0", labels.Items[1].WorkCount)
	}

	labelsNSFW, err := svc.LabelsList(ctx, LabelsListFilter{NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList nsfw: %v", err)
	}
	if labelsNSFW.Items[0].WorkCount != 2 {
		t.Fatalf("nsfw brand work_count = %d, want 2 (r18 now counted)", labelsNSFW.Items[0].WorkCount)
	}

	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", LabelID: brandID}, labels.Items[0].WorkCount)
	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", LabelID: brandID, NSFW: true}, labelsNSFW.Items[0].WorkCount)

	r18OnlyID := addWorkLabel(t, wR18.ID, "R18 Only Brand", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	attached, err := svc.LabelsList(ctx, LabelsListFilter{HasWorks: true}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList has_works sfw: %v", err)
	}
	if len(attached.Items) != 1 || attached.Items[0].ID != brandID || attached.Items[0].WorkCount != 1 {
		t.Fatalf("has_works sfw labels = %+v, want only brand", attached.Items)
	}
	if attached.Total != 1 {
		t.Fatalf("has_works sfw total = %d, want 1 (the empty and r18-only rows leave the total too)", attached.Total)
	}
	attachedNSFW, err := svc.LabelsList(ctx, LabelsListFilter{HasWorks: true, NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList has_works nsfw: %v", err)
	}
	if len(attachedNSFW.Items) != 2 || attachedNSFW.Total != 2 || attachedNSFW.Items[1].ID != r18OnlyID {
		t.Fatalf("has_works nsfw labels = %+v total=%d, want brand + r18-only", attachedNSFW.Items, attachedNSFW.Total)
	}
	if err := testDB.Exec(`DELETE FROM catalog_work_label WHERE label_id = ?`, r18OnlyID).Error; err != nil {
		t.Fatalf("detach r18-only: %v", err)
	}
	if err := testDB.Exec(`DELETE FROM catalog_label WHERE id = ?`, r18OnlyID).Error; err != nil {
		t.Fatalf("drop r18-only label: %v", err)
	}

	kind := model.LabelKindDoujinCircle
	filtered, err := svc.LabelsList(ctx, LabelsListFilter{Kind: &kind}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList kind: %v", err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].ID != circleID {
		t.Fatalf("kind=doujin_circle = %+v, want only %d", filtered.Items, circleID)
	}

	if err := testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id = ?`, circleID).Error; err != nil {
		t.Fatalf("soft delete label: %v", err)
	}
	afterDelete, err := svc.LabelsList(ctx, LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList after soft delete: %v", err)
	}
	if len(afterDelete.Items) != 1 || afterDelete.Items[0].ID != brandID {
		t.Fatalf("soft-deleted label still listed: %+v", afterDelete.Items)
	}

	coreTagID := createCanonicalTag(t, "fantasy", model.TagTierCore, model.TagKindContent)
	metaTagID := createCanonicalTag(t, "PC", model.TagTierHidden, model.TagKindMeta)
	const srcBangumi int16 = 3
	for _, name := range []string{"ファンタジー", "奇幻"} {
		if err := testDB.Create(&model.CatalogTagSourceMap{SourceID: srcBangumi, SourceName: name, TagID: coreTagID}).Error; err != nil {
			t.Fatalf("map source tag %s: %v", name, err)
		}
		for _, w := range []int64{wSafe.ID, wR18.ID} {
			if err := testDB.Create(&model.CatalogWorkTag{WorkID: w, Name: name, Count: 1, SourceID: srcBangumi}).Error; err != nil {
				t.Fatalf("work tag %s: %v", name, err)
			}
		}
	}

	tagPage, err := svc.TagsList(ctx, TagsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("TagsList: %v", err)
	}
	if len(tagPage.Items) != 2 {
		t.Fatalf("tags page = %d, want 2", len(tagPage.Items))
	}
	if tagPage.Items[0].ID != coreTagID || tagPage.Items[0].Tier != "core" || tagPage.Items[0].Kind != "content" {
		t.Fatalf("tags[0] = %+v", tagPage.Items[0])
	}
	if tagPage.Items[0].WorkCount != 1 {
		t.Fatalf("sfw tag work_count = %d, want 1 (two mapped source tags on one work count once)", tagPage.Items[0].WorkCount)
	}
	tagPageNSFW, err := svc.TagsList(ctx, TagsListFilter{NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("TagsList nsfw: %v", err)
	}
	if tagPageNSFW.Items[0].WorkCount != 2 {
		t.Fatalf("nsfw tag work_count = %d, want 2", tagPageNSFW.Items[0].WorkCount)
	}
	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", TagIDs: []int64{coreTagID}}, tagPage.Items[0].WorkCount)
	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", TagIDs: []int64{coreTagID}, NSFW: true}, tagPageNSFW.Items[0].WorkCount)

	attachedTags, err := svc.TagsList(ctx, TagsListFilter{HasWorks: true}, "", 50)
	if err != nil {
		t.Fatalf("TagsList has_works: %v", err)
	}
	if len(attachedTags.Items) != 1 || attachedTags.Items[0].ID != coreTagID || attachedTags.Total != 1 {
		t.Fatalf("has_works tags = %+v total=%d, want only the mapped core tag", attachedTags.Items, attachedTags.Total)
	}

	tier := model.TagTierHidden
	tagKind := model.TagKindMeta
	onlyMeta, err := svc.TagsList(ctx, TagsListFilter{Tier: &tier, Kind: &tagKind}, "", 50)
	if err != nil {
		t.Fatalf("TagsList filtered: %v", err)
	}
	if len(onlyMeta.Items) != 1 || onlyMeta.Items[0].ID != metaTagID {
		t.Fatalf("tier=hidden&kind=meta = %+v, want only %d", onlyMeta.Items, metaTagID)
	}

	kirikiri := createEngine(t, "KiriKiri")
	renpy := createEngine(t, "Ren'Py")
	attachEngine(t, wSafe.ID, kirikiri)
	attachEngine(t, wR18.ID, kirikiri)
	attachEngine(t, wStub.ID, kirikiri)

	engines, err := svc.EnginesList(ctx, EnginesListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList: %v", err)
	}
	if len(engines.Items) != 2 || engines.Items[0].ID != kirikiri || engines.Items[0].Name != "KiriKiri" {
		t.Fatalf("engines page = %+v", engines.Items)
	}
	if engines.Items[0].WorkCount != 1 || engines.Items[1].WorkCount != 0 {
		t.Fatalf("engine counts = [%d,%d], want [1,0]", engines.Items[0].WorkCount, engines.Items[1].WorkCount)
	}
	enginesNSFW, err := svc.EnginesList(ctx, EnginesListFilter{NSFW: true}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList nsfw: %v", err)
	}
	if enginesNSFW.Items[0].WorkCount != 2 {
		t.Fatalf("nsfw engine work_count = %d, want 2", enginesNSFW.Items[0].WorkCount)
	}
	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", EngineID: kirikiri}, engines.Items[0].WorkCount)
	assertCountMatchesWorksList(t, svc, WorksListFilter{Sort: "id", EngineID: kirikiri, NSFW: true}, enginesNSFW.Items[0].WorkCount)
	_ = renpy
	_ = wASMR

	p1, err := svc.EnginesList(ctx, EnginesListFilter{}, "", 1)
	if err != nil {
		t.Fatalf("engines p1: %v", err)
	}
	if len(p1.Items) != 1 || p1.Items[0].ID != kirikiri || p1.NextCursor == nil {
		t.Fatalf("engines p1 = %+v cursor=%v", p1.Items, p1.NextCursor)
	}
	p2, err := svc.EnginesList(ctx, EnginesListFilter{}, *p1.NextCursor, 1)
	if err != nil {
		t.Fatalf("engines p2: %v", err)
	}
	if len(p2.Items) != 1 || p2.Items[0].ID != renpy {
		t.Fatalf("engines p2 = %+v, want [%d]", p2.Items, renpy)
	}
	if p2.NextCursor != nil {
		t.Fatalf("engines p2 is the last page and must end the walk, got cursor=%v", *p2.NextCursor)
	}
	if _, err := svc.EnginesList(ctx, EnginesListFilter{}, "!!!not-base64!!!", 50); err != ErrBadCursor {
		t.Fatalf("malformed engines cursor = %v, want ErrBadCursor", err)
	}
	if _, err := svc.LabelsList(ctx, LabelsListFilter{}, *p1.NextCursor, 50); err != ErrBadCursor {
		t.Fatalf("engines cursor on the labels lane = %v, want ErrBadCursor", err)
	}
	if _, err := svc.TagsList(ctx, TagsListFilter{}, *p1.NextCursor, 50); err != ErrBadCursor {
		t.Fatalf("engines cursor on the tags lane = %v, want ErrBadCursor", err)
	}
	worksPage, err := svc.WorksList(ctx, WorksListFilter{Sort: "id"}, "", 1)
	if err != nil {
		t.Fatalf("works p1: %v", err)
	}
	if _, err := svc.EnginesList(ctx, EnginesListFilter{}, *worksPage.NextCursor, 50); err != ErrBadCursor {
		t.Fatalf("works cursor on the engines lane = %v, want ErrBadCursor", err)
	}
}

func assertCountMatchesWorksList(t *testing.T, svc *PublicService, f WorksListFilter, want int) {
	t.Helper()
	if got := worksListTotal(t, svc, f); got != want {
		t.Fatalf("works?filter=%+v holds %d rows but work_count advertised %d — a count must never disagree with its own member list",
			f, got, want)
	}
}

func worksListTotal(t *testing.T, svc *PublicService, f WorksListFilter) int {
	t.Helper()
	f.OLang, f.IncludeTotal = PublicOLang{All: true}, true
	page, err := svc.WorksList(t.Context(), f, "", 100)
	if err != nil {
		t.Fatalf("WorksList %+v: %v", f, err)
	}
	if int64(len(page.Items)) != page.Total {
		t.Fatalf("works?filter=%+v returned %d rows under a total of %d", f, len(page.Items), page.Total)
	}
	return len(page.Items)
}

func claimLive(t *testing.T, workID, productWorkID int64) {
	t.Helper()
	claimWork(t, workID, "galgame_wiki", productWorkID)
	setClaimState(t, workID, i16(model.ClaimStateLive))
}

func TestEngineDetailRefsExactOnly(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	eng := createEngine(t, "RealLive")
	wSafe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "EngSafe")
	wR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "EngR18")
	for i, id := range []int64{wSafe.ID, wR18.ID} {
		claimLive(t, id, int64(9310+i))
	}
	attachEngine(t, wSafe.ID, eng)
	attachEngine(t, wR18.ID, eng)

	const srcWiki int16 = 12
	addExternalRef(t, model.EntityTypeEngine, eng, srcWiki, "1001", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeEngine, eng, srcVNDB, "e999", model.LinkKindProbable)
	addExternalRef(t, model.EntityTypeEngine, eng, srcDlsite, "https://example.test", model.LinkKindRelated)

	rec, found, err := svc.EngineDetail(ctx, eng, false)
	if err != nil || !found {
		t.Fatalf("EngineDetail: found=%v err=%v", found, err)
	}
	if rec.Name != "RealLive" || rec.WorkCount != 1 {
		t.Fatalf("engine detail = %+v, want RealLive/work_count 1", rec)
	}
	if len(rec.Refs) != 1 || rec.Refs[0].Source != sourceKeyCurated || rec.Refs[0].ExternalID != "1001" {
		t.Fatalf("engine refs = %+v, want only the exact wiki anchor", rec.Refs)
	}

	nsfwRec, _, err := svc.EngineDetail(ctx, eng, true)
	if err != nil {
		t.Fatalf("EngineDetail nsfw: %v", err)
	}
	if nsfwRec.WorkCount != 2 {
		t.Fatalf("nsfw engine work_count = %d, want 2", nsfwRec.WorkCount)
	}

	if _, found, _ := svc.EngineDetail(ctx, 999999, false); found {
		t.Fatal("unknown engine id must be found=false")
	}
}

func TestTagDetailIntrosAlwaysPresent(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	bare := createCanonicalTag(t, "bare", model.TagTierCore, model.TagKindContent)
	described := createCanonicalTag(t, "described", model.TagTierCore, model.TagKindContent)

	rec, found, err := svc.TagDetail(ctx, bare, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("TagDetail bare: found=%v err=%v", found, err)
	}
	if rec.Intros == nil || len(rec.Intros) != 0 {
		t.Fatalf("intros must be an empty slice (serialized []), got %#v", rec.Intros)
	}

	// Both contenders must be UPSTREAM ids. This used to seed source 12 as
	// "another source" — it is the curated lane, and the day the human lane
	// started winning its language the case stopped being about source_id order
	// at all. TestTagIntroHumanLaneWinsTheLangFold owns that direction now.
	const srcGetchu int16 = 17
	for _, in := range []struct {
		lang, body string
		src        int16
	}{
		{"zh-Hans", "低位来源胜出", srcErogamescape},
		{"zh-Hans", "另一来源", srcGetchu},
		{"ja", "日本語", srcVNDB},
	} {
		if err := testDB.Create(&model.CatalogTagIntro{
			TagID: described, Lang: in.lang, Intro: in.body, SourceID: in.src,
		}).Error; err != nil {
			t.Fatalf("create tag intro: %v", err)
		}
	}
	rec, _, err = svc.TagDetail(ctx, described, false, false, 50, 0)
	if err != nil {
		t.Fatalf("TagDetail described: %v", err)
	}
	if len(rec.Intros) != 2 {
		t.Fatalf("intros = %+v, want one row per language", rec.Intros)
	}
	if rec.Intros[0].Lang != "ja" || rec.Intros[0].Intro != "日本語" {
		t.Fatalf("intros[0] = %+v, want the ja row first (lang ASC)", rec.Intros[0])
	}
	if rec.Intros[1].Lang != "zh-Hans" || rec.Intros[1].Intro != "低位来源胜出" {
		t.Fatalf("intros[1] = %+v, want the lowest source_id to win the language", rec.Intros[1])
	}
	if rec.Intros[1].Source != "erogamescape" {
		t.Fatalf("intro source = %q, want the PUBLIC source key", rec.Intros[1].Source)
	}
}

func TestScreenshotMetaEnrichment(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ShotHost")
	known, unknown := hash64("bb22"), hash64("cc33")
	for i, h := range []string{known, unknown} {
		if err := testDB.Create(&model.CatalogWorkScreenshot{
			WorkID: w.ID, ImageHash: h, SortOrder: i, Caption: "cap", SourceID: srcVNDB,
		}).Error; err != nil {
			t.Fatalf("create screenshot: %v", err)
		}
	}

	bare := newPublicSvcCDN()
	rec, found, err := bare.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail unwired: found=%v err=%v", found, err)
	}
	if len(rec.Screenshots) != 2 {
		t.Fatalf("screenshots = %d, want 2", len(rec.Screenshots))
	}
	for _, sc := range rec.Screenshots {
		if sc.URL == "" {
			t.Fatalf("screenshot url must render without enrichment: %+v", sc)
		}
		if sc.Width != 0 || sc.Height != 0 || sc.Thumbhash != "" {
			t.Fatalf("unwired lookup must leave the meta keys empty: %+v", sc)
		}
	}

	svc := newPublicSvcCDN().WithImageMeta(stubMeta(map[string]ImageMeta{
		known: {Width: 1280, Height: 720, Thumbhash: "shot-hash"},
	}))
	rec, _, err = svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("WorkDetail wired: %v", err)
	}
	if rec.Screenshots[0].Width != 1280 || rec.Screenshots[0].Height != 720 || rec.Screenshots[0].Thumbhash != "shot-hash" {
		t.Fatalf("known screenshot not enriched: %+v", rec.Screenshots[0])
	}
	if rec.Screenshots[1].Width != 0 || rec.Screenshots[1].Height != 0 || rec.Screenshots[1].Thumbhash != "" {
		t.Fatalf("unknown hash must degrade, not guess: %+v", rec.Screenshots[1])
	}
}

func TestWorkMediaMetaBatchesCoversAndScreenshots(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	ctx := t.Context()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "BatchHost")
	coverHash, shotHash := hash64("dd44"), hash64("ee55")
	addWorkCover(t, w.ID, coverHash, 0, "main", true, 0, srcVNDB)
	if err := testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: w.ID, ImageHash: shotHash, SortOrder: 0, SourceID: srcVNDB,
	}).Error; err != nil {
		t.Fatalf("create screenshot: %v", err)
	}

	calls := 0
	seen := map[string]bool{}
	svc := newPublicSvcCDN().WithImageMeta(func(_ context.Context, hashes []string) (map[string]ImageMeta, error) {
		calls++
		out := make(map[string]ImageMeta, len(hashes))
		for _, h := range hashes {
			seen[h] = true
			out[h] = ImageMeta{Width: 800, Height: 600, Thumbhash: "th-" + h[:4]}
		}
		return out, nil
	})
	rec, found, err := svc.WorkDetail(ctx, w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if calls != 1 {
		t.Fatalf("image_service called %d times, want exactly 1 batch for the whole record", calls)
	}
	if !seen[coverHash] || !seen[shotHash] {
		t.Fatalf("the single batch must carry both grains: cover=%v shot=%v", seen[coverHash], seen[shotHash])
	}
	if rec.Covers[0].Thumbhash == "" || rec.Screenshots[0].Thumbhash == "" {
		t.Fatalf("both grains must be enriched: cover=%+v shot=%+v", rec.Covers[0], rec.Screenshots[0])
	}
}
