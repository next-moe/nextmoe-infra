package service

import (
	"encoding/json"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const (
	srcBangumiSupply int16 = 3
	srcWeb           int16 = 16
	srcTwitter       int16 = 10
	srcCien          int16 = 14
	srcSteam         int16 = 8
	srcPixiv         int16 = 11
	srcDMM           int16 = 15
)

func setClaimState(t *testing.T, workID int64, state *int16) {
	t.Helper()
	if err := testDB.Exec(`UPDATE catalog_work SET claim_state = ? WHERE id = ?`, state, workID).Error; err != nil {
		t.Fatalf("set claim_state: %v", err)
	}
}

func i16(v int16) *int16 { return &v }

func TestClaimStateProjectedEverywhere(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	live := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ClaimLive")
	draft := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ClaimDraft")
	hidden := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ClaimHidden")
	unstamped := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "ClaimUnstamped")
	bodyless := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Bodyless")
	for i, w := range []*model.CatalogWork{live, draft, hidden, unstamped} {
		claimWork(t, w.ID, "galgame_wiki", int64(7000+i))
	}
	setClaimState(t, live.ID, i16(model.ClaimStateLive))
	setClaimState(t, draft.ID, i16(model.ClaimStateDraft))
	setClaimState(t, hidden.ID, i16(model.ClaimStateHidden))

	want := map[int64]string{
		live.ID: "live", draft.ID: "draft", hidden.ID: "hidden", unstamped.ID: "live",
	}

	// The banned row is deliberately absent from the default browse lane (see
	// TestWorksListClaimStateGate); its projection is proven on the detail and
	// lookup faces below, which carry no claim-state predicate.
	page, err := svc.WorksList(ctx, WorksListFilter{Sort: "id"}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	for _, it := range page.Items {
		if it.ID == hidden.ID {
			t.Fatalf("the default browse lane served banned work %d", it.ID)
		}
	}
	listWant := len(want) - 1
	seen := 0
	for _, it := range page.Items {
		if it.ID == bodyless.ID {
			if it.ClaimedBy != nil {
				t.Fatalf("bodyless work got claimed_by %+v, want null", it.ClaimedBy)
			}
			continue
		}
		if it.ClaimedBy == nil {
			t.Fatalf("work %d lost its claimed_by", it.ID)
		}
		if got := it.ClaimedBy.State; got != want[it.ID] {
			t.Fatalf("list work %d state = %q, want %q", it.ID, got, want[it.ID])
		}
		seen++
	}
	if seen != listWant {
		t.Fatalf("list covered %d claimed works, want %d", seen, listWant)
	}

	for id, state := range want {
		rec, found, err := svc.WorkDetail(ctx, id, PublicInclude{}, false, 0, PublicFields{})
		if err != nil || !found {
			t.Fatalf("WorkDetail %d: found=%v err=%v", id, found, err)
		}
		if rec.ClaimedBy == nil || rec.ClaimedBy.State != state {
			t.Fatalf("detail work %d claimed_by = %+v, want state %q", id, rec.ClaimedBy, state)
		}
	}

	addExternalRef(t, model.EntityTypeWork, hidden.ID, srcVNDB, "v70001", model.LinkKindExact)
	single, found, err := svc.Lookup(ctx, "vndb", "v70001", false)
	if err != nil || !found {
		t.Fatalf("Lookup: found=%v err=%v", found, err)
	}
	if single.ClaimedBy == nil || single.ClaimedBy.State != "hidden" {
		t.Fatalf("lookup claimed_by = %+v, want state hidden", single.ClaimedBy)
	}
	if single.Work == nil || single.Work.ClaimedBy == nil || single.Work.ClaimedBy.State != "hidden" {
		t.Fatalf("lookup work brief claimed_by = %+v, want state hidden", single.Work)
	}
	batch, err := svc.LookupBatch(ctx, []dto.PublicLookupPair{{Source: "vndb", ExternalID: "v70001"}}, false)
	if err != nil {
		t.Fatalf("LookupBatch: %v", err)
	}
	if len(batch) != 1 || batch[0].ClaimedBy == nil || batch[0].ClaimedBy.State != "hidden" {
		t.Fatalf("batch lookup = %+v, want one item with state hidden", batch)
	}
}

func TestClaimStateUnknownValueIsHidden(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "FutureState")
	claimWork(t, w.ID, "galgame_wiki", 7100)
	setClaimState(t, w.ID, i16(99))

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if rec.ClaimedBy == nil || rec.ClaimedBy.State != "hidden" {
		t.Fatalf("unknown claim state projected as %+v, want hidden", rec.ClaimedBy)
	}
}

