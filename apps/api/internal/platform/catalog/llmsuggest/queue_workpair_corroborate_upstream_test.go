package llmsuggest

import (
	"fmt"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/srcvndb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func setupUpstreamDB(t *testing.T) (*gorm.DB, int16) {
	t.Helper()
	db, medium := setupWorkPairDB(t)
	require.NoError(t, db.Exec(`TRUNCATE src_vndb.releases_vn, src_vndb.releases_extlinks, src_vndb.extlinks, catalog_work_relation`).Error)
	return db, medium
}

var nextExtlinkID = 1

// vndbRelease attaches VNDB release rid (of the given VNs) to work and has it
// list the given store links.
func vndbRelease(t *testing.T, db *gorm.DB, work int64, rid string, vns []string, links map[string]string) {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: work, Kind: model.ReleaseKindDefault}
	require.NoError(t, db.Create(rel).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: model.SourceVNDB,
		ExternalID: rid, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
	for _, v := range vns {
		require.NoError(t, db.Create(&srcvndb.ReleaseVN{ID: rid, VID: v, RType: "complete"}).Error)
	}
	for site, value := range links {
		nextExtlinkID++
		require.NoError(t, db.Create(&srcvndb.Extlink{ID: nextExtlinkID, Site: site, Value: value}).Error)
		require.NoError(t, db.Create(&srcvndb.ReleaseExtlink{ID: rid, Link: nextExtlinkID}).Error)
	}
}

func storeReleaseRef(t *testing.T, db *gorm.DB, work int64, source int16, ext string) {
	t.Helper()
	rel := &model.CatalogRelease{WorkID: work, Kind: model.ReleaseKindDefault}
	require.NoError(t, db.Create(rel).Error)
	require.NoError(t, db.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: source,
		ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
}

// 458 and 212193: VNDB r104272 on 458 lists DLsite VJ009575, the id the
// Android port 212193 was minted from.
func TestUpstreamDeclaredStoreID(t *testing.T) {
	db, medium := setupUpstreamDB(t)
	orig := createLiveWork(t, db, medium, "妹ぱらだいす！")
	port := createLiveWork(t, db, medium, "【Android版】妹ぱらだいす！ ポータブル")
	vndbRelease(t, db, orig, "r104272", []string{"v5295"}, map[string]string{"dlsite": "VJ009575"})
	storeReleaseRef(t, db, port, model.SourceDLsite, "VJ009575")

	dmmOrig := createLiveWork(t, db, medium, "dmm declarer")
	dmmPort := createLiveWork(t, db, medium, "dmm holder")
	vndbRelease(t, db, dmmOrig, "r2", []string{"v2"}, map[string]string{"dmm": "dlsoft.dmm.co.jp/detail/ggs_0868/"})
	storeReleaseRef(t, db, dmmPort, model.SourceDMM, "ggs_0868")

	idx, err := LoadUpstreamIndex(db)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("vndb r104272 on %d lists dlsite:vj009575 held by %d", orig, port), idx.Declared(port, orig))
	assert.NotEmpty(t, idx.Declared(dmmOrig, dmmPort), "dmm url folds to its cid")
}

func TestUpstreamDeclaredNeedsOneVNAndOneOtherHolder(t *testing.T) {
	db, medium := setupUpstreamDB(t)
	pack := createLiveWork(t, db, medium, "pack declarer")
	packHolder := createLiveWork(t, db, medium, "pack holder")
	vndbRelease(t, db, pack, "r10", []string{"v10", "v11"}, map[string]string{"dlsite": "VJ000010"})
	storeReleaseRef(t, db, packHolder, model.SourceDLsite, "VJ000010")

	decl := createLiveWork(t, db, medium, "two holders declarer")
	h1 := createLiveWork(t, db, medium, "two holders one")
	h2 := createLiveWork(t, db, medium, "two holders two")
	vndbRelease(t, db, decl, "r20", []string{"v20"}, map[string]string{"dlsite": "VJ000020"})
	storeReleaseRef(t, db, h1, model.SourceDLsite, "VJ000020")
	exactWorkRef(t, db, h2, model.SourceDLsite, "VJ000020")

	self := createLiveWork(t, db, medium, "self holder")
	vndbRelease(t, db, self, "r30", []string{"v30"}, map[string]string{"dlsite": "VJ000030"})
	storeReleaseRef(t, db, self, model.SourceDLsite, "VJ000030")

	idx, err := LoadUpstreamIndex(db)
	require.NoError(t, err)
	assert.Empty(t, idx.Declared(pack, packHolder), "a release of two VNs is a pack")
	assert.Empty(t, idx.Declared(decl, h1), "two other holders")
	assert.Empty(t, idx.DeclaredPairs())
}

func TestUpstreamPortFromEGRelation(t *testing.T) {
	db, medium := setupUpstreamDB(t)
	a := createLiveWork(t, db, medium, "pc original")
	b := createLiveWork(t, db, medium, "android port")
	src := model.SourceErogameScape
	require.NoError(t, db.Create(&model.CatalogWorkRelation{
		AWorkID: a, BWorkID: b, RelationTypeID: relationTypeAlternativeVersion, SourceID: &src,
	}).Error)
	vndb := model.SourceVNDB
	c := createLiveWork(t, db, medium, "vndb alt")
	require.NoError(t, db.Create(&model.CatalogWorkRelation{
		AWorkID: a, BWorkID: c, RelationTypeID: relationTypeAlternativeVersion, SourceID: &vndb,
	}).Error)

	idx, err := LoadUpstreamIndex(db)
	require.NoError(t, err)
	assert.True(t, idx.Port(b, a))
	assert.False(t, idx.Port(a, c), "a VNDB alternative version is a separate VN")
}

func TestUpstreamFactNeedsSameButNotAgreeingYears(t *testing.T) {
	s := workPairSides{AID: 458, BID: 212193}
	for _, ev := range []pairEvidence{
		{UpstreamDeclared: "vndb r104272 on 458 lists dlsite:vj009575 held by 212193", YearsClash: true},
		{UpstreamPort: true, YearsClash: true},
	} {
		assert.Equal(t, applyAccept, planWorkPair(VerdictSame, 0.95, 0.9, 0.7, s, ev).Action, "%+v", ev)
		assert.Equal(t, stampKeptApartUncorroborated, planWorkPair(VerdictUnsure, 0.6, 0.9, 0.7, s, ev).stamp(), "%+v", ev)
		assert.NotEqual(t, applyAccept, planWorkPair(VerdictSame, 0.8, 0.9, 0.7, s, ev).Action, "below the bar: %+v", ev)
	}
}

func TestUpstreamPortAcceptsThroughApply(t *testing.T) {
	db, medium := setupUpstreamDB(t)
	a := createLiveWork(t, db, medium, "妹ぱらだいす！ Hアニメ増量版")
	b := createLiveWork(t, db, medium, "【Android版】妹ぱらだいす！ ポータブル")
	src := model.SourceErogameScape
	require.NoError(t, db.Create(&model.CatalogWorkRelation{
		AWorkID: a, BWorkID: b, RelationTypeID: relationTypeAlternativeVersion, SourceID: &src,
	}).Error)
	fileWorkPair(t, db, a, b, VerdictSame, "hash-port")

	st, err := RunApply(t.Context(), db, StagingDBs{}, testQueueService(db), Options{
		Queue: QueueWorkPair, Actor: 1, MinConfidence: 0.9, MinConfidenceReject: 0.7,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Counts["applied_"+applyAccept], "counts: %v", st.Counts)
}
