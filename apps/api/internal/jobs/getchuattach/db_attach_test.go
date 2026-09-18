package getchuattach

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

func TestUniqueHitDatedAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Alpha Game Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertFetched(t, "100", "Alpha Game Title", "", "2001/01/01")

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Population)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.Written)
	assert.Equal(t, 1, st.TitleDate)
	assert.Equal(t, before, workCount(t), "an attach does not mint a work")
	rel := attachedRelease(t, "100")
	assert.Equal(t, w, rel.WorkID)
	assert.Equal(t, model.ReleaseKindPhysical, rel.Kind)
	assert.Nil(t, rel.Title)
	require.NotNil(t, rel.ReleasedY)
	require.NotNil(t, rel.ReleasedM)
	require.NotNil(t, rel.ReleasedD)
	assert.Equal(t, int16(2001), *rel.ReleasedY)
	assert.Equal(t, int16(1), *rel.ReleasedM)
	assert.Equal(t, int16(1), *rel.ReleasedD)
	refs := getchuRefs(t, "100")
	require.Len(t, refs, 1)
	assert.Equal(t, model.LinkKindExact, refs[0].LinkKind)
	assert.Equal(t, ruleTitleDate, refs[0].MatchedBy)
	var revs []model.CatalogRevision
	require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ?", model.EntityTypeRelease, rel.ID).Find(&revs).Error)
	require.Len(t, revs, 1)
	assert.Equal(t, model.RevisionActionImported, revs[0].Action)
}

func TestBangumiDateAloneDoesNotAttach(t *testing.T) {
	requireDB(t)
	bgm := sourceID(t, "bangumi")
	w := mkWork(t, "Bangumi Date Title")
	mkRef(t, model.EntityTypeWork, w, bgm, "9001", model.LinkKindExact)
	insertBgmSubject(t, 9001, "2002-02-02")
	insertFetched(t, "102", "Bangumi Date Title", "", "2002/02/02")

	st := runLane(t, true, "")
	assert.Zero(t, st.Attached)
	assert.Equal(t, 1, st.AllAges)
	assert.Empty(t, getchuRefs(t, "102"))
}

func TestBrandAloneDoesNotAttach(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Brand Game Title")
	lid := mkLabel(t, "フロントウイング")
	mkWorkLabel(t, w, lid)
	insertFetched(t, "101", "Brand Game Title", "フロントウイング／ブシロード", "2001/01/01")

	st := runLane(t, true, "")
	assert.Zero(t, st.Attached)
	assert.Equal(t, 1, st.AllAges)
	assert.Empty(t, getchuRefs(t, "101"))
}

func TestUncorroboratedHitIsSkipped(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Uncorroborated Title")
	insertFetched(t, "200", "Uncorroborated Title", "", "2001/01/01")

	beforeWorks, beforeRels := workCount(t), releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.Written)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Equal(t, beforeRels, releaseCount(t))
	assert.Empty(t, getchuRefs(t, "200"))
	assert.Empty(t, getchuRefsForWork(t, w))
}

func TestTwoHitsAreSkipped(t *testing.T) {
	requireDB(t)
	w1 := mkWork(t, "Twin Hit Title")
	w2 := mkWork(t, "Twin Hit Title")
	mkReleaseYMD(t, w1, i16(2001), i16(1), i16(1))
	insertFetched(t, "201", "Twin Hit Title", "", "2001/01/01")

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.Attached)
	assert.Equal(t, before, workCount(t))
	assert.Empty(t, getchuRefs(t, "201"))
	assert.Empty(t, getchuRefsForWork(t, w1))
	assert.Empty(t, getchuRefsForWork(t, w2))
}

func TestNoHitNeverMints(t *testing.T) {
	requireDB(t)
	mkWork(t, "Some Other Catalog Title")
	insertFetched(t, "202", "Completely Unknown Getchu Product", "", "2001/01/01")

	beforeWorks, beforeRels := workCount(t), releaseCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.Attached)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Equal(t, beforeRels, releaseCount(t))
	assert.Empty(t, getchuRefs(t, "202"))
}

