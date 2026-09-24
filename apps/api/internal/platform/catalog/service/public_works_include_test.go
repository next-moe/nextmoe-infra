package service

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"
)

const testCDNBase = "https://cdn.example.test/img"

func newPublicSvcCDN() *PublicService {
	return NewPublicService(testDB, NewReadService(testDB), testResolve, testCDNBase)
}

func hash64(seed string) string {
	out := seed
	for len(out) < 64 {
		out += "0"
	}
	return out[:64]
}

func addWorkTitle(t *testing.T, workID int64, lang, title string, kind int16) {
	t.Helper()
	addWorkTitleP(t, workID, lang, title, kind, model.WorkTitleProvenanceSource)
}

func addWorkTitleP(t *testing.T, workID int64, lang, title string, kind, provenance int16) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkTitle{
		WorkID: workID, Lang: lang, Title: title, Kind: kind, Provenance: provenance,
	}).Error; err != nil {
		t.Fatalf("create work title %s/%s: %v", lang, title, err)
	}
}

func addWorkIntro(t *testing.T, workID int64, lang, intro string, sourceID, provenance int16) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkIntro{
		WorkID: workID, Lang: lang, Intro: intro, SourceID: sourceID, Provenance: provenance,
	}).Error; err != nil {
		t.Fatalf("create work intro %s: %v", lang, err)
	}
}

func addWorkCover(t *testing.T, workID int64, hash string, sortOrder int, kind string, pinned bool, sexual int16, sourceID int16) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, SortOrder: sortOrder, Kind: kind,
		PortraitPinned: pinned, Sexual: sexual, SourceID: sourceID,
	}).Error; err != nil {
		t.Fatalf("create work cover %s: %v", hash, err)
	}
}

func addWorkRating(t *testing.T, workID int64, sourceID int16, score float64, votes int) {
	t.Helper()
	if err := testDB.Create(&model.CatalogWorkRating{
		WorkID: workID, SourceID: sourceID, Score: score, VoteCount: votes,
	}).Error; err != nil {
		t.Fatalf("create work rating: %v", err)
	}
}

func addWorkLabel(t *testing.T, workID int64, displayName string, labelKind, edgeKind int16) int64 {
	t.Helper()
	l := &model.CatalogLabel{DisplayName: displayName, Kind: labelKind}
	if err := testDB.Create(l).Error; err != nil {
		t.Fatalf("create label: %v", err)
	}
	if err := testDB.Create(&model.CatalogWorkLabel{WorkID: workID, LabelID: l.ID, Kind: edgeKind}).Error; err != nil {
		t.Fatalf("create work label edge: %v", err)
	}
	return l.ID
}

func stubMeta(table map[string]ImageMeta) ImageMetaFunc {
	return func(_ context.Context, hashes []string) (map[string]ImageMeta, error) {
		out := make(map[string]ImageMeta, len(hashes))
		for _, h := range hashes {
			if m, ok := table[h]; ok {
				out[h] = m
			}
		}
		return out, nil
	}
}

