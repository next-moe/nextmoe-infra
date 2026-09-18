package eganchors

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbablePrimaryBlocksASecondPrimary(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "probable-block")
	mkRef(t, model.EntityTypeWork, w, vndb, "vPB", model.LinkKindExact)
	insertGame(t, 3100, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vPB", "3100")

	first := runLane(t, true, "")
	assert.Equal(t, 1, first.ProbablePlanned)
	assert.Zero(t, first.ExactPlanned)
	got := refsFor(t, w, 3100)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)

	insertGame(t, 3101, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vPB", "3101")
	path := t.TempDir() + "/receipts.jsonl"
	second := runLane(t, true, path)
	assert.Equal(t, 1, second.RelatedPlanned)
	assert.Zero(t, second.ExactPlanned)
	assert.Zero(t, second.ProbablePlanned)
	secondRef := refsFor(t, w, 3101)
	require.Len(t, secondRef, 1)
	assert.Equal(t, model.LinkKindRelated, secondRef[0].LinkKind)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var rec receipt
	require.NoError(t, json.Unmarshal(raw, &rec))
	assert.Equal(t, classEdition, rec.Class)
	assert.Equal(t, int64(3101), rec.EgID)
	held := refsFor(t, w, 3100)
	require.Len(t, held, 1)
	assert.Equal(t, model.LinkKindProbable, held[0].LinkKind)
}

func TestRejectionHonouredOnMultiAndTwin(t *testing.T) {
	t.Run("multi", func(t *testing.T) {
		requireDB(t)
		vndb := sourceID(t, "vndb")
		w1 := mkWork(t, "rej-multi-a")
		w2 := mkWork(t, "rej-multi-b")
		mkRef(t, model.EntityTypeWork, w1, vndb, "vM1", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, w2, vndb, "vM2", model.LinkKindExact)
		insertGame(t, 3200, "", "", "PC", "2001-01-01")
		addVNExtlink(t, "vM1", "3200")
		addVNExtlink(t, "vM2", "3200")
		rejectEG(t, w1, 3200)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.MultiGames)
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Equal(t, 1, st.RelatedPlanned)
		assert.Empty(t, refsFor(t, w1, 3200))
		got := refsFor(t, w2, 3200)
		require.Len(t, got, 1)
		assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
		assert.Equal(t, "rule:eg-xlink-multi", got[0].MatchedBy)
	})
	t.Run("twin A empty", func(t *testing.T) {
		requireDB(t)
		vndb, dlsite := sourceID(t, "vndb"), sourceID(t, "dlsite")
		a := mkWork(t, "rej-twin-a")
		b := mkWork(t, "rej-twin-b")
		mkRef(t, model.EntityTypeWork, a, vndb, "vT1", model.LinkKindExact)
		rel := mkRelease(t, b)
		mkRef(t, model.EntityTypeRelease, rel, dlsite, "RJT1", model.LinkKindExact)
		insertGame(t, 3210, "vT1", "RJT1", "PC", "2001-01-01")
		rejectEG(t, a, 3210)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.TwinGames)
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Equal(t, 1, st.RelatedPlanned)
		assert.Empty(t, refsFor(t, a, 3210))
		got := refsFor(t, b, 3210)
		require.Len(t, got, 1)
		assert.Equal(t, model.LinkKindRelated, got[0].LinkKind)
		assert.Equal(t, "rule:eg-xlink-twin", got[0].MatchedBy)
	})
	t.Run("twin A non-empty", func(t *testing.T) {
		requireDB(t)
		vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
		x := mkWork(t, "rej-twin-held")
		w := mkWork(t, "rej-twin-named")
		mkRef(t, model.EntityTypeWork, x, eg, "3220", model.LinkKindExact)
		mkRef(t, model.EntityTypeWork, w, vndb, "vT2", model.LinkKindExact)
		insertGame(t, 3220, "", "", "PC", "2001-01-01")
		addVNExtlink(t, "vT2", "3220")
		rejectEG(t, w, 3220)

		st := runLane(t, true, "")
		assert.Equal(t, 1, st.AnchoredGames)
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Zero(t, st.RelatedPlanned)
		assert.Empty(t, refsFor(t, w, 3220))
		gotX := refsFor(t, x, 3220)
		require.Len(t, gotX, 1)
		assert.Equal(t, model.LinkKindExact, gotX[0].LinkKind)
		assert.Equal(t, "rule:test", gotX[0].MatchedBy)
	})
}

