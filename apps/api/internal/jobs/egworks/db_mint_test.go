package egworks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoHitMintsLiveWithTitlesReleaseRefPlatformAndLabel(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	lid := mkLabel(t, "Mint Brand")
	mkRef(t, model.EntityTypeLabel, lid, eg, "7", model.LinkKindExact)
	insertGame(t, game{
		ID: 99, Gamename: "Brand New EG Only Game", Furigana: "ブランドニュー",
		Sellday: "2001-06-15", Model: "PC", BrandID: 7, Erogame: true,
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Zero(t, st.Quarantined)
	got := egRefByGame(t, 99)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, ruleWorkImport, got[0].MatchedBy)
	wid := got[0].EntityID

	var w model.CatalogWork
	require.NoError(t, testDB.First(&w, wid).Error)
	assert.Equal(t, mediumID(t), w.MediumID)
	assert.Equal(t, model.OLangDefault, w.OLang)
	assert.Equal(t, "Brand New EG Only Game", w.DisplayName)
	assert.Equal(t, model.ContentRatingR18, w.ContentRating)
	assert.Equal(t, model.WorkStatusLive, w.Status)
	assert.JSONEq(t, `{}`, string(w.Extra))
	assert.JSONEq(t, `{}`, string(w.FieldProvenance))

	var titles []model.CatalogWorkTitle
	require.NoError(t, testDB.Where("work_id = ?", wid).Order("kind").Find(&titles).Error)
	require.Len(t, titles, 2)
	assert.Equal(t, "Brand New EG Only Game", titles[0].Title)
	assert.Equal(t, "ja", titles[0].Lang)
	assert.Equal(t, model.WorkTitleKindOfficial, titles[0].Kind)
	assert.Equal(t, "ブランドニュー", titles[1].Title)
	assert.Equal(t, model.WorkTitleKindSearchHint, titles[1].Kind)

	var rel model.CatalogRelease
	require.NoError(t, testDB.Where("work_id = ?", wid).First(&rel).Error)
	assert.Equal(t, model.ReleaseKindDefault, rel.Kind)
	require.NotNil(t, rel.ReleasedY)
	require.NotNil(t, rel.ReleasedM)
	require.NotNil(t, rel.ReleasedD)
	assert.Equal(t, int16(2001), *rel.ReleasedY)
	assert.Equal(t, int16(6), *rel.ReleasedM)
	assert.Equal(t, int16(15), *rel.ReleasedD)
	require.NotNil(t, rel.Platform)
	assert.Equal(t, "win", *rel.Platform)

	var plat model.CatalogWorkPlatform
	require.NoError(t, testDB.Where("work_id = ?", wid).First(&plat).Error)
	assert.Equal(t, "win", plat.Platform)
	assert.Equal(t, eg, plat.SourceID)

	var edge model.CatalogWorkLabel
	require.NoError(t, testDB.Where("work_id = ?", wid).First(&edge).Error)
	assert.Equal(t, lid, edge.LabelID)
	assert.Equal(t, model.WorkLabelKindDeveloper, edge.Kind)
	require.NotNil(t, edge.SourceID)
	assert.Equal(t, eg, *edge.SourceID)

	var revs []model.CatalogRevision
	require.NoError(t, testDB.Where("entity_id IN ?", []int64{wid, rel.ID}).Order("entity_type").Find(&revs).Error)
	require.Len(t, revs, 2)
	assert.Equal(t, model.RevisionActionImported, revs[0].Action)
	assert.Equal(t, model.RevisionActionImported, revs[1].Action)
}

func TestPlaceholderSelldayLeavesDateEmpty(t *testing.T) {
	requireDB(t)
	insertGame(t, game{ID: 98, Gamename: "Unannounced Future Game", Sellday: "2099-12-31", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	got := egRefByGame(t, 98)
	require.Len(t, got, 1)
	var rel model.CatalogRelease
	require.NoError(t, testDB.Where("work_id = ?", got[0].EntityID).First(&rel).Error)
	assert.Nil(t, rel.ReleasedY)
	assert.Nil(t, rel.ReleasedM)
	assert.Nil(t, rel.ReleasedD)
}

func TestIntraBatchMintsOnePrimaryAndFoldsTheRest(t *testing.T) {
	requireDB(t)
	insertGame(t, game{ID: 5, Gamename: "Same Title Games", Model: "PC", Sellday: "2001-01-01"})
	insertGame(t, game{ID: 6, Gamename: "Same Title Games", Model: "NS", Sellday: "2001-01-01"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Equal(t, 1, st.EditionFolded)
	assert.Equal(t, int64(1), workCount(t))
	primary := egRefByGame(t, 5)
	folded := egRefByGame(t, 6)
	require.Len(t, primary, 1)
	require.Len(t, folded, 1)
	assert.Equal(t, primary[0].EntityID, folded[0].EntityID)
	assert.Equal(t, model.LinkKindExact, primary[0].LinkKind)
	assert.Equal(t, ruleWorkImport, primary[0].MatchedBy)
	assert.Equal(t, model.LinkKindRelated, folded[0].LinkKind)
	assert.Equal(t, ruleEdition, folded[0].MatchedBy)
}

func TestGroupQuarantinedIfAnyMemberHits(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "スマホ版スライムバスターリミテッド")
	insertGame(t, game{ID: 1, Gamename: "【スマホ版】スライムバスター・リミテッド", Model: "PC", BrandID: 55})
	insertGame(t, game{ID: 2, Gamename: "スライムバスター・リミテッド", Model: "PC", BrandID: 55})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 1, st.EditionFolded)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, 1, st.CandidatesPlanned)
	assert.Equal(t, int64(2), workCount(t))
	p := egRefByGame(t, 1)
	f := egRefByGame(t, 2)
	require.Len(t, p, 1)
	require.Len(t, f, 1)
	assert.Equal(t, p[0].EntityID, f[0].EntityID)
	var work model.CatalogWork
	require.NoError(t, testDB.First(&work, p[0].EntityID).Error)
	assert.Equal(t, model.WorkStatusQuarantine, work.Status)
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate`).Scan(&n).Error)
	assert.Equal(t, int64(1), n)
	assert.NotEqual(t, w, p[0].EntityID)
}

func TestDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	insertGame(t, game{ID: 70, Gamename: "Dry Run Only Game", Model: "PC"})
	beforeWorks, beforeRefs := workCount(t), refCount(t)
	st := runLane(t, false, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Equal(t, 1, st.RefsPlanned)
	assert.Zero(t, st.Written)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Equal(t, beforeRefs, refCount(t))
}

func TestSecondRunPlansNothing(t *testing.T) {
	requireDB(t)
	insertGame(t, game{ID: 71, Gamename: "Second Run Game Title", Model: "PC"})
	first := runLane(t, true, "")
	assert.Equal(t, 1, first.MintedLive)
	assert.Equal(t, 1, first.Written)
	second := runLane(t, true, "")
	assert.Zero(t, second.Population)
	assert.Zero(t, second.RefsPlanned)
	assert.Zero(t, second.MintedLive)
	assert.Zero(t, second.Attached)
	assert.Equal(t, int64(1), workCount(t))
	assert.Len(t, allEGRefs(t), 1)
}

func TestReceiptsMatchWrites(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Receipt Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 80, Gamename: "Receipt Attach Title", Sellday: "2001-01-01", Model: "PC"})
	insertGame(t, game{ID: 81, Gamename: "Receipt Mint Unique Game", Model: "PC"})
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	st := runLane(t, true, path)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.MintedLive)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var recs []receipt
	for _, line := range bytes.Split(bytes.TrimSpace(raw), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var rec receipt
		require.NoError(t, json.Unmarshal(line, &rec))
		recs = append(recs, rec)
	}
	require.Len(t, recs, 2)
	byEg := map[int64]receipt{}
	for _, r := range recs {
		byEg[r.EgID] = r
	}
	attach := byEg[80]
	assert.Equal(t, actionAttach, attach.Action)
	assert.Equal(t, w, attach.WorkID)
	assert.Equal(t, ruleTitleDate, attach.MatchedBy)
	require.Len(t, refsFor(t, attach.WorkID, 80), 1)
	mint := byEg[81]
	assert.Equal(t, actionMint, mint.Action)
	assert.Equal(t, ruleWorkImport, mint.MatchedBy)
	got := egRefByGame(t, 81)
	require.Len(t, got, 1)
	assert.Equal(t, st.Written, len(allEGRefs(t)))
}

func TestSameTitleDifferentGamesAreNotFolded(t *testing.T) {
	requireDB(t)
	insertGame(t, game{ID: 80, Gamename: "CARAT", Model: "PC", Sellday: "1992-05-22", BrandID: 1871})
	insertGame(t, game{ID: 81, Gamename: "CARAT", Model: "PC", Sellday: "1998-07-10", BrandID: 391})

	st := runLane(t, true, "")
	assert.Equal(t, 2, st.MintedLive)
	assert.Zero(t, st.EditionFolded)
	assert.Equal(t, int64(2), workCount(t))
	a, b := egRefByGame(t, 80), egRefByGame(t, 81)
	require.Len(t, a, 1)
	require.Len(t, b, 1)
	assert.NotEqual(t, a[0].EntityID, b[0].EntityID)
	assert.Equal(t, model.LinkKindExact, b[0].LinkKind)
}
