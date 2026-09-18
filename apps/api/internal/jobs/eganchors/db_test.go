package eganchors

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTwoAgreeingFamiliesWriteExact(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "agree")
	mkRef(t, model.EntityTypeWork, w, vndb, "v10", model.LinkKindExact)
	insertGame(t, 100, "v10", "", "PC", "2001-01-01")
	addVNExtlink(t, "v10", "100")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Equal(t, 1, st.Written)
	assert.Zero(t, st.ProbablePlanned)
	got := refsFor(t, w, 100)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+egvndb", got[0].MatchedBy)
}

func TestOneFamilyWritesProbable(t *testing.T) {
	t.Run("vndb", func(t *testing.T) {
		requireDB(t)
		vndb := sourceID(t, "vndb")
		w := mkWork(t, "vndb-only")
		mkRef(t, model.EntityTypeWork, w, vndb, "v20", model.LinkKindExact)
		insertGame(t, 200, "", "", "PC", "2001-01-01")
		addVNExtlink(t, "v20", "200")

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.ProbablePlanned)
		assert.Zero(t, st.ExactPlanned)
		got := refsFor(t, w, 200)
		require.Len(t, got, 1)
		assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
		assert.Equal(t, "rule:eg-xlink:vndb", got[0].MatchedBy)
	})
	t.Run("dlsite", func(t *testing.T) {
		requireDB(t)
		dlsite := sourceID(t, "dlsite")
		w := mkWork(t, "dlsite-only")
		rel := mkRelease(t, w)
		mkRef(t, model.EntityTypeRelease, rel, dlsite, "RJ200", model.LinkKindExact)
		insertGame(t, 201, "", "RJ200", "PC", "2001-01-01")

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.ProbablePlanned)
		assert.Zero(t, st.ExactPlanned)
		got := refsFor(t, w, 201)
		require.Len(t, got, 1)
		assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
		assert.Equal(t, "rule:eg-xlink:dlsite", got[0].MatchedBy)
	})
}

func TestReleaseLevelVNDBLinkCounts(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "release-egs")
	mkRef(t, model.EntityTypeWork, w, vndb, "v30", model.LinkKindExact)
	insertGame(t, 300, "", "", "PC", "2001-01-01")
	addReleaseExtlink(t, "r30", "v30", "300")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ProbablePlanned)
	got := refsFor(t, w, 300)
	require.Len(t, got, 1)
	assert.Equal(t, "rule:eg-xlink:vndb", got[0].MatchedBy)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
}

func TestMultiVNGameWritesRelatedOnEach(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w1 := mkWork(t, "multi-a")
	w2 := mkWork(t, "multi-b")
	mkRef(t, model.EntityTypeWork, w1, vndb, "v41", model.LinkKindExact)
	mkRef(t, model.EntityTypeWork, w2, vndb, "v42", model.LinkKindExact)
	insertGame(t, 400, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "v41", "400")
	addVNExtlink(t, "v42", "400")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.MultiGames)
	assert.Equal(t, 2, st.RelatedPlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	for _, w := range []int64{w1, w2} {
		got := refsFor(t, w, 400)
		require.Len(t, got, 1)
		assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
		assert.Equal(t, "rule:eg-xlink-multi", got[0].MatchedBy)
	}
}

func TestTwoVNsOnOneWorkAreNotMulti(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "same-work-two-vn")
	mkRef(t, model.EntityTypeWork, w, vndb, "v51", model.LinkKindExact)
	mkRef(t, model.EntityTypeWork, w, vndb, "v52", model.LinkKindExact)
	insertGame(t, 500, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "v51", "500")
	addVNExtlink(t, "v52", "500")

	st := runLane(t, true, "")
	assert.Zero(t, st.MultiGames)
	assert.Equal(t, 1, st.CandidateGames)
	assert.Equal(t, 1, st.ProbablePlanned)
	got := refsFor(t, w, 500)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb", got[0].MatchedBy)
}