func TestSearchHintIsNotCorpus(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Completely Different Host Title")
	mkWorkTitle(t, w, "Search Hint Only Title", model.WorkTitleKindSearchHint)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertFetched(t, "500", "Search Hint Only Title", "", "2001/01/01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.Attached)
	assert.Empty(t, getchuRefs(t, "500"))
	assert.Empty(t, getchuRefsForWork(t, w))
}

func TestQuarantinedAndDeletedWorksAreNotCorpus(t *testing.T) {
	requireDB(t)
	q := mkWorkStatus(t, "Hidden Corpus Title", model.WorkStatusQuarantine)
	mkReleaseYMD(t, q, i16(2001), i16(1), i16(1))
	d := mkWork(t, "Hidden Corpus Title")
	mkReleaseYMD(t, d, i16(2001), i16(1), i16(1))
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, d).Error)
	insertFetched(t, "501", "Hidden Corpus Title", "", "2001/01/01")

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AllAges)
	assert.Zero(t, st.Attached)
	assert.Equal(t, before, workCount(t))
	assert.Empty(t, getchuRefs(t, "501"))
	assert.Empty(t, getchuRefsForWork(t, q))
}

func TestRejectionRemovesTheHit(t *testing.T) {
	requireDB(t)
	t.Run("work", func(t *testing.T) {
		requireDB(t)
		w := mkWork(t, "Rejected Attach Title")
		mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		insertFetched(t, "300", "Rejected Attach Title", "", "2001/01/01")
		rejectGetchu(t, model.EntityTypeWork, w, "300")

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Equal(t, 1, st.AllAges)
		assert.Zero(t, st.Attached)
		assert.Empty(t, getchuRefs(t, "300"))
		assert.Empty(t, getchuRefsForWork(t, w))
	})
	t.Run("release", func(t *testing.T) {
		requireDB(t)
		w := mkWork(t, "Rejected Release Title")
		relID := mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		insertFetched(t, "301", "Rejected Release Title", "", "2001/01/01")
		rejectGetchu(t, model.EntityTypeRelease, relID, "301")

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Equal(t, 1, st.AllAges)
		assert.Zero(t, st.Attached)
		assert.Empty(t, getchuRefs(t, "301"))
		assert.Empty(t, getchuRefsForWork(t, w))
	})
}

func TestAnchoredIdOnDeletedReleaseIsNotPopulation(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Retired Release Title")
	relID := mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	mkRef(t, model.EntityTypeRelease, relID, sourceID(t, "getchu"), "600", model.LinkKindExact)
	require.NoError(t, testDB.Delete(&model.CatalogRelease{}, relID).Error)
	insertFetched(t, "600", "Retired Release Title", "", "2001/01/01")

	beforeRefs, beforeRels := refCount(t), releaseCount(t)
	st := runLane(t, true, "")
	assert.Zero(t, st.Population)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.Written)
	assert.Equal(t, beforeRefs, refCount(t))
	assert.Equal(t, beforeRels, releaseCount(t))
}

func TestPlaceholderDateLeavesReleaseUndated(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Unannounced Future Title")
	mkReleaseYMD(t, w, i16(2099), i16(12), i16(31))
	insertFetched(t, "98", "Unannounced Future Title", "フロントウイング", "2099/12/31")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	rel := attachedRelease(t, "98")
	assert.Equal(t, w, rel.WorkID)
	assert.Nil(t, rel.ReleasedY)
	assert.Nil(t, rel.ReleasedM)
	assert.Nil(t, rel.ReleasedD)
	assert.Equal(t, ruleTitleDate, getchuRefs(t, "98")[0].MatchedBy)
}

func TestDeletedReleaseDateDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Deleted Date Title")
	mkReleaseYMD(t, w, i16(2003), i16(3), i16(3))
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET deleted_at = now() WHERE work_id = ?`, w).Error)
	insertFetched(t, "610", "Deleted Date Title", "", "2003/03/03")

	st := runLane(t, true, "")
	assert.Zero(t, st.Attached)
	assert.Equal(t, 1, st.AllAges)
	assert.Empty(t, getchuRefs(t, "610"))
	assert.Empty(t, getchuRefsForWork(t, w))
}

func TestDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Dry Run Only Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertFetched(t, "70", "Dry Run Only Title", "", "2001/01/01")

	beforeWorks, beforeRefs, beforeRels := workCount(t), refCount(t), releaseCount(t)
	st := runLane(t, false, "")
	assert.Equal(t, 1, st.Attached)
	assert.Zero(t, st.Written)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Equal(t, beforeRefs, refCount(t))
	assert.Equal(t, beforeRels, releaseCount(t))
}

func TestSecondRunPlansNothing(t *testing.T) {
	requireDB(t)
	t.Run("attach", func(t *testing.T) {
		requireDB(t)
		w := mkWork(t, "Second Run Title")
		mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		insertFetched(t, "71", "Second Run Title", "", "2001/01/01")

		first := runLane(t, true, "")
		assert.Equal(t, 1, first.Attached)
		assert.Equal(t, 1, first.Written)
		second := runLane(t, true, "")
		assert.Zero(t, second.Population)
		assert.Zero(t, second.Attached)
		assert.Zero(t, second.Written)
		assert.Len(t, getchuRefs(t, "71"), 1)
	})
	t.Run("mint", func(t *testing.T) {
		requireDB(t)
		seedMintBrand(t, 13, "SecondMint")
		insertItem(t, item{
			GetchuID: "907", Title: "Second Run Mint Unique", Brand: "SecondMint", BrandID: 13,
			ReleaseDate: "2001/01/01", Adult: true,
		})
		first := runLane(t, true, "")
		assert.Equal(t, 1, first.MintedLive)
		second := runLane(t, true, "")
		assert.Zero(t, second.Population)
		assert.Zero(t, second.MintedLive)
		assert.Len(t, getchuRefs(t, "907"), 1)
	})
}

func TestReceiptsMatchWrites(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Receipt Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertFetched(t, "80", "Receipt Attach Title", "", "2001/01/01")
	seedMintBrand(t, 14, "ReceiptMint")
	insertItem(t, item{
		GetchuID: "81", Title: "Receipt Mint Unique Game", Brand: "ReceiptMint", BrandID: 14,
		ReleaseDate: "2001/01/01", Adult: true,
	})
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
	byID := map[string]receipt{}
	for _, r := range recs {
		if r.GetchuID != "" {
			byID[r.GetchuID] = r
		}
		for _, id := range r.GetchuIDs {
			byID[id] = r
		}
	}
	attach := byID["80"]
	assert.Equal(t, actionAttach, attach.Action)
	assert.Equal(t, "80", attach.GetchuID)
	assert.Equal(t, w, attach.WorkID)
	assert.Equal(t, ruleTitleDate, attach.MatchedBy)
	assert.Equal(t, attach.WorkID, attachedRelease(t, "80").WorkID)
	mint := byID["81"]
	assert.Equal(t, actionMint, mint.Action)
	assert.Equal(t, "live", mint.Status)
	assert.Contains(t, mint.GetchuIDs, "81")
	assert.Equal(t, ruleWorkImport, mint.MatchedBy)
	assert.Len(t, getchuRefs(t, "81"), 1)
}

func getchuRefsForWork(t *testing.T, workID int64) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Raw(`
		SELECT r.* FROM catalog_external_ref r
		JOIN catalog_release rel ON rel.id = r.entity_id
		WHERE r.entity_type = ? AND r.source_id = ? AND rel.work_id = ?`,
		model.EntityTypeRelease, sourceID(t, "getchu"), workID,
	).Scan(&out).Error)
	return out
}
