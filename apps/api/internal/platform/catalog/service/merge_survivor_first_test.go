package service

import (
	"fmt"
	"maps"
	"testing"

	"api/internal/platform/catalog/dto"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The survivor of 211826 → 3993 and the shape of 9453: the loser's rows are
// created first, so every one of them has the lower id.
const (
	survivorDisplay = "魔女っ娘ア・ラ・モードII～華麗なる変身、ふたたび～"
	loserJaTitle    = "魔女っ娘ア・ラ・モード2　～華麗なる変身、ふたたび～"
)

var (
	sPortrait  = hash64("5a01")
	sShot      = hash64("5b01")
	lPortrait  = hash64("1a01")
	lLandscape = hash64("1a02")
	lShot      = hash64("1b01")
	lShared    = hash64("1a03")
)

func survivorFirstMeta() map[string]ImageMeta {
	return map[string]ImageMeta{
		sPortrait:  {Width: 256, Height: 362},
		sShot:      {Width: 1280, Height: 720},
		lPortrait:  {Width: 600, Height: 900},
		lLandscape: {Width: 1280, Height: 720},
		lShot:      {Width: 1920, Height: 1080},
		lShared:    {Width: 200, Height: 280},
	}
}

type survivorFace struct {
	DisplayName  string
	Localized    map[string]string
	Intros       map[string]string
	Portrait     string
	Banner       string
	ListCover    string
	ListPortrait string
	ListBanner   string
	ListNames    map[string]string
	Exact        []string
}

func (f survivorFace) clone() survivorFace {
	f.Localized, f.Intros, f.ListNames = maps.Clone(f.Localized), maps.Clone(f.Intros), maps.Clone(f.ListNames)
	f.Exact = append([]string{}, f.Exact...)
	return f
}

func localizedStrings(in map[string]dto.PublicLocalizedName) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = fmt.Sprintf("%s|%s|%t", v.Value, v.Kind, v.Machine)
	}
	return out
}

func slotURLOf(s *dto.PublicCoverSlot) string {
	if s == nil {
		return ""
	}
	return s.Origin + ":" + s.URL
}

func faceOf(t *testing.T, svc *PublicService, id int64) survivorFace {
	t.Helper()
	rec, found, err := svc.WorkDetail(t.Context(), id, PublicInclude{}, false, 0, PublicFields{})
	require.NoError(t, err)
	require.True(t, found)
	f := survivorFace{
		DisplayName: rec.DisplayName,
		Localized:   localizedStrings(rec.Localized),
		Intros:      map[string]string{},
		Portrait:    slotURLOf(portraitOf(rec.CoverSlots)),
		Banner:      slotURLOf(bannerOf(rec.CoverSlots)),
	}
	for _, in := range rec.Intros {
		f.Intros[in.Lang] = fmt.Sprintf("%s|%s|%t", in.Intro, in.Source, in.Machine)
	}
	page, err := svc.WorksList(t.Context(), WorksListFilter{IDs: []int64{id},
		Include: ParseWorksListInclude("covers,names")}, "", 10)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	it := page.Items[0]
	f.ListCover, f.ListNames = it.Cover, localizedStrings(it.Localized)
	f.ListPortrait, f.ListBanner = slotURLOf(portraitOf(it.Covers)), slotURLOf(bannerOf(it.Covers))
	require.NoError(t, testDB.Raw(`SELECT s.key || ':' || r.external_id FROM catalog_external_ref r
		JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ? AND r.link_kind = ? ORDER BY 1`,
		model.EntityTypeWork, id, model.LinkKindExact).Scan(&f.Exact).Error)
	return f
}

func newSurvivorFirstSvc() *PublicService {
	return newPublicSvcCDN().WithImageMeta(stubMeta(survivorFirstMeta()))
}