func seedRichWork(t *testing.T) int64 {
	t.Helper()
	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Rich Brief")
	addWorkTitle(t, w.ID, "ja", "リッチ", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "zh-Hans", "富简介", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "zh-Hant", "富簡介", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "en", "Rich Brief", model.WorkTitleKindOfficial)
	addWorkIntro(t, w.ID, "ja", "日本語の紹介", srcVNDB, 0)
	addWorkCover(t, w.ID, hash64("aa11"), 0, "main", false, 0, srcVNDB)
	addWorkRating(t, w.ID, srcVNDB, 8.5, 1200)
	addWorkLabel(t, w.ID, "Test Brand", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	createRelease(t, w.ID, 2021, 6, 4)
	return w.ID
}

func TestWorksListDefaultResponseIsByteIdentical(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()
	svc.WithImageMeta(stubMeta(map[string]ImageMeta{
		hash64("aa11"): {Width: 800, Height: 1200, Thumbhash: "TH-AA"},
	}))

	id := seedRichWork(t)
	if err := testDB.Exec(`UPDATE catalog_work SET created_at = ?, updated_at = ? WHERE id = ?`,
		"2025-11-30T09:08:07Z", "2026-01-02T03:04:05Z", id).Error; err != nil {
		t.Fatalf("stamp created_at/updated_at: %v", err)
	}

	page, err := svc.WorksList(t.Context(), WorksListFilter{Sort: "id"}, "", 50)
	if err != nil {
		t.Fatalf("WorksList default: %v", err)
	}
	got, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `{"items":[{"id":` + itoa(id) + `,"medium":"galgame","display_name":"Rich Brief",` +
		`"content_rating":"all_ages","content_limit":"sfw","olang":"ja","release_date":"2021-06-04","claimed_by":null,` +
		`"cover":"` + testCDNBase + `/aa/11/` + hash64("aa11") + `.webp",` +
		`"created":"2025-11-30T09:08:07Z","updated":"2026-01-02T03:04:05Z"}],"next_cursor":null}`
	if string(got) != want {
		t.Fatalf("default works-list response drifted from the frozen contract\n got: %s\nwant: %s", got, want)
	}
}

func TestWorksListIncludeNamesAndIntros(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Pivot")
	addWorkTitle(t, w.ID, "ja", "ぴぼっと", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "ja", "ピボット別名", model.WorkTitleKindAlias)
	addWorkTitle(t, w.ID, "en", "pivot-searchhint", model.WorkTitleKindSearchHint)
	addWorkTitle(t, w.ID, "zh-Hans", "枢轴", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "zh-Hant", "樞軸", model.WorkTitleKindOfficial)
	addWorkTitle(t, w.ID, "ko", "피벗", model.WorkTitleKindOfficial)

	addWorkIntro(t, w.ID, "ja", "原文", srcVNDB, 0)
	addWorkIntro(t, w.ID, "zh-Hans", "机翻", srcVNDB, 1)
	addWorkIntro(t, w.ID, "ko", "한국어", srcVNDB, 0)

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("names,intros,nonsense")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page = %d items, want 1", len(page.Items))
	}
	it := page.Items[0]

	wantNames := map[string]string{
		"ja": "ぴぼっと", "zh-Hans": "枢轴", "zh-Hant": "樞軸", "ko": "피벗",
	}
	if got := localizedValues(it.Localized); !reflect.DeepEqual(got, wantNames) {
		t.Fatalf("localized = %+v, want %+v (alias loses to official, the search hint claims nothing)", got, wantNames)
	}

	wantIntros := []dto.PublicIntro{
		{Lang: "ja", Intro: "原文", Source: "vndb"},
		{Lang: "ko", Intro: "한국어", Source: "vndb"},
		{Lang: "zh-Hans", Intro: "机翻", Source: "vndb", Machine: true},
	}
	if !reflect.DeepEqual(it.Intros, wantIntros) {
		t.Fatalf("intros = %+v, want %+v", it.Intros, wantIntros)
	}

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if !reflect.DeepEqual(rec.Intros, it.Intros) {
		t.Fatalf("detail intros = %+v, list intros = %+v — the two faces share one election",
			rec.Intros, it.Intros)
	}

	if it.Labels != nil || it.Ratings != nil || it.Covers != nil {
		t.Fatalf("unrequested blocks leaked: labels=%v ratings=%v covers=%v", it.Labels, it.Ratings, it.Covers)
	}
}

func TestWorksListIncludeLabelsAndRatings(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Flat")
	labelID := addWorkLabel(t, w.ID, "Brand X", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	addWorkRating(t, w.ID, srcVNDB, 8.4, 900)
	addWorkRating(t, w.ID, srcErogamescape, 78, 51)
	bare := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Bare")

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("labels,ratings")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("page = %d items, want 2", len(page.Items))
	}
	rich, empty := page.Items[0], page.Items[1]
	if rich.ID != w.ID || empty.ID != bare.ID {
		t.Fatalf("unexpected order: %d, %d", rich.ID, empty.ID)
	}

	if len(rich.Labels) != 1 || rich.Labels[0].ID != labelID ||
		rich.Labels[0].DisplayName != "Brand X" ||
		rich.Labels[0].LabelKind != "game_brand" || rich.Labels[0].Kind != "brand" {
		t.Fatalf("labels = %+v", rich.Labels)
	}
	if len(rich.Ratings) != 2 {
		t.Fatalf("ratings = %+v, want 2 source-native rows", rich.Ratings)
	}
	if rich.Ratings[0].Source != "vndb" || rich.Ratings[0].Score != 8.4 || rich.Ratings[0].VoteCount != 900 {
		t.Fatalf("ratings[0] = %+v", rich.Ratings[0])
	}
	if rich.Ratings[1].Source != "erogamescape" || rich.Ratings[1].Score != 78 {
		t.Fatalf("ratings[1] = %+v", rich.Ratings[1])
	}
	if empty.Labels != nil || empty.Ratings != nil {
		t.Fatalf("bare work must omit both blocks, got labels=%v ratings=%v", empty.Labels, empty.Ratings)
	}
}

