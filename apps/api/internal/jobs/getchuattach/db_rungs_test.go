package getchuattach

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTitleCutReachesTheSubtitle(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "トリプルペアリング ミニファンディスク")
	mkReleaseYMD(t, w, i16(2001), i16(6), i16(1))
	insertFetched(t, "810", "トリプルペアリング ミニファンディスク 初回版", "", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.TitleCut)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, w, attachedRelease(t, "810").WorkID)
	assert.Equal(t, ruleTitleCut, getchuRefs(t, "810")[0].MatchedBy)
}

func TestLongerCutWinsOverTheSeriesTitle(t *testing.T) {
	requireDB(t)
	series := mkWork(t, "トリプルペアリング")
	fan := mkWork(t, "トリプルペアリング ミニファンディスク")
	mkReleaseYMD(t, series, i16(2001), i16(6), i16(1))
	mkReleaseYMD(t, fan, i16(2001), i16(6), i16(1))
	insertFetched(t, "811", "トリプルペアリング ミニファンディスク DX", "", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.TitleCut)
	assert.Equal(t, fan, attachedRelease(t, "811").WorkID)
	assert.Empty(t, getchuRefsForWork(t, series))
}

func TestEGBrandDateAttachesAcrossASeriesPrefix(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Witch Tutor Catalog Name")
	mkReleaseYMD(t, w, i16(2001), i16(6), i16(1))
	insertEGBrand(t, 20, "おかずクラブ", "")
	insertEGGame(t, 50, "家庭教師は魔女先生! ～個人授業はエッチ重視で～", "2001-06-01", 20)
	mapEGWork(t, w, 50)
	insertFetched(t, "812", "今日のおかず 家庭教師は魔女先生！～個人授業はエッチ重視で～", "おかずクラブ", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrand)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, w, attachedRelease(t, "812").WorkID)
	assert.Equal(t, ruleEGBrand, getchuRefs(t, "812")[0].MatchedBy)
}

func TestEGBrandMatchesByFurigana(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Elf Furigana Host")
	mkReleaseYMD(t, w, i16(2001), i16(6), i16(1))
	insertEGBrand(t, 21, "エルフ", "elf")
	insertEGGame(t, 51, "Elf Furigana Game", "2001-06-01", 21)
	mapEGWork(t, w, 51)
	insertFetched(t, "813", "Elf Furigana Game Extra Prefix", "elf", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrand)
	assert.Equal(t, w, attachedRelease(t, "813").WorkID)
}

func TestCensoredTitleAttachesSameBrandSameDay(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Censor Host Work")
	mkReleaseYMD(t, w, i16(2001), i16(6), i16(1))
	insertEGBrand(t, 22, "CensorBrand", "")
	insertEGGame(t, 52, "催眠痴漢Episode1", "2001-06-01", 22)
	mapEGWork(t, w, 52)
	insertFetched(t, "814", "〇眠〇漢Episode1", "CensorBrand", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrand)
	assert.Equal(t, w, attachedRelease(t, "814").WorkID)
}

func TestBundleIsNeverAttachedByTitleNorMinted(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Bundle Game コンプリートBOX")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	seedMintBrand(t, 30, "BundleBrand")
	insertItem(t, item{
		GetchuID: "815", Title: "Bundle Game コンプリートBOX", Brand: "BundleBrand", BrandID: 30,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Bundles)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, before, workCount(t))
	assert.Empty(t, getchuRefs(t, "815"))
}

func TestGoodsSkipped(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 31, "GoodsBrand")
	insertItem(t, item{
		GetchuID: "816", Title: "抱き枕カバー スペシャル", Brand: "GoodsBrand", BrandID: 31,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Goods)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.MintedLive)
	assert.Empty(t, getchuRefs(t, "816"))
}

func TestAllAgesNeverMinted(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 32, "AllAgesBrand")
	insertItem(t, item{
		GetchuID: "817", Title: "All Ages Only Unique Title", Brand: "AllAgesBrand", BrandID: 32,
		ReleaseDate: "2001/01/01",
	})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, before, workCount(t))
}

func TestReissueNeverMinted(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 33, "ReissueBrand")
	insertItem(t, item{
		GetchuID: "818", Title: "Some Game 廉価版", Brand: "ReissueBrand", BrandID: 33,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Reissues)
	assert.Zero(t, st.MintedLive)
}

func TestUndatedAndCancelledNeverMinted(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 34, "DateBrand")
	insertItem(t, item{
		GetchuID: "819", Title: "Cancelled Unique Game", Brand: "DateBrand", BrandID: 34,
		ReleaseDate: "発売中止", Adult: true,
	})
	insertItem(t, item{
		GetchuID: "820", Title: "Undated Unique Game", Brand: "DateBrand", BrandID: 34,
		ReleaseDate: "coming soon", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Cancelled)
	assert.Equal(t, 1, st.Undated)
	assert.Zero(t, st.MintedLive)
}

