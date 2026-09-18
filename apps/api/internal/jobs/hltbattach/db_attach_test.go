package hltbattach

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

func TestUniqueHitWorldDateAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Alpha Game Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 100, Name: "Alpha Game Title", World: "2001-01-01", Japan: "0000-00-00"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Population)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.Written)
	got := refsFor(t, w, 100)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	assert.Equal(t, ruleTitleDate, got[0].MatchedBy)
}

func TestUniqueHitJapanDateAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Japan Date Title")
	mkReleaseYMD(t, w, i16(2002), i16(2), i16(2))
	insertGame(t, game{ID: 101, Name: "Japan Date Title", World: "0000-00-00", Japan: "2002-02-02"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	got := refsFor(t, w, 101)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	assert.Equal(t, ruleTitleDate, got[0].MatchedBy)
}

func TestAliasHitAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "名探偵コナン からくり寺院殺人事件")
	mkReleaseYMD(t, w, i16(1998), i16(1), i16(1))
	insertGame(t, game{
		ID: 102, Name: "Detective Conan: The Mechanical Temple",
		Alias: "名探偵コナン からくり寺院殺人事件", World: "1998-01-01",
	})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	got := refsFor(t, w, 102)
	require.Len(t, got, 1)
	assert.Equal(t, ruleTitleDate, got[0].MatchedBy)
}

func TestUncorroboratedHitIsSkipped(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Uncorroborated Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 200, Name: "Uncorroborated Title", World: "2002-02-02"})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Uncorroborated)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.Written)
	assert.Empty(t, refsFor(t, w, 200))
	assert.Equal(t, before, workCount(t))
}

func TestTwoHitsAreSkipped(t *testing.T) {
	requireDB(t)
	w1 := mkWork(t, "Twin Date Title")
	w2 := mkWork(t, "Twin Date Title")
	mkReleaseYMD(t, w1, i16(2001), i16(1), i16(1))
	mkReleaseYMD(t, w2, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 201, Name: "Twin Date Title", World: "2001-01-01"})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MultiHit)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w1, 201))
	assert.Empty(t, refsFor(t, w2, 201))
	assert.Equal(t, before, workCount(t))
}

func TestNoHitNeverMints(t *testing.T) {
	requireDB(t)
	mkWork(t, "Unrelated Host Title")
	insertGame(t, game{ID: 202, Name: "Brand New HLTB Only Game", World: "2001-01-01"})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.NoHit)
	assert.Zero(t, st.Attached)
	assert.Zero(t, st.Written)
	assert.Equal(t, before, workCount(t))
	assert.Empty(t, allHLTBRefs(t))
}

func TestSearchHintIsNotCorpus(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Completely Different Host Title")
	mkWorkTitle(t, w, "Search Hint Only Title", model.WorkTitleKindSearchHint)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 500, Name: "Search Hint Only Title", World: "2001-01-01"})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.NoHit)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w, 500))
	assert.Equal(t, before, workCount(t))
}

func TestQuarantinedAndDeletedWorksAreNotCorpus(t *testing.T) {
	requireDB(t)
	q := mkWorkStatus(t, "Hidden Corpus Title", model.WorkStatusQuarantine)
	d := mkWork(t, "Hidden Corpus Title")
	mkReleaseYMD(t, q, i16(2001), i16(1), i16(1))
	mkReleaseYMD(t, d, i16(2001), i16(1), i16(1))
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, d).Error)
	insertGame(t, game{ID: 501, Name: "Hidden Corpus Title", World: "2001-01-01"})

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.NoHit)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, q, 501))
	assert.Empty(t, allHLTBRefs(t))
	assert.Equal(t, before, workCount(t))
}

func TestRejectionRemovesTheHit(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Rejected Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 300, Name: "Rejected Attach Title", World: "2001-01-01"})
	rejectHLTB(t, w, 300)

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.RejectedSkips)
	assert.Equal(t, 1, st.NoHit)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w, 300))
	assert.Equal(t, before, workCount(t))
}

func TestAnyExistingRefKeepsTheIdOutOfPopulation(t *testing.T) {
	requireDB(t)
	hltb := sourceID(t, "howlongtobeat")
	w := mkWork(t, "Would Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	gone := mkWork(t, "Retired Holder Title")
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: gone, SourceID: hltb,
		ExternalID: "600", LinkKind: model.LinkKindRelated, MatchedBy: "rule:test",
		DeadAt: deadNow(),
	}).Error)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, gone).Error)
	insertGame(t, game{ID: 600, Name: "Would Attach Title", World: "2001-01-01"})

	st := runLane(t, true, "")
	assert.Zero(t, st.Population)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w, 600))
}

func TestDeletedReleaseDateDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Deleted Date Title")
	mkReleaseYMD(t, w, i16(2003), i16(3), i16(3))
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET deleted_at = now() WHERE work_id = ?`, w).Error)
	insertGame(t, game{ID: 610, Name: "Deleted Date Title", World: "2003-03-03"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Uncorroborated)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w, 610))
}

func TestDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Dry Run Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 70, Name: "Dry Run Attach Title", World: "2001-01-01"})
	beforeWorks, beforeRefs := workCount(t), refCount(t)
	st := runLane(t, false, "")
	assert.Equal(t, 1, st.Attached)
	assert.Zero(t, st.Written)
	assert.Equal(t, beforeWorks, workCount(t))
	assert.Equal(t, beforeRefs, refCount(t))
	assert.Empty(t, refsFor(t, w, 70))
}

func TestSecondRunPlansNothing(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Second Run Game Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 71, Name: "Second Run Game Title", World: "2001-01-01"})
	first := runLane(t, true, "")
	assert.Equal(t, 1, first.Attached)
	assert.Equal(t, 1, first.Written)
	second := runLane(t, true, "")
	assert.Zero(t, second.Population)
	assert.Zero(t, second.Attached)
	assert.Zero(t, second.Written)
	assert.Len(t, refsFor(t, w, 71), 1)
	assert.Len(t, allHLTBRefs(t), 1)
}

func TestReceiptsMatchWrites(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Receipt Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 80, Name: "Receipt Attach Title", World: "2001-01-01"})
	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	st := runLane(t, true, path)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.Written)
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
	require.Len(t, recs, 1)
	assert.Equal(t, actionAttach, recs[0].Action)
	assert.Equal(t, int64(80), recs[0].HltbID)
	assert.Equal(t, w, recs[0].WorkID)
	assert.Equal(t, ruleTitleDate, recs[0].MatchedBy)
	require.Len(t, refsFor(t, recs[0].WorkID, 80), 1)
	assert.Equal(t, st.Written, len(allHLTBRefs(t)))
}
