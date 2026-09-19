package getchuattach

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJanVNDBAnchorsTheExistingRelease(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "JAN VNDB Host")
	relID := mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertVNDBRelease(t, "r9001", 4901234567894)
	mkRef(t, model.EntityTypeRelease, relID, sourceID(t, "vndb"), "r9001", model.LinkKindExact)
	insertItem(t, item{GetchuID: "800", Title: "Unrelated JAN Title", ReleaseDate: "2001/01/01", JAN: 4901234567894})

	beforeRels := releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.JanVNDB)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, beforeRels, releaseCount(t), "JAN→VNDB anchors the existing release")
	refs := getchuRefs(t, "800")
	require.Len(t, refs, 1)
	assert.Equal(t, relID, refs[0].EntityID)
	assert.Equal(t, ruleJanVNDB, refs[0].MatchedBy)
	assert.Equal(t, model.LinkKindExact, refs[0].LinkKind)
	var revs int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_revision WHERE entity_id = ?`, relID).Scan(&revs).Error)
	assert.Zero(t, revs, "a ref-only anchor writes no revision")
}

func TestJanEGAttachesANewRelease(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "JAN EG Host")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertEGGame(t, 40, "JAN EG Host", "2001-01-01", 1)
	mapEGWork(t, w, 40)
	insertEGItemJAN(t, 9, "4901111111111")
	insertEGItemGame(t, 9, 40)
	insertItem(t, item{GetchuID: "801", Title: "Different JAN EG Title", ReleaseDate: "2001/01/01", JAN: 4901111111111})

	beforeRels := releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.JanEG)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, beforeRels+1, releaseCount(t))
	rel := attachedRelease(t, "801")
	assert.Equal(t, w, rel.WorkID)
	assert.Equal(t, ruleJanEG, getchuRefs(t, "801")[0].MatchedBy)
}

func TestJanConflictNeverMints(t *testing.T) {
	requireDB(t)
	vndbW := mkWork(t, "JAN Conflict VNDB")
	egW := mkWork(t, "JAN Conflict EG")
	relID := mkReleaseYMD(t, vndbW, i16(2001), i16(1), i16(1))
	insertVNDBRelease(t, "r9002", 4902222222222)
	mkRef(t, model.EntityTypeRelease, relID, sourceID(t, "vndb"), "r9002", model.LinkKindExact)
	insertEGGame(t, 41, "JAN Conflict EG", "2001-01-01", 1)
	mapEGWork(t, egW, 41)
	insertEGItemJAN(t, 10, "4902222222222")
	insertEGItemGame(t, 10, 41)
	seedMintBrand(t, 1, "ConflictBrand")
	insertItem(t, item{
		GetchuID: "802", Title: "Conflict Never Minted Unique", Brand: "ConflictBrand", BrandID: 1,
		ReleaseDate: "2001/01/01", JAN: 4902222222222, Adult: true,
	})

	beforeWorks := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.JanConflict)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Empty(t, getchuRefs(t, "802"))
}

func TestJanRejectedWorkFallsThrough(t *testing.T) {
	requireDB(t)
	janW := mkWork(t, "JAN Rejected Host")
	titleW := mkWork(t, "Jan Fallthrough Title")
	relID := mkReleaseYMD(t, janW, i16(2001), i16(1), i16(1))
	mkReleaseYMD(t, titleW, i16(2001), i16(1), i16(1))
	insertVNDBRelease(t, "r9003", 4903333333333)
	mkRef(t, model.EntityTypeRelease, relID, sourceID(t, "vndb"), "r9003", model.LinkKindExact)
	insertItem(t, item{GetchuID: "803", Title: "Jan Fallthrough Title", ReleaseDate: "2001/01/01", JAN: 4903333333333})
	rejectGetchu(t, model.EntityTypeWork, janW, "803")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.RejectedSkips)
	assert.Equal(t, 1, st.TitleDate)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, titleW, attachedRelease(t, "803").WorkID)
	assert.Empty(t, getchuRefsForWork(t, janW))
}
