package getchuattach

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEGBrandNearDateAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Tetra Catalog Host")
	mkReleaseYMD(t, w, i16(2009), i16(2), i16(13))
	insertEGBrand(t, 30, "じぃすぽっと", "")
	insertEGGame(t, 80, "霊甲テトラ ～密着スーツにスベり込むッ触手と白濁液の恐怖!～", "2009-02-13", 30)
	mapEGWork(t, w, 80)
	insertFetched(t, "820", "今日のおかず 霊甲テトラ～密着スーツにスベり込むッ触手と白濁液の恐怖！～", "じぃすぽっと", "2009/02/27")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrandNear)
	assert.Zero(t, st.EGBrand)
	assert.Equal(t, w, attachedRelease(t, "820").WorkID)
	assert.Equal(t, ruleEGBrandNear, getchuRefs(t, "820")[0].MatchedBy)
}

func TestSameDayBeatsNearDate(t *testing.T) {
	requireDB(t)
	same := mkWork(t, "Same Day Host")
	near := mkWork(t, "Near Day Host")
	insertEGBrand(t, 31, "DayBrand", "")
	insertEGGame(t, 81, "Day Game", "2001-06-01", 31)
	insertEGGame(t, 82, "Day Game", "2001-06-10", 31)
	mapEGWork(t, same, 81)
	mapEGWork(t, near, 82)
	insertFetched(t, "821", "Series Day Game", "DayBrand", "2001/06/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrand)
	assert.Zero(t, st.EGBrandNear)
	assert.Equal(t, same, attachedRelease(t, "821").WorkID)
}

func TestSameBrandEditionBeyondAMonthNeverMints(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Edition Host")
	seedMintBrand(t, 32, "EditionBrand")
	insertEGGame(t, 83, "Edition Host Game", "2001-01-01", 32)
	mapEGWork(t, w, 83)
	insertItem(t, item{
		GetchuID: "822", Title: "Edition Host Game 特装版", Brand: "EditionBrand", BrandID: 32,
		ReleaseDate: "2001/06/01", Adult: true,
	})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGEditions)
	assert.Zero(t, st.MintGroups)
	assert.Zero(t, st.Attached)
	assert.Equal(t, before, workCount(t))
	assert.Empty(t, getchuRefs(t, "822"))
}