func TestWorksListIncludeCoverSlots(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()

	portraitHash, bannerHash, sexualHash := hash64("aabb"), hash64("ccdd"), hash64("eeff")
	svc.WithImageMeta(stubMeta(map[string]ImageMeta{
		portraitHash: {Width: 800, Height: 1200, Thumbhash: "TH-P"},
		bannerHash:   {Width: 1920, Height: 1080, Thumbhash: "TH-B"},
		sexualHash:   {Width: 1600, Height: 900, Thumbhash: "TH-S"},
	}))

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Slots")
	addWorkCover(t, w.ID, sexualHash, 0, "main", false, 2, srcVNDB)
	addWorkCover(t, w.ID, bannerHash, 1, "main", false, 0, srcVNDB)
	addWorkCover(t, w.ID, portraitHash, 2, "main", true, 0, srcVNDB)

	inc := ParseWorksListInclude("covers")
	page, err := svc.WorksList(t.Context(), WorksListFilter{Sort: "id", Include: inc}, "", 50)
	if err != nil {
		t.Fatalf("WorksList covers: %v", err)
	}
	cov := page.Items[0].Covers
	if cov == nil || cov.Portrait == nil || cov.Banner == nil {
		t.Fatalf("covers block = %+v, want both slots filled", cov)
	}
	if cov.Portrait.URL != testCDNBase+"/aa/bb/"+portraitHash+".webp" {
		t.Fatalf("portrait url = %q", cov.Portrait.URL)
	}
	if cov.Portrait.Width != 800 || cov.Portrait.Height != 1200 || cov.Portrait.Thumbhash != "TH-P" {
		t.Fatalf("portrait meta = %+v", cov.Portrait)
	}
	if cov.Banner.URL != testCDNBase+"/cc/dd/"+bannerHash+".webp" {
		t.Fatalf("banner url = %q, want the landscape cover (the sexual one is skipped for sfw)", cov.Banner.URL)
	}
	if cov.Banner.Width != 1920 || cov.Banner.Height != 1080 || cov.Banner.Thumbhash != "TH-B" {
		t.Fatalf("banner meta = %+v", cov.Banner)
	}
	if cov.Portrait.Sexual != 0 || cov.Banner.Sexual != 0 {
		t.Fatalf("sfw caller received a sexual-flagged cover: %+v / %+v", cov.Portrait, cov.Banner)
	}

	page, err = svc.WorksList(t.Context(), WorksListFilter{Sort: "id", NSFW: true, Include: inc}, "", 50)
	if err != nil {
		t.Fatalf("WorksList covers nsfw: %v", err)
	}
	cov = page.Items[0].Covers
	if cov.Banner == nil || cov.Banner.URL != testCDNBase+"/cc/dd/"+bannerHash+".webp" {
		t.Fatalf("nsfw banner = %+v, want the display-safe cover: the work is not display_nsfw", cov.Banner)
	}

	setDisplayNSFW(t, w.ID, true)
	page, err = svc.WorksList(t.Context(), WorksListFilter{Sort: "id", NSFW: true, Include: inc}, "", 50)
	if err != nil {
		t.Fatalf("WorksList covers nsfw+display: %v", err)
	}
	cov = page.Items[0].Covers
	if cov.Banner == nil || cov.Banner.URL != testCDNBase+"/cc/dd/"+bannerHash+".webp" {
		t.Fatalf("banner = %+v, want the display-safe cover to keep winning its tier", cov.Banner)
	}
	if cov.Portrait == nil || cov.Portrait.URL != testCDNBase+"/aa/bb/"+portraitHash+".webp" {
		t.Fatalf("nsfw portrait = %+v", cov.Portrait)
	}
}

func TestWorksListCoverSlotsWithoutImageMeta(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "NoMeta")
	addWorkCover(t, w.ID, hash64("1234"), 0, "main", false, 0, srcVNDB)

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("covers")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList: %v", err)
	}
	cov := page.Items[0].Covers
	if cov == nil || cov.Portrait == nil {
		t.Fatalf("covers = %+v, want a portrait fallback", cov)
	}
	if cov.Portrait.Width != 0 || cov.Portrait.Thumbhash != "" {
		t.Fatalf("portrait carries metadata without a lookup: %+v", cov.Portrait)
	}
	if cov.Banner != nil {
		t.Fatalf("banner = %+v, want null without orientation evidence", cov.Banner)
	}
}