func seedSurvivor(t *testing.T, s int64) {
	t.Helper()
	addWorkTitle(t, s, "ja", survivorDisplay, model.WorkTitleKindOfficial)
	addWorkTitle(t, s, "zh-Hans", "魔法少女时装秀", model.WorkTitleKindAlias)
	addWorkTitle(t, s, "zh-Hant", "魔法少女時裝秀", model.WorkTitleKindOfficial)
	addWorkTitleP(t, s, "en", "Witch Girl a la Mode II", model.WorkTitleKindOfficial, model.WorkTitleProvenanceMachine)

	addWorkIntro(t, s, "ja", "survivor curated ja", model.SourceCurated, model.IntroProvenanceSource)
	addWorkIntro(t, s, "zh-Hans", "survivor mt zh", model.SourceCurated, model.IntroProvenanceMachine)
	addWorkIntro(t, s, "en", "survivor mt en", srcVNDB, model.IntroProvenanceMachine)

	addWorkCover(t, s, sPortrait, 0, "main", false, 0, srcVNDB)
	addWorkCover(t, s, lShared, 1, "main", false, 0, srcVNDB)
	require.NoError(t, testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: s, ImageHash: sShot, SourceID: srcDlsite}).Error)

	addExternalRef(t, model.EntityTypeWork, s, srcVNDB, "v100", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, s, srcErogamescape, "5000", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, s, model.SourceBangumi, "100", model.LinkKindExact)
}

func seedLoser(t *testing.T, l int64, withVNDB bool) {
	t.Helper()
	addWorkTitle(t, l, "ja", loserJaTitle, model.WorkTitleKindOfficial)
	addWorkTitle(t, l, "zh-Hans", "魔女时装秀2", model.WorkTitleKindOfficial)
	addWorkTitleP(t, l, "zh-Hant", "魔女時裝秀2", model.WorkTitleKindOfficial, model.WorkTitleProvenanceMachine)
	addWorkTitle(t, l, "en", "Majokko a la Mode 2", model.WorkTitleKindOfficial)
	addWorkTitle(t, l, "ko", "마녀 아 라 모드 2", model.WorkTitleKindOfficial)
	addWorkTitle(t, l, "ja", "まじょっこ2", model.WorkTitleKindSearchHint)

	addWorkIntro(t, l, "ja", "loser dlsite ja", srcDlsite, model.IntroProvenanceSource)
	addWorkIntro(t, l, "zh-Hans", "loser mt zh", srcDlsite, model.IntroProvenanceMachine)
	addWorkIntro(t, l, "en", "loser vndb en", srcVNDB, model.IntroProvenanceSource)
	addWorkIntro(t, l, "ko", "loser vndb ko", srcVNDB, model.IntroProvenanceSource)
	addWorkIntro(t, l, "de", "loser curated de", model.SourceCurated, model.IntroProvenanceSource)

	addWorkCover(t, l, lPortrait, 0, "main", true, 0, srcVNDB)
	addWorkCover(t, l, lLandscape, 1, "main", false, 0, srcVNDB)
	addWorkCover(t, l, lShared, 2, "main", false, 0, srcVNDB)
	require.NoError(t, testDB.Create(&model.CatalogWorkScreenshot{
		WorkID: l, ImageHash: lShot, SourceID: srcVNDB}).Error)

	if withVNDB {
		addExternalRef(t, model.EntityTypeWork, l, srcVNDB, "v200", model.LinkKindExact)
	}
	addExternalRef(t, model.EntityTypeWork, l, srcErogamescape, "6000", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, l, model.SourceBangumi, "200", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, l, srcDlsite, "RJ123", model.LinkKindExact)
}

func enriched(before survivorFace) survivorFace {
	want := before.clone()
	want.Localized["ko"] = "마녀 아 라 모드 2|official|false"
	want.ListNames["ko"] = want.Localized["ko"]
	want.Intros["en"] = "loser vndb en|vndb|false"
	want.Intros["ko"] = "loser vndb ko|vndb|false"
	want.Intros["de"] = "loser curated de|curated|false"
	want.Exact = []string{"bangumi:100", "dlsite:RJ123", "erogamescape:5000", "vndb:v100"}
	return want
}

