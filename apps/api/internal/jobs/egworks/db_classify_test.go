package egworks

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPackRefsEveryHeldSubjectAndNeverMints(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	w1 := mkWork(t, "Subject One Game")
	w2 := mkWork(t, "Subject Two Game")
	mkRef(t, model.EntityTypeWork, w1, eg, "10", model.LinkKindExact)
	mkRef(t, model.EntityTypeWork, w2, eg, "11", model.LinkKindExact)
	insertGame(t, game{ID: 10, Gamename: "Subject One Game", Model: "PC"})
	insertGame(t, game{ID: 11, Gamename: "Subject Two Game", Model: "PC"})
	insertGame(t, game{ID: 20, Gamename: "Compilation Pack Title", Model: "PC"})
	insertBundling(t, 10, 20)
	insertBundling(t, 11, 20)

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.PackGames)
	assert.Equal(t, 1, st.Population)
	assert.Zero(t, st.MintedLive)
	assert.Zero(t, st.Quarantined)
	assert.Equal(t, before, workCount(t), "a pack must never mint")
	r1 := refsFor(t, w1, 20)
	r2 := refsFor(t, w2, 20)
	require.Len(t, r1, 1)
	require.Len(t, r2, 1)
	assert.Equal(t, model.LinkKindRelated, r1[0].LinkKind)
	assert.Equal(t, rulePack, r1[0].MatchedBy)
	assert.Equal(t, model.LinkKindRelated, r2[0].LinkKind)
	assert.Equal(t, rulePack, r2[0].MatchedBy)
}

func TestOneSubjectBundleIsNotAPack(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	w := mkWork(t, "Host Game Title")
	mkRef(t, model.EntityTypeWork, w, eg, "10", model.LinkKindExact)
	insertGame(t, game{ID: 10, Gamename: "Host Game Title", Model: "PC"})
	insertGame(t, game{ID: 20, Gamename: "Bonus Game Title Alone", Model: "PC"})
	insertBundling(t, 10, 20)

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Zero(t, st.PackGames)
	assert.Equal(t, 1, st.MintedLive)
	assert.Equal(t, before+1, workCount(t))
	assert.Empty(t, refsFor(t, w, 20))
}

func TestPortAttachesToTheOriginalsWork(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	w := mkWork(t, "Original PC Title")
	mkRef(t, model.EntityTypeWork, w, eg, "10", model.LinkKindExact)
	insertGame(t, game{ID: 10, Gamename: "Original PC Title", Model: "PC"})
	insertGame(t, game{ID: 30, Gamename: "Ported Console Title", Model: "PS2"})
	insertTransplant(t, 30, 10)

	before := workCount(t)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.PortGames)
	assert.Zero(t, st.MintedLive)
	assert.Equal(t, before, workCount(t))
	got := refsFor(t, w, 30)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
	assert.Equal(t, ruleTransplant, got[0].MatchedBy)
}

func TestUniqueCorroboratedByDateAttachesExact(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Alpha Game Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 100, Gamename: "Alpha Game Title", Sellday: "2001-01-01", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	assert.Zero(t, st.MintedLive)
	got := refsFor(t, w, 100)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, ruleTitleDate, got[0].MatchedBy)
}