func TestWorkDetailCoversCarryImageMeta(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvcCDN()

	known, unknown := hash64("beef"), hash64("dead")
	svc.WithImageMeta(stubMeta(map[string]ImageMeta{
		known: {Width: 1280, Height: 720, Thumbhash: "TH-K"},
	}))

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Detail")
	addWorkCover(t, w.ID, known, 0, "main", false, 0, srcVNDB)
	addWorkCover(t, w.ID, unknown, 1, "main", false, 0, srcVNDB)

	rec, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if len(rec.Covers) != 2 {
		t.Fatalf("covers = %d, want 2", len(rec.Covers))
	}
	byHash := map[string]int{known: -1, unknown: -1}
	for i, c := range rec.Covers {
		for h := range byHash {
			if c.URL == testCDNBase+"/"+h[:2]+"/"+h[2:4]+"/"+h+".webp" {
				byHash[h] = i
			}
		}
	}
	kc := rec.Covers[byHash[known]]
	if kc.Width != 1280 || kc.Height != 720 || kc.Thumbhash != "TH-K" {
		t.Fatalf("known cover meta = %+v", kc)
	}
	uc := rec.Covers[byHash[unknown]]
	if uc.Width != 0 || uc.Height != 0 || uc.Thumbhash != "" {
		t.Fatalf("unknown cover must omit metadata, got %+v", uc)
	}
}

// moyu renders every work title out of the include=names block, so the token
// spellings are a contract in their own right: rename one and it renders empty
// titles site-wide, with no error raised on either side.
func TestParseWorksListIncludeTokenSpellings(t *testing.T) {
	want := WorksListInclude{
		Names: true, Intros: true, Labels: true, Ratings: true, Covers: true,
		Refs: true, Tags: true, Credits: true,
	}
	if inc := ParseWorksListInclude("names,intros,labels,ratings,covers,refs,tags,credits"); inc != want {
		t.Fatalf("include selector = %+v, want every token set: %+v", inc, want)
	}
	for _, tok := range []string{"names", "intros", "labels", "ratings", "covers", "refs", "tags", "credits"} {
		if !ParseWorksListInclude(tok).any() {
			t.Fatalf("token %q selected nothing", tok)
		}
	}
}

func TestParseWorksListIncludeIgnoresUnknown(t *testing.T) {
	inc := ParseWorksListInclude(" names , covers ,relations,,garbage")
	if !inc.Names || !inc.Covers {
		t.Fatalf("known tokens lost: %+v", inc)
	}
	if inc.Intros || inc.Labels || inc.Ratings {
		t.Fatalf("unknown tokens leaked into the selector: %+v", inc)
	}
	if !inc.any() {
		t.Fatal("any() must be true when a token matched")
	}
	if ParseWorksListInclude("").any() {
		t.Fatal("empty include must select nothing")
	}
}

func itoa(v int64) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestWorkLabelsExcludeSoftDeleted(t *testing.T) {
	cleanTables(t)
	svc := newPublicSvc()

	w := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "Duplicated Brand")
	claimLive(t, w.ID, 9470)
	survivor := addWorkLabel(t, w.ID, "みるくそふと", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	merged := addWorkLabel(t, w.ID, "みるくそふと", model.LabelKindGameBrand, model.WorkLabelKindBrand)
	if err := testDB.Exec(`UPDATE catalog_label SET deleted_at = now() WHERE id = ?`, merged).Error; err != nil {
		t.Fatalf("soft-delete the merged label: %v", err)
	}
	now := time.Now()
	if err := testDB.Create(&model.CatalogRedirect{
		EntityType: model.EntityTypeLabel, OldID: merged, CurrentID: survivor,
		MergedAt: &now, Reason: "test",
	}).Error; err != nil {
		t.Fatalf("create redirect: %v", err)
	}

	detail, found, err := svc.WorkDetail(t.Context(), w.ID, PublicInclude{}, false, 0, PublicFields{})
	if err != nil || !found {
		t.Fatalf("WorkDetail: found=%v err=%v", found, err)
	}
	if len(detail.Labels) != 1 || detail.Labels[0].ID != survivor {
		t.Fatalf("public detail labels = %+v, want only the survivor %d", detail.Labels, survivor)
	}

	page, err := svc.WorksList(t.Context(),
		WorksListFilter{Sort: "id", Include: ParseWorksListInclude("labels")}, "", 50)
	if err != nil {
		t.Fatalf("WorksList include=labels: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("page = %d items, want 1", len(page.Items))
	}
	if len(page.Items[0].Labels) != 1 || page.Items[0].Labels[0].ID != survivor {
		t.Fatalf("list labels = %+v, want only the survivor %d", page.Items[0].Labels, survivor)
	}

	s2s, err := NewReadService(testDB).WorkByID(t.Context(), w.ID, 0)
	if err != nil {
		t.Fatalf("S2S WorkByID: %v", err)
	}
	if len(s2s.Labels) != 1 || s2s.Labels[0].LabelID != survivor {
		t.Fatalf("s2s labels = %+v, want only the survivor %d", s2s.Labels, survivor)
	}
}