func TestWorkWithExactEGGetsRelated(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	w := mkWork(t, "already-exact")
	mkRef(t, model.EntityTypeWork, w, vndb, "v60", model.LinkKindExact)
	mkRef(t, model.EntityTypeWork, w, eg, "1", model.LinkKindExact)
	insertGame(t, 600, "v60", "", "PC", "2001-01-01")
	addVNExtlink(t, "v60", "600")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.RelatedPlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	got := refsFor(t, w, 600)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
	held := refsFor(t, w, 1)
	require.Len(t, held, 1)
	assert.Equal(t, model.LinkKindExact, held[0].LinkKind)
}

func TestDisagreeingFamiliesWriteTwinOnBoth(t *testing.T) {
	requireDB(t)
	vndb, dlsite := sourceID(t, "vndb"), sourceID(t, "dlsite")
	a := mkWork(t, "twin-a")
	b := mkWork(t, "twin-b")
	mkRef(t, model.EntityTypeWork, a, vndb, "v70", model.LinkKindExact)
	rel := mkRelease(t, b)
	mkRef(t, model.EntityTypeRelease, rel, dlsite, "RJ70", model.LinkKindExact)
	insertGame(t, 700, "v70", "RJ70", "PC", "2001-01-01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.TwinGames)
	assert.Equal(t, 2, st.RelatedPlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	gotA := refsFor(t, a, 700)
	gotB := refsFor(t, b, 700)
	require.Len(t, gotA, 1)
	require.Len(t, gotB, 1)
	assert.Equal(t, model.LinkKindRelated, gotA[0].LinkKind)
	assert.Equal(t, model.LinkKindRelated, gotB[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink-twin", gotA[0].MatchedBy)
	assert.Equal(t, "rule:eg-xlink-twin", gotB[0].MatchedBy)
}

func TestAnchoredGameGetsTwinOnTheOtherWork(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	x := mkWork(t, "already-held")
	w := mkWork(t, "vndb-named")
	mkRef(t, model.EntityTypeWork, x, eg, "800", model.LinkKindExact)
	mkRef(t, model.EntityTypeWork, w, vndb, "v80", model.LinkKindExact)
	insertGame(t, 800, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "v80", "800")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.AnchoredGames)
	assert.Equal(t, 1, st.TwinGames)
	assert.Equal(t, 1, st.RelatedPlanned)
	gotW := refsFor(t, w, 800)
	require.Len(t, gotW, 1)
	assert.Equal(t, model.LinkKindRelated, gotW[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink-twin", gotW[0].MatchedBy)
	gotX := refsFor(t, x, 800)
	require.Len(t, gotX, 1)
	assert.Equal(t, model.LinkKindExact, gotX[0].LinkKind)
	assert.Equal(t, "rule:test", gotX[0].MatchedBy)
}

func TestRejectionIsHonoured(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "rejected")
	mkRef(t, model.EntityTypeWork, w, vndb, "v90", model.LinkKindExact)
	insertGame(t, 900, "v90", "", "PC", "2001-01-01")
	addVNExtlink(t, "v90", "900")
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: w, SourceID: sourceID(t, "erogamescape"),
		ExternalID: "900", Reason: "not this work",
	}).Error)

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.RejectedSkips)
	assert.Equal(t, 1, st.CandidateGames)
	assert.Zero(t, st.ExactPlanned+st.ProbablePlanned+st.RelatedPlanned)
	assert.Empty(t, refsFor(t, w, 900))
}

