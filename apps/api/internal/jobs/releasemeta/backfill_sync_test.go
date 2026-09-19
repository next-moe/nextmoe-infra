package releasemeta

import (
	"context"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBackfillReleaseMeta(t *testing.T) {
	clean(t)
	ctx := context.Background()
	medium := galgameMedium(t)
	reg, err := resolveRegistry(ctx, testDB)
	require.NoError(t, err)
	wiki := "galgame_wiki"

	for i := 0; i < 19; i++ {
		mkWork(t, medium, fmt.Sprintf("rating-volume-pad-%d", i), nil, nil, 0)
	}

	wVndbMoved := mkDateWork(t, medium, "vndb-moved")
	relVndbMoved := mkReleaseYMD(t, wVndbMoved, pi16(2026), pi16(7), pi16(10))
	mkReleaseAnchor(t, relVndbMoved, "r1001", reg.vndbSource)
	mkVndbReleaseAt(t, "v1001", "r1001", 20260814, nil, false, false)

	wVndbRefine := mkDateWork(t, medium, "vndb-refine")
	relVndbRefine := mkReleaseYMD(t, wVndbRefine, pi16(2026), nil, nil)
	mkReleaseAnchor(t, relVndbRefine, "r1002", reg.vndbSource)
	mkVndbReleaseAt(t, "v1002", "r1002", 20261019, nil, false, false)

	wVndbCoarse := mkDateWork(t, medium, "vndb-coarse")
	relVndbCoarse := mkReleaseYMD(t, wVndbCoarse, pi16(2026), pi16(8), pi16(28))
	mkReleaseAnchor(t, relVndbCoarse, "r1003", reg.vndbSource)
	mkVndbReleaseAt(t, "v1003", "r1003", 20269999, nil, false, false)

	wVndbTBA := mkDateWork(t, medium, "vndb-tba")
	relVndbTBA := mkReleaseYMD(t, wVndbTBA, pi16(2026), pi16(8), pi16(27))
	mkReleaseAnchor(t, relVndbTBA, "r1004", reg.vndbSource)
	mkVndbReleaseAt(t, "v1004", "r1004", 99999999, nil, false, false)

	wVndbZero := mkDateWork(t, medium, "vndb-zero")
	relVndbZero := mkReleaseYMD(t, wVndbZero, pi16(2020), pi16(1), pi16(1))
	mkReleaseAnchor(t, relVndbZero, "r1005", reg.vndbSource)
	mkVndbReleaseAt(t, "v1005", "r1005", 0, nil, false, false)

	wVndbTBAEg := mkDateWork(t, medium, "vndb-tba-eg")
	relVndbTBAEg := mkReleaseYMD(t, wVndbTBAEg, nil, nil, nil)
	mkReleaseAnchor(t, relVndbTBAEg, "r1006", reg.vndbSource)
	mkVndbReleaseAt(t, "v1006", "r1006", 99999999, nil, false, false)
	mkWorkAnchor(t, wVndbTBAEg, "801", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 801, "2011-02-03")

	wVndbMissDl := mkDateWork(t, medium, "vndb-missing-dl")
	relVndbMissDl := mkReleaseYMD(t, wVndbMissDl, nil, nil, nil)
	mkReleaseAnchor(t, relVndbMissDl, "r1007", reg.vndbSource)
	mkReleaseAnchor(t, relVndbMissDl, "RJ010001", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010001", "2021-07-15 00:00:00", "2021-07-15 00:00:00+00", "")

	wVndbBeatsDlDate := mkDateWork(t, medium, "vndb-beats-dl-date")
	relVndbBeatsDlDate := mkReleaseYMD(t, wVndbBeatsDlDate, pi16(2020), pi16(1), pi16(1))
	mkReleaseAnchor(t, relVndbBeatsDlDate, "r1008", reg.vndbSource)
	mkVndbReleaseAt(t, "v1008", "r1008", 20220102, nil, false, false)
	mkReleaseAnchor(t, relVndbBeatsDlDate, "RJ010002", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010002", "2019-03-04 00:00:00", "2019-03-04 00:00:00+00", "")

	wDlAPI := mkDateWork(t, medium, "dl-api-beats-column")
	relDlAPI := mkReleaseYMD(t, wDlAPI, pi16(2025), pi16(8), pi16(19))
	mkReleaseAnchor(t, relDlAPI, "RJ010003", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010003", "2025-08-18 16:00:00", "2025-08-18 16:00:00+00", "")

	wDlCol := mkDateWork(t, medium, "dl-column-fallback")
	relDlCol := mkReleaseYMD(t, wDlCol, nil, nil, nil)
	mkReleaseAnchor(t, relDlCol, "RJ010004", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010004", "", "2004-04-12 16:00:00+00", "")

	wDlStale := mkDateWork(t, medium, "dl-stale-column")
	relDlStale := mkReleaseYMD(t, wDlStale, pi16(2026), pi16(4), pi16(8))
	mkReleaseAnchor(t, relDlStale, "RJ010005", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010005", "2026-07-18 00:00:00", "2026-04-08 00:00:00+00", "")

	wDlNone := mkDateWork(t, medium, "dl-no-date")
	relDlNone := mkReleaseYMD(t, wDlNone, pi16(2022), pi16(2), pi16(2))
	mkReleaseAnchor(t, relDlNone, "RJ010006", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010006", "", "", "")

	wDl2099 := mkDateWork(t, medium, "dl-2099")
	relDl2099 := mkReleaseYMD(t, wDl2099, nil, nil, nil)
	mkReleaseAnchor(t, relDl2099, "RJ010007", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010007", "2099-12-31 00:00:00", "2099-12-31 00:00:00+00", "")

	wDlFill := mkDateWork(t, medium, "dl-fill")
	relDlFill := mkReleaseYMD(t, wDlFill, nil, nil, nil)
	mkReleaseAnchor(t, relDlFill, "RJ010008", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010008", "2020-05-01 00:00:00", "2020-05-01 00:00:00+00", "")

	wGcFill := mkDateWork(t, medium, "gc-fill")
	relGcFill := mkReleaseYMD(t, wGcFill, nil, nil, nil)
	mkReleaseAnchor(t, relGcFill, "5001", reg.getchuSource)
	mkGcItem(t, "5001", "2026/10/24")

	wGcShort := mkDateWork(t, medium, "gc-short")
	relGcShort := mkReleaseYMD(t, wGcShort, nil, nil, nil)
	mkReleaseAnchor(t, relGcShort, "5002", reg.getchuSource)
	mkGcItem(t, "5002", "2018/5/4")

	wGcDecade := mkDateWork(t, medium, "gc-decade")
	relGcDecade := mkReleaseYMD(t, wGcDecade, nil, nil, nil)
	mkReleaseAnchor(t, relGcDecade, "5003", reg.getchuSource)
	mkGcItem(t, "5003", "2026/01/下旬")

	wGcTBA := mkDateWork(t, medium, "gc-tba")
	relGcTBA := mkReleaseYMD(t, wGcTBA, pi16(2026), pi16(10), pi16(1))
	mkReleaseAnchor(t, relGcTBA, "5004", reg.getchuSource)
	mkGcItem(t, "5004", "未定")

	wGcCancel := mkDateWork(t, medium, "gc-cancel")
	relGcCancel := mkReleaseYMD(t, wGcCancel, pi16(2020), pi16(1), pi16(1))
	mkReleaseAnchor(t, relGcCancel, "5005", reg.getchuSource)
	mkGcItem(t, "5005", "発売中止")

	wGcPlaceholder := mkDateWork(t, medium, "gc-0001")
	relGcPlaceholder := mkReleaseYMD(t, wGcPlaceholder, pi16(2020), pi16(1), pi16(1))
	mkReleaseAnchor(t, relGcPlaceholder, "5006", reg.getchuSource)
	mkGcItem(t, "5006", "0001/01/01")

	wEgMoved := mkDateWork(t, medium, "eg-moved")
	relEgMoved := mkReleaseYMD(t, wEgMoved, pi16(2000), pi16(1), pi16(1))
	mkWorkAnchor(t, wEgMoved, "802", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 802, "2004-05-28")

	wEg2050 := mkDateWork(t, medium, "eg-2050")
	relEg2050 := mkReleaseYMD(t, wEg2050, pi16(2020), pi16(1), pi16(1))
	mkWorkAnchor(t, wEg2050, "803", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 803, "2050-01-01")

	wEgClaimed := mkWork(t, medium, "eg-claimed", str(wiki), i64(9101), model.ContentRatingSensitive)
	relEgClaimed := mkReleaseYMD(t, wEgClaimed, nil, nil, nil)
	mkWorkAnchor(t, wEgClaimed, "804", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 804, "2005-05-05")

	wEgTwo := mkDateWork(t, medium, "eg-two-releases")
	relEgTwoA := mkReleaseYMD(t, wEgTwo, nil, nil, nil)
	relEgTwoB := mkReleaseYMD(t, wEgTwo, nil, nil, nil)
	mkWorkAnchor(t, wEgTwo, "805", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 805, "2003-03-03")

	wEgMulti := mkDateWork(t, medium, "eg-multi-anchor")
	relEgMulti := mkReleaseYMD(t, wEgMulti, nil, nil, nil)
	mkWorkAnchor(t, wEgMulti, "806", reg.egSource, model.LinkKindExact)
	mkWorkAnchor(t, wEgMulti, "807", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 807, "2015-03-03")

	wBgmPartial := mkDateWork(t, medium, "bgm-partial")
	relBgmPartial := mkReleaseYMD(t, wBgmPartial, nil, nil, nil)
	mkWorkAnchor(t, wBgmPartial, "901", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 901, "2015", false)

	wBgmGarbage := mkDateWork(t, medium, "bgm-garbage")
	relBgmGarbage := mkReleaseYMD(t, wBgmGarbage, pi16(2012), pi16(12), pi16(12))
	mkWorkAnchor(t, wBgmGarbage, "902", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 902, "TBA?", false)

	wEgBeatsBgm := mkDateWork(t, medium, "eg-beats-bgm")
	relEgBeatsBgm := mkReleaseYMD(t, wEgBeatsBgm, nil, nil, nil)
	mkWorkAnchor(t, wEgBeatsBgm, "808", reg.egSource, model.LinkKindExact)
	mkEgGame(t, 808, "2001-02-03")
	mkWorkAnchor(t, wEgBeatsBgm, "903", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 903, "2002-03-04", false)

	wHuman := mkDateWork(t, medium, "human-date")
	relHuman := mkReleaseYMD(t, wHuman, pi16(2010), pi16(1), pi16(1))
	mkReleaseAnchor(t, relHuman, "RJ010009", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010009", "2024-01-02 00:00:00", "2024-01-02 00:00:00+00", "")
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_release SET field_provenance =
		 '{"released_y":[{"source":"curated","at":"2026-01-01T00:00:00Z"}]}' WHERE id = ?`, relHuman).Error)

	wDelRel := mkDateWork(t, medium, "deleted-release")
	relDel := mkReleaseYMD(t, wDelRel, nil, nil, nil)
	mkReleaseAnchor(t, relDel, "RJ010010", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010010", "2018-01-01 00:00:00", "2018-01-01 00:00:00+00", "")
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET deleted_at = now() WHERE id = ?`, relDel).Error)

	wDelWork := mkDateWork(t, medium, "deleted-work")
	relDelWork := mkReleaseYMD(t, wDelWork, nil, nil, nil)
	mkReleaseAnchor(t, relDelWork, "RJ010011", reg.dlsiteSource)
	mkDlWorkFull(t, "RJ010011", "2018-02-02 00:00:00", "2018-02-02 00:00:00+00", "")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, wDelWork).Error)

	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET updated_at = now() - interval '1 hour' WHERE id = ?`, wDlFill).Error)
	var touchBefore time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, wDlFill).Scan(&touchBefore).Error)

	rDlAdult := mkWork(t, medium, "rating-dl-adult", nil, nil, 0)
	relRDlAdult := mkRelease(t, rDlAdult, 2000, 1, 1)
	mkReleaseAnchor(t, relRDlAdult, "RJ000101", reg.dlsiteSource)
	mkDlWork(t, "RJ000101", "", "3")

	rDlR15 := mkWork(t, medium, "rating-dl-r15", nil, nil, 0)
	relRDlR15 := mkRelease(t, rDlR15, 2000, 1, 1)
	mkReleaseAnchor(t, relRDlR15, "RJ000102", reg.dlsiteSource)
	mkDlWork(t, "RJ000102", "", "2")

	rDlAll := mkWork(t, medium, "rating-dl-all", str(wiki), i64(9103), 0)
	relRDlAll := mkRelease(t, rDlAll, 2000, 1, 1)
	mkReleaseAnchor(t, relRDlAll, "RJ000103", reg.dlsiteSource)
	mkDlWork(t, "RJ000103", "", "1")

	rBgm := mkWork(t, medium, "rating-bgm-nsfw", nil, nil, 0)
	mkWorkAnchor(t, rBgm, "708", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 708, "", true)

	rBgmFalse := mkWork(t, medium, "rating-bgm-sfw", nil, nil, 0)
	mkWorkAnchor(t, rBgmFalse, "709", reg.bangumiSource, model.LinkKindExact)
	mkSubject(t, 709, "", false)

	rRated := mkWork(t, medium, "rating-already-rated", nil, nil, model.ContentRatingR18)
	relRRated := mkRelease(t, rRated, 2000, 1, 1)
	mkReleaseAnchor(t, relRRated, "RJ000104", reg.dlsiteSource)
	mkDlWork(t, "RJ000104", "", "2")

	age18 := int16(18)
	age0 := int16(0)

	rVndb18 := mkWork(t, medium, "rating-vndb-minage", nil, nil, 0)
	mkWorkAnchor(t, rVndb18, "v901", reg.vndbSource, model.LinkKindExact)
	mkVndbRelease(t, "v901", "r901", &age18, false, false)

	rVndbEro := mkWork(t, medium, "rating-vndb-ero", nil, nil, 0)
	mkWorkAnchor(t, rVndbEro, "v902", reg.vndbSource, model.LinkKindExact)
	mkVndbRelease(t, "v902", "r902", nil, true, false)

	rVndbPatch := mkWork(t, medium, "rating-vndb-patch-only", nil, nil, 0)
	mkWorkAnchor(t, rVndbPatch, "v903", reg.vndbSource, model.LinkKindExact)
	mkVndbRelease(t, "v903", "r903", &age18, true, true)

	rVndbSafe := mkWork(t, medium, "rating-vndb-all-ages", nil, nil, 0)
	mkWorkAnchor(t, rVndbSafe, "v904", reg.vndbSource, model.LinkKindExact)
	mkVndbRelease(t, "v904", "r904", &age0, false, false)

	rVndbBeatsDl := mkWork(t, medium, "rating-vndb-beats-dl-allages", nil, nil, 0)
	relVndbBeatsDl := mkRelease(t, rVndbBeatsDl, 2000, 1, 1)
	mkReleaseAnchor(t, relVndbBeatsDl, "RJ000105", reg.dlsiteSource)
	mkDlWork(t, "RJ000105", "", "1")
	mkWorkAnchor(t, rVndbBeatsDl, "v905", reg.vndbSource, model.LinkKindExact)
	mkVndbRelease(t, "v905", "r905", &age18, true, false)

	rEgTrue := mkWork(t, medium, "rating-eg-erogame", nil, nil, 0)
	mkWorkAnchor(t, rEgTrue, "611", reg.egSource, model.LinkKindExact)
	mkEgErogame(t, 611, true)

	rEgFalse := mkWork(t, medium, "rating-eg-not-erogame", nil, nil, 0)
	mkWorkAnchor(t, rEgFalse, "612", reg.egSource, model.LinkKindExact)
	mkEgErogame(t, 612, false)

	rEgBeatsDl := mkWork(t, medium, "rating-eg-beats-dl-allages", nil, nil, 0)
	relEgBeatsDl := mkRelease(t, rEgBeatsDl, 2000, 1, 1)
	mkReleaseAnchor(t, relEgBeatsDl, "RJ000106", reg.dlsiteSource)
	mkDlWork(t, "RJ000106", "", "1")
	mkWorkAnchor(t, rEgBeatsDl, "613", reg.egSource, model.LinkKindExact)
	mkEgErogame(t, 613, true)

	rBgmMeta := mkWork(t, medium, "rating-bgm-meta-tag", nil, nil, 0)
	mkWorkAnchor(t, rBgmMeta, "710", reg.bangumiSource, model.LinkKindExact)
	mkSubjectMeta(t, 710, false, "游戏", "R18")

	st, err := Run(ctx, runOpts(false))
	require.NoError(t, err)
	assert.Equal(t, 33, st.DatesCandidates)
	assert.Equal(t, 10, st.AllFilled)
	assert.Equal(t, 7, st.AllMoved)
	assert.Equal(t, 3, st.AllCleared)
	assert.Equal(t, 12, st.DatesUnknown)
	assert.Equal(t, 1, st.DatesHuman)
	assert.Zero(t, st.DatesSame)
	assert.Zero(t, st.DatesWritten)
	assert.Zero(t, st.DatesLost)
	assert.Equal(t, 4, st.VndbDateMoved)
	assert.Equal(t, 1, st.VndbDateCleared)
	assert.Zero(t, st.VndbDateFilled)
	assert.Equal(t, 3, st.DlDateFilled)
	assert.Equal(t, 2, st.DlDateMoved)
	assert.Equal(t, 3, st.GcDateFilled)
	assert.Equal(t, 1, st.GcDateCleared)
	assert.Equal(t, 3, st.EgDateFilled)
	assert.Equal(t, 1, st.EgDateMoved)
	assert.Equal(t, 1, st.EgDateCleared)
	assert.Equal(t, 1, st.BgmDateFilled)
	assert.Equal(t, 1, st.VndbMissing)
	assert.Equal(t, 33, st.RatingCandidates, "every rating-0 work; rRated excluded")
	assert.Equal(t, 3, st.RatingVndbR18, "minage + has_ero + the dl-allages preemption")
	assert.Equal(t, 1, st.RatingDlR18)
	assert.Equal(t, 1, st.RatingDlSensitive)
	assert.Equal(t, 1, st.RatingDlAllAges, "explicit all-ages verdict keeps the row at 0")
	assert.Equal(t, 2, st.RatingEgR18, "erogame=true + the dl-allages preemption")
	assert.Equal(t, 2, st.RatingBgmR18, "nsfw flag + R18 meta_tag")
	assert.Equal(t, 23, st.RatingNoVerdict)
	assert.Equal(t, 9, st.RatingPlanned)
	assert.Zero(t, st.DatesWritten+st.RatingFilled+st.RatingSkippedNonEmpty+st.Errors)
	y, _, _ := relDate(t, relDlFill)
	assert.Nil(t, y, "dry run writes nothing")
	assert.Equal(t, int16(0), workRating(t, rDlAdult), "dry run writes nothing")

	st, err = Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 10, st.AllFilled)
	assert.Equal(t, 7, st.AllMoved)
	assert.Equal(t, 3, st.AllCleared)
	assert.Equal(t, st.AllFilled+st.AllMoved+st.AllCleared, st.DatesWritten)
	assert.Zero(t, st.DatesLost+st.Errors)
	assert.Equal(t, 9, st.RatingFilled)

	assertDate(t, relVndbMoved, 2026, 8, 14)
	assertDate(t, relVndbRefine, 2026, 10, 19)
	assertDate(t, relVndbCoarse, 2026, 0, 0)
	assertNoDate(t, relVndbTBA)
	assertDate(t, relVndbZero, 2020, 1, 1)
	assertDate(t, relVndbTBAEg, 2011, 2, 3)
	assertDate(t, relVndbMissDl, 2021, 7, 15)
	assertDate(t, relVndbBeatsDlDate, 2022, 1, 2)
	assertDate(t, relDlAPI, 2025, 8, 18)
	assertDate(t, relDlCol, 2004, 4, 13)
	assertDate(t, relDlStale, 2026, 7, 18)
	assertDate(t, relDlNone, 2022, 2, 2)
	assertNoDate(t, relDl2099)
	assertDate(t, relDlFill, 2020, 5, 1)
	assertDate(t, relGcFill, 2026, 10, 24)
	assertDate(t, relGcShort, 2018, 5, 4)
	assertDate(t, relGcDecade, 2026, 1, 0)
	assertNoDate(t, relGcTBA)
	assertDate(t, relGcCancel, 2020, 1, 1)
	assertDate(t, relGcPlaceholder, 2020, 1, 1)
	assertDate(t, relEgMoved, 2004, 5, 28)
	assertNoDate(t, relEg2050)
	assertNoDate(t, relEgClaimed)
	assertNoDate(t, relEgTwoA)
	assertNoDate(t, relEgTwoB)
	assertDate(t, relEgMulti, 2015, 3, 3)
	assertDate(t, relBgmPartial, 2015, 0, 0)
	assertDate(t, relBgmGarbage, 2012, 12, 12)
	assertDate(t, relEgBeatsBgm, 2001, 2, 3)
	assertDate(t, relHuman, 2010, 1, 1)
	assertNoDate(t, relDel)
	assertNoDate(t, relDelWork)

	assert.Equal(t, model.ContentRatingR18, workRating(t, rDlAdult))
	assert.Equal(t, model.ContentRatingSensitive, workRating(t, rDlR15))
	assert.Equal(t, int16(0), workRating(t, rDlAll), "an explicit all-ages verdict leaves the row at 0")
	assert.Equal(t, model.ContentRatingR18, workRating(t, rBgm))
	assert.Equal(t, int16(0), workRating(t, rBgmFalse), "nsfw=false never infers a rating")
	assert.Equal(t, model.ContentRatingR18, workRating(t, rRated), "non-zero rating untouched")
	assert.Equal(t, model.ContentRatingR18, workRating(t, rVndb18))
	assert.Equal(t, model.ContentRatingR18, workRating(t, rVndbEro))
	assert.Equal(t, int16(0), workRating(t, rVndbPatch), "an 18+ patch is not the work's rating")
	assert.Equal(t, int16(0), workRating(t, rVndbSafe))
	assert.Equal(t, model.ContentRatingR18, workRating(t, rVndbBeatsDl),
		"work-level vndb verdict outranks the 全年齢版 SKU's dlsite age")
	assert.Equal(t, model.ContentRatingR18, workRating(t, rEgTrue))
	assert.Equal(t, model.ContentRatingR18, workRating(t, rEgBeatsDl),
		"work-level EG erogame outranks the 全年齢版 SKU's dlsite age")
	assert.Equal(t, int16(0), workRating(t, rEgFalse), "erogame=false never infers a rating")
	assert.Equal(t, model.ContentRatingR18, workRating(t, rBgmMeta))

	var touchAfter time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, wDlFill).Scan(&touchAfter).Error)
	assert.True(t, touchAfter.After(touchBefore), "a written date touches the work")

	st, err = Run(ctx, runOpts(true))
	require.NoError(t, err)
	assert.Equal(t, 33, st.DatesCandidates)
	assert.Zero(t, st.AllFilled+st.AllMoved+st.AllCleared, "second pass plans zero date changes")
	assert.Zero(t, st.DatesWritten+st.DatesLost)
	assert.Equal(t, 20, st.DatesSame)
	assert.Equal(t, 12, st.DatesUnknown)
	assert.Equal(t, 1, st.DatesHuman)
	assert.Equal(t, 24, st.RatingCandidates, "the nine filled works left the set")
	assert.Zero(t, st.RatingPlanned, "second pass plans zero")
	assert.Zero(t, st.RatingFilled+st.Errors, "second pass writes zero")
	assert.Equal(t, 1, st.RatingDlAllAges, "all-ages verdicts persist as counted no-ops")
}