func TestClaimStateDefaultsLiveForNonWikiClaimer(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "LetmoeClaim")
	claimWork(t, w.ID, "letmoe", 42)

	rec, _, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("WorkDetail: %v", err)
	}
	if rec.ClaimedBy == nil || rec.ClaimedBy.Site != "letmoe" || rec.ClaimedBy.State != "live" {
		t.Fatalf("non-wiki claim = %+v, want site letmoe state live", rec.ClaimedBy)
	}
}

func TestWorksListIncludeRefs(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "RefsWork")
	rel := createRelease(t, w.ID, 2021, 6, 4)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v555", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeRelease, rel.ID, srcDlsite, "RJ123456", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcBangumiSupply, "9999", model.LinkKindProbable)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "https://example.test/x", model.LinkKindRelated)

	bare, err := svc.WorksList(t.Context(), WorksListFilter{Sort: "id"}, "", 50)
	if err != nil {
		t.Fatalf("WorksList bare: %v", err)
	}
	if len(bare.Items) != 1 || bare.Items[0].Refs != nil {
		t.Fatalf("refs surfaced without include= : %+v", bare.Items)
	}

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("refs")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include=refs: %v", err)
	}
	got := page.Items[0].Refs
	if len(got) != 2 {
		t.Fatalf("refs = %+v, want exactly the two EXACT anchors", got)
	}
	if got[0].Source != "dlsite" || got[0].ExternalID != "RJ123456" ||
		got[1].Source != "vndb" || got[1].ExternalID != "v555" {
		t.Fatalf("refs = %+v, want [{dlsite RJ123456} {vndb v555}]", got)
	}
}

func TestWorkDetailEnginesAndLinksAndCreated(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "BlocksWork")
	kiri := createEngine(t, "KiriKiri")
	renpy := createEngine(t, "Ren'Py")
	attachEngine(t, w.ID, renpy)
	attachEngine(t, w.ID, kiri)

	addExternalRef(t, model.EntityTypeWork, w.ID, srcWeb, "https://mill-soft.jp", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcTwitter, "appetite_game", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcCien, "15636", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcSteam, "1917450", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcPixiv, "1338845", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcDlsite, "RJ01088277", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcDMM, "d_774266", model.LinkKindRelated)
	addExternalRef(t, model.EntityTypeWork, w.ID, srcVNDB, "v999", model.LinkKindExact)

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}

	if len(rec.Engines) != 2 || rec.Engines[0].Name != "KiriKiri" || rec.Engines[1].Name != "Ren'Py" {
		t.Fatalf("engines = %+v, want KiriKiri then Ren'Py", rec.Engines)
	}
	if rec.Engines[0].ID != kiri || rec.Engines[1].ID != renpy {
		t.Fatalf("engine ids = %+v, want %d then %d", rec.Engines, kiri, renpy)
	}

	want0 := dto.PublicLink{Source: "vndb", URL: "https://vndb.org/v999", Identity: true, PageID: "v999"}
	if len(rec.Links) == 0 || rec.Links[0] != want0 {
		t.Fatalf("links = %+v, want the exact vndb anchor first as %+v", rec.Links, want0)
	}
	byURL := map[string]bool{}
	for _, l := range rec.Links[1:] {
		byURL[l.URL] = true
		if l.Identity || l.PageID != "" {
			t.Fatalf("a related anchor must not be an identity nor carry page_id: %+v", l)
		}
	}
	for _, want := range []string{
		"https://mill-soft.jp",
		"https://x.com/appetite_game",
		"https://ci-en.dlsite.com/creator/15636",
		"https://store.steampowered.com/app/1917450",
		"https://www.pixiv.net/users/1338845",
	} {
		if !byURL[want] {
			t.Fatalf("links = %+v, missing %s", rec.Links, want)
		}
	}
	if len(rec.Links) != 6 {
		t.Fatalf("links = %+v, want exactly 6 (dlsite/dmm have no defensible template)", rec.Links)
	}

	if rec.Created == "" || rec.Created > rec.Updated {
		t.Fatalf("created=%q updated=%q — created must be present and not after updated", rec.Created, rec.Updated)
	}

	empty := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "EmptyBlocks")
	rec2, _, err := svc.WorkDetail(t.Context(), empty.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("WorkDetail empty: %v", err)
	}
	blob, err := json.Marshal(struct {
		Engines any `json:"engines"`
		Links   any `json:"links"`
	}{rec2.Engines, rec2.Links})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(blob) != `{"engines":[],"links":[]}` {
		t.Fatalf("empty blocks serialized as %s, want [] for both", blob)
	}
}