func TestSurvivorFirstMergeKeepsTheSurvivorsFace(t *testing.T) {
	cleanTables(t)
	svc := newSurvivorFirstSvc()
	l := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, loserJaTitle)
	s := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, survivorDisplay)
	seedLoser(t, l.ID, true)
	seedSurvivor(t, s.ID)

	before := faceOf(t, svc, s.ID)
	require.Equal(t, survivorDisplay+"|official|false", before.Localized["ja"])
	require.Equal(t, "魔法少女时装秀|alias|false", before.Localized["zh-Hans"])
	require.Equal(t, "cover:"+svc.imageURL(sPortrait), before.Portrait)
	require.Equal(t, "screenshot:"+svc.imageURL(sShot), before.Banner)

	mergeNow(t, model.EntityTypeWork, l.ID, s.ID)

	assert.Equal(t, enriched(before), faceOf(t, svc, s.ID))
	assertLoserRefs(t, s.ID, map[string]int16{
		"vndb:v200": model.LinkKindProbable, "erogamescape:6000": model.LinkKindRelated,
		"bangumi:200": model.LinkKindRelated, "dlsite:RJ123": model.LinkKindExact,
	})

	var aliases []string
	require.NoError(t, testDB.Raw(`SELECT title FROM catalog_work_title
		WHERE work_id = ? AND lang = '' AND kind = ?`, s.ID, model.WorkTitleKindAlias).Scan(&aliases).Error)
	assert.ElementsMatch(t, []string{"Majokko a la Mode 2", loserJaTitle, "魔女时装秀2"}, aliases,
		"the loser's titles in the survivor's languages stay findable as language-less aliases")
	var hints int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_title WHERE work_id = ? AND kind = ?`,
		s.ID, model.WorkTitleKindSearchHint).Scan(&hints).Error)
	assert.EqualValues(t, 1, hints, "a search hint moves as it is")

	var moved []model.CatalogWorkCover
	require.NoError(t, testDB.Where("work_id = ? AND merged_in", s.ID).Order("sort_order").Find(&moved).Error)
	require.Len(t, moved, 2, "the shared hash collides and stays the survivor's")
	for _, c := range moved {
		assert.False(t, c.PortraitPinned, "a merged-in cover loses its pin")
		assert.Greater(t, c.SortOrder, 1, "numbered after the survivor's own covers")
	}

	archived := archiveCounts(t, s.ID, l.ID)
	assert.Equal(t, map[string]int64{
		"catalog_work_title": 4, "catalog_work_intro": 3, "catalog_work_cover": 1,
	}, archived)
}

func TestStragglerSweepKeepsTheSurvivorsFace(t *testing.T) {
	cleanTables(t)
	svc := newSurvivorFirstSvc()
	l := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, loserJaTitle)
	s := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, survivorDisplay)
	seedSurvivor(t, s.ID)
	before := faceOf(t, svc, s.ID)

	mergeNow(t, model.EntityTypeWork, l.ID, s.ID)
	seedLoser(t, l.ID, true)

	rep, err := testMerge.SweepStragglers(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, rep.Repaired)

	assert.Equal(t, enriched(before), faceOf(t, svc, s.ID))
	assertLoserRefs(t, s.ID, map[string]int16{
		"vndb:v200": model.LinkKindRelated, "erogamescape:6000": model.LinkKindRelated,
		"bangumi:200": model.LinkKindRelated, "dlsite:RJ123": model.LinkKindExact,
	})
	assert.NotEmpty(t, archiveCounts(t, s.ID, l.ID), "the replay archives what it drops under the merge's proposal")
}

func assertLoserRefs(t *testing.T, workID int64, want map[string]int16) {
	t.Helper()
	var rows []struct {
		Ref  string
		Kind int16
	}
	require.NoError(t, testDB.Raw(`SELECT s.key || ':' || r.external_id AS ref, r.link_kind AS kind
		FROM catalog_external_ref r JOIN catalog_source s ON s.id = r.source_id
		WHERE r.entity_type = ? AND r.entity_id = ?`, model.EntityTypeWork, workID).Scan(&rows).Error)
	got := map[string]int16{}
	for _, r := range rows {
		if _, ok := want[r.Ref]; ok {
			got[r.Ref] = r.Kind
		}
	}
	assert.Equal(t, want, got)
}

func archiveCounts(t *testing.T, survivor, loser int64) map[string]int64 {
	t.Helper()
	var rows []struct {
		Table string `gorm:"column:table_name"`
		N     int64  `gorm:"column:n"`
	}
	require.NoError(t, testDB.Raw(`SELECT a.table_name, count(*) AS n FROM catalog_merge_archive a
		JOIN catalog_merge_proposal p ON p.id = a.proposal_id
		WHERE p.source_entity_id = ? AND p.target_entity_id = ? GROUP BY 1`, loser, survivor).Scan(&rows).Error)
	out := map[string]int64{}
	for _, r := range rows {
		out[r.Table] = r.N
	}
	return out
}