func TestQuarantinedDeletedAndDeadTargetsIgnored(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")

	q := mkWorkStatus(t, "quarantine", model.WorkStatusQuarantine)
	mkRef(t, model.EntityTypeWork, q, vndb, "vQ", model.LinkKindExact)
	insertGame(t, 1001, "vQ", "", "PC", "2001-01-01")

	d := mkWork(t, "deleted")
	mkRef(t, model.EntityTypeWork, d, vndb, "vD", model.LinkKindExact)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, d).Error)
	insertGame(t, 1002, "vD", "", "PC", "2001-01-01")

	deadW := mkWork(t, "dead-vndb")
	mkDeadRef(t, model.EntityTypeWork, deadW, vndb, "vDead")
	insertGame(t, 1003, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vDead", "1003")

	probW := mkWork(t, "probable-vndb")
	mkRef(t, model.EntityTypeWork, probW, vndb, "vProb", model.LinkKindProbable)
	insertGame(t, 1004, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vProb", "1004")

	st := runLane(t, true, "")
	assert.Equal(t, 4, st.NoEvidenceGames)
	assert.Zero(t, st.ExactPlanned+st.ProbablePlanned+st.RelatedPlanned)
	assert.Zero(t, st.Written)
	assert.Empty(t, allEGRefs(t))
}

func TestDryRunWritesNothing(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "dry")
	mkRef(t, model.EntityTypeWork, w, vndb, "v110", model.LinkKindExact)
	insertGame(t, 1100, "v110", "", "PC", "2001-01-01")
	addVNExtlink(t, "v110", "1100")
	before := refCount(t)

	dry := runLane(t, false, "")
	assert.Equal(t, before, refCount(t))
	assert.Zero(t, dry.Written)

	apply := runLane(t, true, "")
	assert.Equal(t, dry.Games, apply.Games)
	assert.Equal(t, dry.CandidateGames, apply.CandidateGames)
	assert.Equal(t, dry.ExactPlanned, apply.ExactPlanned)
	assert.Equal(t, dry.ProbablePlanned, apply.ProbablePlanned)
	assert.Equal(t, dry.RelatedPlanned, apply.RelatedPlanned)
	assert.Equal(t, dry.NoEvidenceGames, apply.NoEvidenceGames)
	assert.Equal(t, 1, apply.Written)
}

func TestSecondRunPlansNothing(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "second")
	mkRef(t, model.EntityTypeWork, w, vndb, "v120", model.LinkKindExact)
	insertGame(t, 1200, "v120", "", "PC", "2001-01-01")
	addVNExtlink(t, "v120", "1200")

	first := runLane(t, true, "")
	assert.Equal(t, 1, first.Written)

	second := runLane(t, true, "")
	assert.Zero(t, second.ExactPlanned)
	assert.Zero(t, second.ProbablePlanned)
	assert.Zero(t, second.RelatedPlanned)
	assert.Zero(t, second.Written)

	dry := runLane(t, false, "")
	assert.Zero(t, dry.ExactPlanned)
	assert.Zero(t, dry.ProbablePlanned)
	assert.Zero(t, dry.RelatedPlanned)
	assert.Zero(t, dry.Written)
}

func TestReceiptsMatchWrites(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "receipts")
	mkRef(t, model.EntityTypeWork, w, vndb, "v130", model.LinkKindExact)
	insertGame(t, 1300, "v130", "", "PC", "2001-01-01")
	addVNExtlink(t, "v130", "1300")

	path := filepath.Join(t.TempDir(), "receipts.jsonl")
	st := runLane(t, true, path)
	assert.Equal(t, 1, st.Written)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var rec receipt
	require.NoError(t, json.Unmarshal(raw, &rec))
	got := allEGRefs(t)
	require.Len(t, got, 1)
	assert.Equal(t, rec.EgID, int64(1300))
	assert.Equal(t, rec.WorkID, w)
	assert.Equal(t, rec.LinkKind, got[0].LinkKind)
	assert.Equal(t, rec.MatchedBy, got[0].MatchedBy)
	assert.Equal(t, rec.Corroboration, "")
}

func TestSteamIsNotEvidence(t *testing.T) {
	requireDB(t)
	insertSteamGame(t, 1400, 424242)
	st := runLane(t, true, "")
	assert.Equal(t, 1, st.NoEvidenceGames)
	assert.Zero(t, st.ExactPlanned+st.ProbablePlanned+st.RelatedPlanned)
	assert.Empty(t, allEGRefs(t))
}
