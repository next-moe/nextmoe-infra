package eganchors

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrimaryLessFamilies(t *testing.T) {
	more := candidate{game: egGame{ID: 300, Model: "NS", Sellday: "2024-01-01"}, families: []string{"vndb", "egvndb"}, transplant: true}
	fewer := candidate{game: egGame{ID: 100, Model: "PC", Sellday: "2000-01-01"}, families: []string{"vndb"}, transplant: false}
	assert.True(t, primaryLess(more, fewer))
	assert.False(t, primaryLess(fewer, more))
}

func TestPrimaryLessTransplant(t *testing.T) {
	orig := candidate{game: egGame{ID: 300, Model: "NS", Sellday: "2024-01-01"}, families: []string{"vndb"}, transplant: false}
	port := candidate{game: egGame{ID: 100, Model: "PC", Sellday: "2000-01-01"}, families: []string{"vndb"}, transplant: true}
	assert.True(t, primaryLess(orig, port))
	assert.False(t, primaryLess(port, orig))
}

func TestPrimaryLessPC(t *testing.T) {
	pc := candidate{game: egGame{ID: 300, Model: "PC", Sellday: "2024-01-01"}, families: []string{"vndb"}}
	ns := candidate{game: egGame{ID: 100, Model: "NS", Sellday: "2000-01-01"}, families: []string{"vndb"}}
	assert.True(t, primaryLess(pc, ns))
	assert.False(t, primaryLess(ns, pc))
}

func TestPrimaryLessSellday(t *testing.T) {
	early := candidate{game: egGame{ID: 300, Model: "PC", Sellday: "2000-01-01"}, families: []string{"vndb"}}
	late := candidate{game: egGame{ID: 100, Model: "PC", Sellday: "2024-01-01"}, families: []string{"vndb"}}
	empty := candidate{game: egGame{ID: 200, Model: "PC", Sellday: ""}, families: []string{"vndb"}}
	assert.True(t, primaryLess(early, late))
	assert.True(t, primaryLess(late, empty))
	assert.True(t, primaryLess(early, empty))
}

func TestPrimaryLessID(t *testing.T) {
	low := candidate{game: egGame{ID: 100, Model: "PC", Sellday: "2000-01-01"}, families: []string{"vndb"}}
	high := candidate{game: egGame{ID: 300, Model: "PC", Sellday: "2000-01-01"}, families: []string{"vndb"}}
	assert.True(t, primaryLess(low, high))
	got := choosePrimary([]candidate{high, low})
	assert.Equal(t, int64(100), got.game.ID)
}

func TestMatchedByJoinsFamiliesInFixedOrder(t *testing.T) {
	assert.Equal(t, "rule:eg-xlink:vndb+egvndb+dlsite", matchedByFamilies([]string{"dlsite", "vndb", "egvndb"}))
	assert.Equal(t, "rule:eg-xlink:vndb+egvndb", matchedByFamilies([]string{"egvndb", "vndb"}))
	assert.Equal(t, "rule:eg-xlink:dlsite", matchedByFamilies([]string{"dlsite"}))
}

func TestClassifyMultiBeforeAnchored(t *testing.T) {
	snap := snapshot{
		games:          []egGame{{ID: 1}},
		vndbLinks:      map[int64][]string{1: {"v1", "v2"}},
		vndbWork:       map[string]int64{"v1": 10, "v2": 20},
		egHoldings:     map[int64][]holding{1: {{WorkID: 10, LinkKind: model.LinkKindExact}}},
		workHasPrimary: map[int64]bool{10: true},
	}
	planned, st := decide(snap)
	assert.Equal(t, 1, st.MultiGames)
	assert.Equal(t, 1, st.AnchoredGames)
	assert.Zero(t, st.TwinGames)
	require.Len(t, planned, 1)
	assert.Equal(t, int64(20), planned[0].WorkID)
	assert.Equal(t, classMulti, planned[0].Class)
	assert.Equal(t, model.LinkKindRelated, planned[0].LinkKind)
}