func TestQuarantineCandidatesPreferTheWholeTitle(t *testing.T) {
	requireDB(t)
	var series []int64
	for i := 0; i < 4; i++ {
		w := mkWork(t, "Alpha")
		mkReleaseYMD(t, w, i16(1999), i16(1), i16(int16(i+1)))
		series = append(series, w)
	}
	whole := mkWork(t, "Alpha Beta Gamma")
	mkReleaseYMD(t, whole, i16(1999), i16(6), i16(1))
	seedMintBrand(t, 33, "AlphaBrand")
	insertItem(t, item{
		GetchuID: "823", Title: "Alpha Beta Gamma", Brand: "AlphaBrand", BrandID: 33,
		ReleaseDate: "2001/06/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedQuarantined)
	assert.Equal(t, maxCandidates, st.Candidates)
	m := mintedWork(t, "823")
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate
		WHERE entity_type = ? AND ((a_id = ? AND b_id = ?) OR (a_id = ? AND b_id = ?))`,
		model.EntityTypeWork, m.ID, whole, whole, m.ID).Scan(&n).Error)
	assert.Equal(t, int64(1), n, "the work sharing the whole title must be among the candidates")
}

func TestBrandlessItemsNeverFold(t *testing.T) {
	a := item{GetchuID: "1", ReleaseDate: "2001/01/01"}
	b := item{GetchuID: "2", ReleaseDate: "2001/01/01"}
	assert.False(t, foldableItems(a, b))
	a.BrandID, b.BrandID = 5, 5
	assert.True(t, foldableItems(a, b))
}

func TestEGBrandOtherBrandDoesNotAttach(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Other Brand Host")
	insertEGBrand(t, 34, "RightBrand", "")
	insertEGGame(t, 84, "Other Brand Game", "2001-06-01", 34)
	mapEGWork(t, w, 84)
	insertFetched(t, "824", "Series Other Brand Game", "WrongBrand", "2001/06/01")

	st := runLane(t, true, "")
	assert.Zero(t, st.EGBrand)
	assert.Zero(t, st.Attached)
	assert.Empty(t, getchuRefs(t, "824"))
}

func TestDownloadMediaMintsADigitalRelease(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 35, "DLBrand")
	insertItem(t, item{
		GetchuID: "825", Title: "Download Only Getchu Game", Brand: "DLBrand", BrandID: 35,
		ReleaseDate: "2010/07/20", Adult: true, Media: "ダウンロード作品",
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Equal(t, model.ReleaseKindDigital, attachedRelease(t, "825").Kind)
}

func TestUnrelatedSameBrandGameNeitherAttachesNorBlocksAMint(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Completely Different Host")
	seedMintBrand(t, 36, "BusyBrand")
	insertEGGame(t, 86, "Completely Different", "2001-06-01", 36)
	mapEGWork(t, w, 86)
	insertItem(t, item{
		GetchuID: "826", Title: "Brand New Busy Title", Brand: "BusyBrand", BrandID: 36,
		ReleaseDate: "2001/06/01", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.EGEditions)
	assert.Equal(t, 1, st.MintedLive)
	assert.NotEqual(t, w, attachedRelease(t, "826").WorkID)
}

func TestAnyBrandEGEditionNeverMints(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Roshutsu Host")
	insertEGBrand(t, 37, "モニスタラッシュ", "")
	insertEGGame(t, 87, "露出快楽2 ～アソコの奥まで見せたがる恋人な同級生、立花美月。", "2010-07-30", 37)
	mapEGWork(t, w, 87)
	seedMintBrand(t, 38, "じぃすぽっと")
	insertItem(t, item{
		GetchuID: "827", Title: "今日のおかず 露出快楽2～アソコの奥まで見せたがる恋人な同級生、立花美月。",
		Brand: "じぃすぽっと", BrandID: 38, ReleaseDate: "2010/08/06", Adult: true,
	})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGEditions)
	assert.Zero(t, st.MintGroups)
	assert.Equal(t, before, workCount(t))
}

func TestCensoredTitleAttachesAcrossSpacingAndCase(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Breeder Host")
	insertEGBrand(t, 39, "CHAOS-R", "")
	insertEGGame(t, 88, "New Breeder 〜エッチな獣娘を狩って自分好みに調教！〜", "2024-12-20", 39)
	mapEGWork(t, w, 88)
	insertFetched(t, "828", "NEW BREEDER～エッチな獣娘を狩って自分好みに○○！～ 限定版 ピンナップガールズスキットル付き", "CHAOS-R", "2024/12/20")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.EGBrand)
	assert.Equal(t, w, attachedRelease(t, "828").WorkID)
}

func TestBrandWithoutAnAdultEGTitleNeverMints(t *testing.T) {
	requireDB(t)
	insertEGBrand(t, 40, "General Publisher", "")
	insertEGGame(t, 89, "Some Console Title", "2009-01-01", 40)
	insertItem(t, item{
		GetchuID: "829", Title: "Portable Handheld Pearl", Brand: "General Publisher", BrandID: 40,
		ReleaseDate: "2002/04/12", Adult: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.BrandUnknown)
	assert.Zero(t, st.MintGroups)
}

func TestActionOnlySubgenreNeverMints(t *testing.T) {
	requireDB(t)
	seedMintBrand(t, 41, "ShooterBrand")
	insertItem(t, item{
		GetchuID: "830", Title: "Zombie Survival Shooter", Brand: "ShooterBrand", BrandID: 41,
		ReleaseDate: "2009/05/22", Adult: true, Subgenre: "アクション [一覧]",
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.General)
	assert.Zero(t, st.MintGroups)
}