func TestEngineDescriptionAndAliases(t *testing.T) {
	cleanTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()

	rich := createEngine(t, "Artemis")
	if err := testDB.Exec(
		`UPDATE catalog_engine SET description = ?, aliases = ? WHERE id = ?`,
		"Artemis Engine", `["ArtemisEngine", "  ", "アルテミス"]`, rich).Error; err != nil {
		t.Fatalf("stamp engine: %v", err)
	}
	bare := createEngine(t, "Zilch")

	list, err := svc.EnginesList(t.Context(), EnginesListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("EnginesList: %v", err)
	}
	if list.Total != 2 {
		t.Fatalf("engines total = %d, want 2", list.Total)
	}
	byID := map[int64]int{}
	for i, it := range list.Items {
		byID[it.ID] = i
	}
	got := list.Items[byID[rich]]
	if got.Description != "Artemis Engine" {
		t.Fatalf("list description = %q", got.Description)
	}
	if len(got.Aliases) != 2 || got.Aliases[0] != "ArtemisEngine" || got.Aliases[1] != "アルテミス" {
		t.Fatalf("list aliases = %+v, want the two non-blank entries", got.Aliases)
	}
	if a := list.Items[byID[bare]].Aliases; a == nil || len(a) != 0 {
		t.Fatalf("empty aliases = %+v, want a non-nil empty slice", a)
	}

	rec, found, err := svc.EngineDetail(t.Context(), rich, false)
	if err != nil || !found {
		t.Fatalf("EngineDetail: found=%v err=%v", found, err)
	}
	if rec.Description != "Artemis Engine" || len(rec.Aliases) != 2 {
		t.Fatalf("engine record = %+v", rec)
	}
}

const supplyLogoHash = "0f1e2d3c4b5a69788796a5b4c3d2e1f00f1e2d3c4b5a69788796a5b4c3d2e1f0"

func TestLabelAliasesLangAndWorkCount(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "LabelledWork")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "LabelledR18")
	for i, id := range []int64{w.ID, r18.ID} {
		claimLive(t, id, int64(9330+i))
	}
	labelID := addWorkLabel(t, w.ID, "みるくそふと", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	if err := testDB.Exec(`UPDATE catalog_label SET lang = 'ja', logo_hash = ? WHERE id = ?`,
		supplyLogoHash, labelID).Error; err != nil {
		t.Fatalf("stamp label lang: %v", err)
	}
	if err := testDB.Create(&model.CatalogWorkLabel{
		WorkID: r18.ID, LabelID: labelID, Kind: model.WorkLabelKindBrand, SourceID: i16(srcVNDB),
	}).Error; err != nil {
		t.Fatalf("attach r18 label: %v", err)
	}
	for _, a := range []struct {
		name, lang string
		kind       int16
	}{
		{"Milk Soft", "en", 0}, {"みるくそふと", "ja", 0}, {"Milk Soft", "ja", 0},
		{"milksoft-hint", "", model.AliasKindSearchHint},
	} {
		if err := testDB.Create(&model.CatalogLabelAlias{
			LabelID: labelID, Name: a.name, Lang: a.lang, Kind: a.kind,
		}).Error; err != nil {
			t.Fatalf("create alias: %v", err)
		}
	}

	rec, found, err := svc.Label(t.Context(), labelID, false, false, 50, 0)
	if err != nil || !found {
		t.Fatalf("Label: found=%v err=%v", found, err)
	}
	if rec.Lang != "ja" {
		t.Fatalf("label lang = %q, want ja", rec.Lang)
	}
	if rec.LogoHash != supplyLogoHash {
		t.Fatalf("label logo_hash = %q, want %q", rec.LogoHash, supplyLogoHash)
	}
	if len(rec.Aliases) != 2 ||
		rec.Aliases[0].Value != "Milk Soft" || rec.Aliases[0].Lang != "en" ||
		rec.Aliases[1].Value != "Milk Soft" || rec.Aliases[1].Lang != "ja" {
		t.Fatalf("label aliases = %+v, want Milk Soft under en and ja as separate rows", rec.Aliases)
	}
	if rec.WorkCount != 1 {
		t.Fatalf("sfw label work_count = %d, want 1 (r18 excluded)", rec.WorkCount)
	}
	recNSFW, _, err := svc.Label(t.Context(), labelID, false, true, 50, 0)
	if err != nil {
		t.Fatalf("Label nsfw: %v", err)
	}
	if recNSFW.WorkCount != 2 {
		t.Fatalf("nsfw label work_count = %d, want 2", recNSFW.WorkCount)
	}

	detail, _, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil {
		t.Fatalf("WorkDetail: %v", err)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].Lang != "ja" {
		t.Fatalf("detail labels = %+v, want lang ja", detail.Labels)
	}
	if detail.Labels[0].LogoHash != supplyLogoHash {
		t.Fatalf("detail label logo_hash = %q, want %q", detail.Labels[0].LogoHash, supplyLogoHash)
	}
	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("labels")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include=labels: %v", err)
	}
	for _, it := range page.Items {
		if it.ID != w.ID {
			continue
		}
		if len(it.Labels) != 1 || it.Labels[0].Lang != "ja" {
			t.Fatalf("list labels = %+v, want lang ja", it.Labels)
		}
		if it.Labels[0].LogoHash != supplyLogoHash {
			t.Fatalf("list label logo_hash = %q, want %q", it.Labels[0].LogoHash, supplyLogoHash)
		}
	}
}