func TestUniqueCorroboratedByBrandAttachesExact(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	w := mkWork(t, "Brand Game Title")
	lid := mkLabel(t, "Brand Seven")
	mkRef(t, model.EntityTypeLabel, lid, eg, "7", model.LinkKindExact)
	mkWorkLabel(t, w, lid)
	insertGame(t, game{ID: 101, Gamename: "Brand Game Title", BrandID: 7, Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	got := refsFor(t, w, 101)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, ruleTitleBrand, got[0].MatchedBy)
}

func TestUniqueCorroboratedByBangumiDateAttachesExact(t *testing.T) {
	requireDB(t)
	bgm := sourceID(t, "bangumi")
	w := mkWork(t, "Bangumi Date Title")
	mkRef(t, model.EntityTypeWork, w, bgm, "9001", model.LinkKindExact)
	insertBgmSubject(t, 9001, "2002-02-02")
	insertGame(t, game{ID: 102, Gamename: "Bangumi Date Title", Sellday: "2002-02-02", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	got := refsFor(t, w, 102)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, ruleTitleBgm, got[0].MatchedBy)
}

func TestAttachToWorkHoldingEGRefIsRelated(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	w := mkWork(t, "Shared Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	mkRef(t, model.EntityTypeWork, w, eg, "1", model.LinkKindExact)
	insertGame(t, game{ID: 1, Gamename: "Shared Attach Title", Sellday: "2001-01-01", Model: "PC"})
	insertGame(t, game{ID: 2, Gamename: "Shared Attach Title", Sellday: "2001-01-01", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Attached)
	got := refsFor(t, w, 2)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
	assert.Equal(t, ruleTitleDate, got[0].MatchedBy)
}

func TestUncorroboratedHitMintsQuarantinedWithCandidate(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Uncorroborated Title")
	insertGame(t, game{ID: 200, Gamename: "Uncorroborated Title", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 1, st.CandidatesPlanned)
	assert.Zero(t, st.MintedLive)
	assert.Zero(t, st.Attached)
	got := egRefByGame(t, 200)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, ruleWorkImport, got[0].MatchedBy)
	var work model.CatalogWork
	require.NoError(t, testDB.First(&work, got[0].EntityID).Error)
	assert.Equal(t, model.WorkStatusQuarantine, work.Status)
	var cands []model.CatalogMatchCandidate
	require.NoError(t, testDB.Find(&cands).Error)
	require.Len(t, cands, 1)
	assert.Equal(t, model.CandidateReasonNameNormEqual, cands[0].Reason)
	assert.Equal(t, model.CandidateStatusPending, cands[0].Status)
	a, b := cands[0].AID, cands[0].BID
	if a > b {
		a, b = b, a
	}
	ids := []int64{work.ID, w}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}
	assert.Equal(t, ids[0], a)
	assert.Equal(t, ids[1], b)
}

func TestTwoCorroboratedHitsQuarantine(t *testing.T) {
	requireDB(t)
	w1 := mkWork(t, "Twin Date Title")
	w2 := mkWork(t, "Twin Date Title")
	mkReleaseYMD(t, w1, i16(2001), i16(1), i16(1))
	mkReleaseYMD(t, w2, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 201, Gamename: "Twin Date Title", Sellday: "2001-01-01", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 2, st.CandidatesPlanned)
	assert.Zero(t, st.Attached)
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_match_candidate`).Scan(&n).Error)
	assert.Equal(t, int64(2), n)
}

func TestCandidatesCappedAtThree(t *testing.T) {
	requireDB(t)
	var ids []int64
	for i := 0; i < 4; i++ {
		ids = append(ids, mkWork(t, "Four Hit Title"))
	}
	insertGame(t, game{ID: 202, Gamename: "Four Hit Title", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.Quarantined)
	assert.Equal(t, 3, st.CandidatesPlanned)
	var cands []model.CatalogMatchCandidate
	require.NoError(t, testDB.Order("a_id, b_id").Find(&cands).Error)
	require.Len(t, cands, 3)
	got := egRefByGame(t, 202)
	require.Len(t, got, 1)
	newID := got[0].EntityID
	paired := map[int64]struct{}{}
	for _, c := range cands {
		other := c.BID
		if other == newID {
			other = c.AID
		}
		paired[other] = struct{}{}
	}
	assert.NotContains(t, paired, ids[3], "the highest work id is the one dropped")
	assert.Contains(t, paired, ids[0])
	assert.Contains(t, paired, ids[1])
	assert.Contains(t, paired, ids[2])
}

func TestRejectionHonouredOnEveryWrite(t *testing.T) {
	requireDB(t)
	t.Run("attach", func(t *testing.T) {
		requireDB(t)
		w := mkWork(t, "Rejected Attach Title")
		mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
		insertGame(t, game{ID: 300, Gamename: "Rejected Attach Title", Sellday: "2001-01-01", Model: "PC"})
		rejectEG(t, w, 300)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Zero(t, st.Attached)
		assert.Empty(t, refsFor(t, w, 300))
		assert.Equal(t, 1, st.MintedLive, "a rejected work is no longer a hit, so the game gets its own work")
		got := egRefByGame(t, 300)
		require.Len(t, got, 1)
		assert.NotEqual(t, w, got[0].EntityID)
	})
	t.Run("port", func(t *testing.T) {
		requireDB(t)
		eg := sourceID(t, "erogamescape")
		w := mkWork(t, "Original Game For Port")
		mkRef(t, model.EntityTypeWork, w, eg, "40", model.LinkKindExact)
		insertGame(t, game{ID: 40, Gamename: "Original Game For Port", Model: "PC"})
		insertGame(t, game{ID: 41, Gamename: "Rejected Port Title", Model: "PS2"})
		insertTransplant(t, 41, 40)
		rejectEG(t, w, 41)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Zero(t, st.PortGames)
		assert.Empty(t, refsFor(t, w, 41))
		assert.Equal(t, 1, st.MintedLive)
		require.Len(t, egRefByGame(t, 41), 1)
	})
	t.Run("pack", func(t *testing.T) {
		requireDB(t)
		eg := sourceID(t, "erogamescape")
		w1 := mkWork(t, "Pack Subject A")
		w2 := mkWork(t, "Pack Subject B")
		mkRef(t, model.EntityTypeWork, w1, eg, "10", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, w2, eg, "11", model.LinkKindExact)
		insertGame(t, game{ID: 10, Gamename: "Pack Subject A", Model: "PC"})
		insertGame(t, game{ID: 11, Gamename: "Pack Subject B", Model: "PC"})
		insertGame(t, game{ID: 20, Gamename: "Rejected Pack Object", Model: "PC"})
		insertBundling(t, 10, 20)
		insertBundling(t, 11, 20)
		rejectEG(t, w1, 20)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Equal(t, 1, st.PackGames)
		assert.Empty(t, refsFor(t, w1, 20))
		got := refsFor(t, w2, 20)
		require.Len(t, got, 1)
		assert.Equal(t, rulePack, got[0].MatchedBy)
	})
}

func TestLimitCapsMintsNotAttaches(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Limit Attach Title")
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, game{ID: 400, Gamename: "Limit Attach Title", Sellday: "2001-01-01", Model: "PC"})
	insertGame(t, game{ID: 401, Gamename: "Mint Alpha Unique Name", Model: "PC"})
	insertGame(t, game{ID: 402, Gamename: "Mint Bravo Unique Name", Model: "PC"})
	insertGame(t, game{ID: 403, Gamename: "Mint Charlie Unique Name", Model: "PC"})

	st := runLaneLimit(t, true, "", 1)
	assert.Equal(t, 1, st.Attached)
	assert.Equal(t, 1, st.MintedLive)
	assert.Equal(t, 2, st.Limited)
	require.Len(t, refsFor(t, w, 400), 1)
	assert.Len(t, egRefByGame(t, 401), 1)
	assert.Empty(t, egRefByGame(t, 402))
	assert.Empty(t, egRefByGame(t, 403))
}

func TestSearchHintsAreNotCorpus(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Completely Different Host Title")
	mkWorkTitle(t, w, "Search Hint Only Title", model.WorkTitleKindSearchHint)
	insertGame(t, game{ID: 500, Gamename: "Search Hint Only Title", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Zero(t, st.Quarantined)
	assert.Zero(t, st.Attached)
	assert.Empty(t, refsFor(t, w, 500))
}

func TestQuarantinedAndDeletedWorksAreNotCorpus(t *testing.T) {
	requireDB(t)
	q := mkWorkStatus(t, "Hidden Corpus Title", model.WorkStatusQuarantine)
	d := mkWork(t, "Hidden Corpus Title")
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, d).Error)
	insertGame(t, game{ID: 501, Gamename: "Hidden Corpus Title", Model: "PC"})

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MintedLive)
	assert.Zero(t, st.Quarantined)
	assert.Empty(t, refsFor(t, q, 501))
	got := egRefByGame(t, 501)
	require.Len(t, got, 1)
	var w model.CatalogWork
	require.NoError(t, testDB.First(&w, got[0].EntityID).Error)
	assert.Equal(t, model.WorkStatusLive, w.Status)
}

func TestHoldingsOnDeletedWorksDoNotCount(t *testing.T) {
	requireDB(t)
	eg := sourceID(t, "erogamescape")
	gone := mkWork(t, "Retired Holder One")
	mkRef(t, model.EntityTypeWork, gone, eg, "600", model.LinkKindExact)
	related := mkWork(t, "Retired Holder Two")
	mkRef(t, model.EntityTypeWork, related, eg, "601", model.LinkKindRelated)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, []int64{gone, related}).Error)
	insertGame(t, game{ID: 600, Gamename: "Retired Exact Game", Model: "PC"})
	insertGame(t, game{ID: 601, Gamename: "Related On Deleted Game", Model: "PC"})

	st := runLane(t, true, "")
	assert.Zero(t, st.Errors)
	assert.Equal(t, 1, st.Population, "an exact ref on a deleted work still owns its id; a related one does not")
	assert.Equal(t, 1, st.MintedLive)
	assert.Len(t, egRefByGame(t, 600), 1)
	assert.Len(t, egRefByGame(t, 601), 2)
}

func TestDeletedReleaseDateDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "Deleted Date Title")
	mkReleaseYMD(t, w, i16(2003), i16(3), i16(3))
	require.NoError(t, testDB.Exec(`UPDATE catalog_release SET deleted_at = now() WHERE work_id = ?`, w).Error)
	insertGame(t, game{ID: 610, Gamename: "Deleted Date Title", Sellday: "2003-03-03", Model: "PC"})

	st := runLane(t, true, "")
	assert.Zero(t, st.Attached)
	assert.Equal(t, 1, st.Quarantined)
	assert.Empty(t, refsFor(t, w, 610))
}