func TestUnknownBrandNeverMinted(t *testing.T) {
	requireDB(t)
	insertEGBrand(t, 35, "KnownBrand", "")
	insertItem(t, item{
		GetchuID: "821", Title: "Unknown Brand Unique Game", Brand: "NoSuchBrand", BrandID: 99,
		ReleaseDate: "2001/01/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.BrandUnknown)
	assert.Zero(t, st.MintedLive)
}

func TestHoldoutReportCountsEveryAttachRule(t *testing.T) {
	requireDB(t)
	vndb, getchu := sourceID(t, "vndb"), sourceID(t, "getchu")

	janV := mkWork(t, "Holdout JAN VNDB")
	janVRel := mkReleaseYMD(t, janV, i16(2001), i16(1), i16(1))
	mkRef(t, model.EntityTypeWork, janV, vndb, "vHV", model.LinkKindExact)
	mkRef(t, model.EntityTypeRelease, janVRel, getchu, "50", model.LinkKindExact)
	insertVNDBRelease(t, "rH1", 4910000000001)
	mkRef(t, model.EntityTypeRelease, janVRel, vndb, "rH1", model.LinkKindExact)
	insertItem(t, item{GetchuID: "50", Title: "Holdout JAN VNDB Item", ReleaseDate: "2001/01/01", JAN: 4910000000001})

	janE := mkWork(t, "Holdout JAN EG")
	janERel := mkReleaseYMD(t, janE, i16(2001), i16(1), i16(1))
	mkRef(t, model.EntityTypeWork, janE, vndb, "vHE", model.LinkKindExact)
	mkRef(t, model.EntityTypeRelease, janERel, getchu, "51", model.LinkKindExact)
	insertEGGame(t, 60, "Holdout JAN EG Game", "2001-01-01", 1)
	mapEGWork(t, janE, 60)
	insertEGItemJAN(t, 11, "4910000000002")
	insertEGItemGame(t, 11, 60)
	insertItem(t, item{GetchuID: "51", Title: "Holdout JAN EG Item", ReleaseDate: "2001/01/01", JAN: 4910000000002})

	td := mkWork(t, "Holdout Title Date")
	tdRel := mkReleaseYMD(t, td, i16(2001), i16(1), i16(1))
	mkRef(t, model.EntityTypeWork, td, vndb, "vHT", model.LinkKindExact)
	mkRef(t, model.EntityTypeRelease, tdRel, getchu, "52", model.LinkKindExact)
	insertFetched(t, "52", "Holdout Title Date", "", "2001/01/01")

	cut := mkWork(t, "Holdout Cut Prefix")
	cutRel := mkReleaseYMD(t, cut, i16(2001), i16(6), i16(1))
	mkRef(t, model.EntityTypeWork, cut, vndb, "vHC", model.LinkKindExact)
	mkRef(t, model.EntityTypeRelease, cutRel, getchu, "53", model.LinkKindExact)
	insertFetched(t, "53", "Holdout Cut Prefix DX", "", "2001/06/01")

	br := mkWork(t, "Holdout Brand Host")
	brRel := mkReleaseYMD(t, br, i16(2001), i16(6), i16(1))
	mkRef(t, model.EntityTypeWork, br, vndb, "vHB", model.LinkKindExact)
	mkRef(t, model.EntityTypeRelease, brRel, getchu, "54", model.LinkKindExact)
	insertEGBrand(t, 70, "HoldoutBrand", "")
	insertEGGame(t, 61, "Holdout Brand Game", "2001-06-01", 70)
	mapEGWork(t, br, 61)
	insertFetched(t, "54", "Series Prefix Holdout Brand Game", "HoldoutBrand", "2001/06/01")

	st, err := Run(context.Background(), Opts{
		DSN: testDSN, GetchuDSN: gcTestDSN, EGDSN: egTestDSN, HoldoutReport: true,
		Now: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	assert.Equal(t, 5, st.Population)
	assert.Equal(t, 5, st.HoldoutCorrect)
	assert.Zero(t, st.HoldoutWrong)
	got := map[string]int{}
	for _, r := range st.HoldoutRules {
		got[r.Rule] = r.Correct
	}
	assert.Equal(t, 1, got[ruleJanVNDB])
	assert.Equal(t, 1, got[ruleJanEG])
	assert.Equal(t, 1, got[ruleTitleDate])
	assert.Equal(t, 1, got[ruleTitleCut])
	assert.Equal(t, 1, got[ruleEGBrand])
}