func TestTaxonomyTotalsAndTagDetailWorkCount(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	cleanTaxonomyTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	core := createCanonicalTag(t, "fantasy", model.TagTierCore, model.TagKindContent)
	createCanonicalTag(t, "PC", model.TagTierHidden, model.TagKindMeta)
	createCanonicalTag(t, "nakige", model.TagTierCore, model.TagKindContent)

	wSafe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "TagSafe")
	wR18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "TagR18")
	for i, id := range []int64{wSafe.ID, wR18.ID} {
		claimLive(t, id, int64(9340+i))
	}
	if err := testDB.Create(&model.CatalogTagSourceMap{
		SourceID: srcBangumiSupply, SourceName: "ファンタジー", TagID: core,
	}).Error; err != nil {
		t.Fatalf("map tag: %v", err)
	}
	for _, id := range []int64{wSafe.ID, wR18.ID} {
		if err := testDB.Create(&model.CatalogWorkTag{
			WorkID: id, Name: "ファンタジー", Count: 5, SourceID: srcBangumiSupply,
		}).Error; err != nil {
			t.Fatalf("tag work: %v", err)
		}
	}

	first, err := svc.TagsList(ctx, TagsListFilter{}, "", 1)
	if err != nil {
		t.Fatalf("TagsList: %v", err)
	}
	if first.Total != 3 {
		t.Fatalf("tags total = %d, want 3", first.Total)
	}
	second, err := svc.TagsList(ctx, TagsListFilter{}, *first.NextCursor, 1)
	if err != nil {
		t.Fatalf("TagsList page 2: %v", err)
	}
	if second.Total != 3 {
		t.Fatalf("page-2 tags total = %d, want 3 (the whole set, not the tail)", second.Total)
	}
	filtered, err := svc.TagsList(ctx, TagsListFilter{Kind: i16(model.TagKindMeta)}, "", 50)
	if err != nil {
		t.Fatalf("TagsList kind=meta: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 {
		t.Fatalf("kind=meta total=%d items=%d, want 1/1", filtered.Total, len(filtered.Items))
	}

	for _, tc := range []struct {
		nsfw bool
		want int
	}{{false, 1}, {true, 2}} {
		list, err := svc.TagsList(ctx, TagsListFilter{NSFW: tc.nsfw}, "", 50)
		if err != nil {
			t.Fatalf("TagsList: %v", err)
		}
		var listCount int
		for _, it := range list.Items {
			if it.ID == core {
				listCount = it.WorkCount
			}
		}
		rec, found, err := svc.TagDetail(ctx, core, false, tc.nsfw, 50, 0)
		if err != nil || !found {
			t.Fatalf("TagDetail: found=%v err=%v", found, err)
		}
		if rec.WorkCount != tc.want || listCount != tc.want {
			t.Fatalf("nsfw=%v: detail=%d list=%d, want %d", tc.nsfw, rec.WorkCount, listCount, tc.want)
		}
	}

	l1 := addWorkLabel(t, wSafe.ID, "BrandA", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	addWorkLabel(t, wSafe.ID, "BrandB", model.LabelKindPublisher, model.WorkLabelKindPublisher)
	labels, err := svc.LabelsList(ctx, LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList: %v", err)
	}
	if labels.Total != 2 {
		t.Fatalf("labels total = %d, want 2", labels.Total)
	}
	if err := testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id = ?`, l1).Error; err != nil {
		t.Fatalf("soft-delete label: %v", err)
	}
	labels, err = newPublicSvc().LabelsList(ctx, LabelsListFilter{}, "", 50)
	if err != nil {
		t.Fatalf("LabelsList after merge: %v", err)
	}
	if labels.Total != 1 || len(labels.Items) != 1 {
		t.Fatalf("after soft-delete total=%d items=%d, want 1/1", labels.Total, len(labels.Items))
	}
}

func TestWorksListTagIDConjunction(t *testing.T) {
	cleanTables(t)
	cleanTagTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	tagA := createCanonicalTag(t, "romance", model.TagTierCore, model.TagKindContent)
	tagB := createCanonicalTag(t, "school", model.TagTierCore, model.TagKindContent)
	tagC := createCanonicalTag(t, "mecha", model.TagTierCore, model.TagKindContent)
	for _, m := range []struct {
		name string
		id   int64
	}{{"恋愛", tagA}, {"学園", tagB}, {"ロボット", tagC}} {
		if err := testDB.Create(&model.CatalogTagSourceMap{
			SourceID: srcBangumiSupply, SourceName: m.name, TagID: m.id,
		}).Error; err != nil {
			t.Fatalf("map tag: %v", err)
		}
	}

	both := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "BothTags")
	onlyA := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "OnlyA")
	for _, tc := range []struct {
		work int64
		name string
	}{{both.ID, "恋愛"}, {both.ID, "学園"}, {onlyA.ID, "恋愛"}} {
		if err := testDB.Create(&model.CatalogWorkTag{
			WorkID: tc.work, Name: tc.name, Count: 1, SourceID: srcBangumiSupply,
		}).Error; err != nil {
			t.Fatalf("tag work: %v", err)
		}
	}

	ids := func(f WorksListFilter) []int64 {
		t.Helper()
		page, err := svc.WorksList(ctx, f, "", 50)
		if err != nil {
			t.Fatalf("WorksList: %v", err)
		}
		out := make([]int64, 0, len(page.Items))
		for _, it := range page.Items {
			out = append(out, it.ID)
		}
		return out
	}

	if got := ids(WorksListFilter{Sort: "id", TagIDs: []int64{tagA}}); len(got) != 2 {
		t.Fatalf("single tag = %v, want both works", got)
	}
	got := ids(WorksListFilter{Sort: "id", TagIDs: []int64{tagA, tagB}})
	if len(got) != 1 || got[0] != both.ID {
		t.Fatalf("two tags ANDed = %v, want only %d", got, both.ID)
	}
	if got := ids(WorksListFilter{Sort: "id", TagIDs: []int64{tagA, tagC}}); len(got) != 0 {
		t.Fatalf("disjoint tags = %v, want empty", got)
	}
	if got := ids(WorksListFilter{Sort: "id", TagIDs: []int64{tagA, tagA}}); len(got) != 2 {
		t.Fatalf("repeated tag = %v, want both works", got)
	}
}

func TestCalendarBoundsFollowThePopulationGates(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()
	ctx := t.Context()

	mk := func(name, olang string, rating int16, y, m, d int16) {
		t.Helper()
		w := createWorkX(t, galgameMediumID, rating, model.WorkStatusLive, name)
		if err := testDB.Exec(`UPDATE catalog_work SET olang = ? WHERE id = ?`, olang, w.ID).Error; err != nil {
			t.Fatalf("stamp olang: %v", err)
		}
		createRelease(t, w.ID, y, m, d)
	}
	mk("OldJa", "ja", model.ContentRatingAllAges, 2020, 3, 14)
	mk("NewR18", "ja", model.ContentRatingR18, 2024, 8, 1)
	mk("NewEn", "en", model.ContentRatingAllAges, 2026, 1, 20)

	for _, tc := range []struct {
		name             string
		f                CalendarFilter
		wantMin, wantMax int64
	}{
		{"sfw + default olang", CalendarFilter{}, 202003_00, 202003_00},
		{"nsfw + default olang", CalendarFilter{NSFW: true}, 202003_00, 202408_00},
		{"sfw + olang=all", CalendarFilter{OLang: PublicOLang{All: true}}, 202003_00, 202601_00},
	} {
		minOrd, maxOrd, found, err := svc.CalendarBounds(ctx, tc.f)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !found || minOrd != tc.wantMin || maxOrd != tc.wantMax {
			t.Fatalf("%s: bounds=(%d,%d) found=%v, want (%d,%d)",
				tc.name, minOrd, maxOrd, found, tc.wantMin, tc.wantMax)
		}
	}

	if _, _, found, err := svc.CalendarBounds(ctx, CalendarFilter{
		OLang: PublicOLang{Values: []string{"xx-nonexistent"}},
	}); err != nil || found {
		t.Fatalf("empty population: found=%v err=%v, want found=false", found, err)
	}
}