func TestSingleFamilyTitleCorroborationWritesExact(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "DEAR My Friend")
	mkRef(t, model.EntityTypeWork, w, vndb, "vTitleEq", model.LinkKindExact)
	insertNamedGame(t, 3300, "ＤＥＡＲ　Ｍｙ　Ｆｒｉｅｎｄ", "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vTitleEq", "3300")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Equal(t, 1, st.Corroborated)
	assert.Zero(t, st.ProbablePlanned)
	got := refsFor(t, w, 3300)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+title", got[0].MatchedBy)
}

func TestSingleFamilyTitlePrefixCorroborates(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "prefix-host")
	mkRef(t, model.EntityTypeWork, w, vndb, "vTitlePx", model.LinkKindExact)
	mkWorkTitle(t, w, "海の唄がきこえる", model.WorkTitleKindOfficial)
	insertNamedGame(t, 3301, "海の唄がきこえる 上巻", "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vTitlePx", "3301")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Equal(t, 1, st.Corroborated)
	got := refsFor(t, w, 3301)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+title", got[0].MatchedBy)
}

func TestTitlePrefixShorterThanFourDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "ABC")
	mkRef(t, model.EntityTypeWork, w, vndb, "vTitleShort", model.LinkKindExact)
	insertNamedGame(t, 3302, "ABCDEF", "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vTitleShort", "3302")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.Corroborated)
	got := refsFor(t, w, 3302)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb", got[0].MatchedBy)
}

func TestSearchHintTitleDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "search-hint-host")
	mkRef(t, model.EntityTypeWork, w, vndb, "vHint", model.LinkKindExact)
	mkWorkTitle(t, w, "Matching Title Game", model.WorkTitleKindSearchHint)
	insertNamedGame(t, 3303, "Matching Title Game", "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vHint", "3303")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.Corroborated)
	got := refsFor(t, w, 3303)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
}

func TestSingleFamilyDateCorroborationWritesExact(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "date-host")
	mkRef(t, model.EntityTypeWork, w, vndb, "vDate", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, 3400, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vDate", "3400")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Equal(t, 1, st.Corroborated)
	assert.Zero(t, st.ProbablePlanned)
	got := refsFor(t, w, 3400)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+date", got[0].MatchedBy)
}

func TestPartialReleaseDateDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "partial-date")
	mkRef(t, model.EntityTypeWork, w, vndb, "vPart", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), nil)
	insertGame(t, 3401, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vPart", "3401")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.Corroborated)
	got := refsFor(t, w, 3401)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
}

func TestUncorroboratedSingleFamilyStaysProbable(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "uncorroborated")
	mkRef(t, model.EntityTypeWork, w, vndb, "vNone", model.LinkKindExact)
	insertNamedGame(t, 3500, "Completely Different", "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vNone", "3500")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.Corroborated)
	got := refsFor(t, w, 3500)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb", got[0].MatchedBy)
}

func TestTwoFamilyMatchedByUnchanged(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "Same Title")
	mkRef(t, model.EntityTypeWork, w, vndb, "vTwo", model.LinkKindExact)
	insertNamedGame(t, 3600, "Same Title", "vTwo", "", "PC", "2001-01-01")
	addVNExtlink(t, "vTwo", "3600")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Zero(t, st.Corroborated)
	got := refsFor(t, w, 3600)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+egvndb", got[0].MatchedBy)
}

func rejectEG(t *testing.T, workID, egID int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID(t, "erogamescape"),
		ExternalID: strconv.FormatInt(egID, 10), Reason: "not this work",
	}).Error)
}