func TestClassifyTwoVNsSameWorkAreNotMulti(t *testing.T) {
	snap := snapshot{
		games:     []egGame{{ID: 1}},
		vndbLinks: map[int64][]string{1: {"v1", "v2"}},
		vndbWork:  map[string]int64{"v1": 10, "v2": 10},
	}
	planned, st := decide(snap)
	assert.Zero(t, st.MultiGames)
	assert.Equal(t, 1, st.CandidateGames)
	require.Len(t, planned, 1)
	assert.Equal(t, model.LinkKindProbable, planned[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb", planned[0].MatchedBy)
}

func TestClassifySteamIsAbsent(t *testing.T) {
	planned, st := decide(snapshot{games: []egGame{{ID: 1}}})
	assert.Equal(t, 1, st.NoEvidenceGames)
	assert.Empty(t, planned)
}

func TestDecideProbablePrimaryBlocksSecondCandidate(t *testing.T) {
	snap := snapshot{
		games:          []egGame{{ID: 2}},
		vndbLinks:      map[int64][]string{2: {"v1"}},
		vndbWork:       map[string]int64{"v1": 10},
		workHasPrimary: map[int64]bool{10: true},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, classEdition, planned[0].Class)
	assert.Equal(t, model.LinkKindRelated, planned[0].LinkKind)
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	assert.Equal(t, 1, st.RelatedPlanned)
}

func TestDecideRejectionSkipsMultiAndTwin(t *testing.T) {
	t.Run("multi", func(t *testing.T) {
		snap := snapshot{
			games:     []egGame{{ID: 1}},
			vndbLinks: map[int64][]string{1: {"v1", "v2"}},
			vndbWork:  map[string]int64{"v1": 10, "v2": 20},
			rejected:  map[string]struct{}{rejKey(10, 1): {}},
		}
		planned, st := decide(snap)
		assert.Equal(t, 1, st.MultiGames)
		assert.Equal(t, 1, st.RejectedSkips)
		require.Len(t, planned, 1)
		assert.Equal(t, int64(20), planned[0].WorkID)
		assert.Equal(t, classMulti, planned[0].Class)
	})
	t.Run("twin empty A", func(t *testing.T) {
		snap := snapshot{
			games:      []egGame{{ID: 1, VNDB: "v1", DLsiteID: "RJ1"}},
			vndbWork:   map[string]int64{"v1": 10},
			dlsiteWork: map[string]int64{"RJ1": 20},
			rejected:   map[string]struct{}{rejKey(10, 1): {}},
		}
		planned, st := decide(snap)
		assert.Equal(t, 1, st.TwinGames)
		assert.Equal(t, 1, st.RejectedSkips)
		require.Len(t, planned, 1)
		assert.Equal(t, int64(20), planned[0].WorkID)
		assert.Equal(t, classTwin, planned[0].Class)
	})
	t.Run("twin non-empty A", func(t *testing.T) {
		snap := snapshot{
			games:      []egGame{{ID: 1}},
			vndbLinks:  map[int64][]string{1: {"v1"}},
			vndbWork:   map[string]int64{"v1": 20},
			egHoldings: map[int64][]holding{1: {{WorkID: 10, LinkKind: model.LinkKindExact}}},
			rejected:   map[string]struct{}{rejKey(20, 1): {}},
		}
		planned, st := decide(snap)
		assert.Equal(t, 1, st.AnchoredGames)
		assert.Zero(t, st.TwinGames)
		assert.Equal(t, 1, st.RejectedSkips)
		assert.Empty(t, planned)
	})
}

func TestDecideSingleFamilyTitleMakesExact(t *testing.T) {
	snap := snapshot{
		games:      []egGame{{ID: 1, Gamename: "DEAR My Friend"}},
		vndbLinks:  map[int64][]string{1: {"v1"}},
		vndbWork:   map[string]int64{"v1": 10},
		workTitles: map[int64][]string{10: {"ＤＥＡＲ　Ｍｙ　Ｆｒｉｅｎｄ"}},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, model.LinkKindExact, planned[0].LinkKind)
	assert.Equal(t, "title", planned[0].Corroboration)
	assert.Equal(t, "rule:eg-xlink:vndb+title", planned[0].MatchedBy)
	assert.Equal(t, 1, st.Corroborated)
	assert.Equal(t, 1, st.ExactPlanned)
}

func TestDecideTitlePreferredOverDate(t *testing.T) {
	snap := snapshot{
		games:      []egGame{{ID: 1, Gamename: "Alpha Game", Sellday: "2001-01-01"}},
		vndbLinks:  map[int64][]string{1: {"v1"}},
		vndbWork:   map[string]int64{"v1": 10},
		workTitles: map[int64][]string{10: {"Alpha Game"}},
		workDates:  map[int64][]string{10: {"2001-01-01"}},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, "title", planned[0].Corroboration)
	assert.Equal(t, "rule:eg-xlink:vndb+title", planned[0].MatchedBy)
	assert.Equal(t, 1, st.Corroborated)
}

func TestDecideTwoFamilyMatchedByIgnoresTitle(t *testing.T) {
	snap := snapshot{
		games:      []egGame{{ID: 1, VNDB: "v1", Gamename: "Same"}},
		vndbLinks:  map[int64][]string{1: {"v1"}},
		vndbWork:   map[string]int64{"v1": 10},
		workTitles: map[int64][]string{10: {"Same"}},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, model.LinkKindExact, planned[0].LinkKind)
	assert.Equal(t, "rule:eg-xlink:vndb+egvndb", planned[0].MatchedBy)
	assert.Empty(t, planned[0].Corroboration)
	assert.Zero(t, st.Corroborated)
}

func TestDecideSingleFamilyDateMakesExact(t *testing.T) {
	snap := snapshot{
		games:     []egGame{{ID: 1, Sellday: "2001-01-01"}},
		vndbLinks: map[int64][]string{1: {"v1"}},
		vndbWork:  map[string]int64{"v1": 10},
		workDates: map[int64][]string{10: {"2001-01-01"}},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, model.LinkKindExact, planned[0].LinkKind)
	assert.Equal(t, "date", planned[0].Corroboration)
	assert.Equal(t, "rule:eg-xlink:vndb+date", planned[0].MatchedBy)
	assert.Equal(t, 1, st.Corroborated)
}

func TestDecidePrefixShorterThanFourStaysProbable(t *testing.T) {
	snap := snapshot{
		games:      []egGame{{ID: 1, Gamename: "ABCDEF"}},
		vndbLinks:  map[int64][]string{1: {"v1"}},
		vndbWork:   map[string]int64{"v1": 10},
		workTitles: map[int64][]string{10: {"ABC"}},
	}
	planned, st := decide(snap)
	require.Len(t, planned, 1)
	assert.Equal(t, model.LinkKindProbable, planned[0].LinkKind)
	assert.Zero(t, st.Corroborated)
}
