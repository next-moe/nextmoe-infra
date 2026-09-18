package eganchors

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOnePrimaryPerWork(t *testing.T) {
	t.Run("families", func(t *testing.T) {
		requireDB(t)
		w := seedPrimaryWork(t, "vF")
		dlsite := sourceID(t, "dlsite")
		rel := mkRelease(t, w)
		mkRef(t, model.EntityTypeRelease, rel, dlsite, "RJF", model.LinkKindExact)
		insertGame(t, 100, "vF", "", "PC", "2000-01-01")
		insertGame(t, 200, "vF", "", "PC", "2000-01-01")
		insertGame(t, 300, "vF", "RJF", "NS", "2024-01-01")
		insertTransplant(t, 300, 1)

		st := runLane(t, true, "")
		assertPrimary(t, st, w, 300, model.LinkKindExact, "rule:eg-xlink:egvndb+dlsite", []int64{100, 200})
	})
	t.Run("transplant", func(t *testing.T) {
		requireDB(t)
		w := seedPrimaryWork(t, "vT")
		insertGame(t, 100, "vT", "", "PC", "2000-01-01")
		insertGame(t, 200, "vT", "", "PC", "2000-01-01")
		insertGame(t, 300, "vT", "", "NS", "2024-01-01")
		insertTransplant(t, 100, 1)
		insertTransplant(t, 200, 1)

		st := runLane(t, true, "")
		assertPrimary(t, st, w, 300, model.LinkKindProbable, "rule:eg-xlink:egvndb", []int64{100, 200})
	})
	t.Run("pc", func(t *testing.T) {
		requireDB(t)
		w := seedPrimaryWork(t, "vP")
		insertGame(t, 100, "vP", "", "NS", "2000-01-01")
		insertGame(t, 200, "vP", "", "NS", "2000-01-01")
		insertGame(t, 300, "vP", "", "PC", "2024-01-01")

		st := runLane(t, true, "")
		assertPrimary(t, st, w, 300, model.LinkKindProbable, "rule:eg-xlink:egvndb", []int64{100, 200})
	})
	t.Run("sellday", func(t *testing.T) {
		requireDB(t)
		w := seedPrimaryWork(t, "vS")
		insertGame(t, 100, "vS", "", "PC", "2024-01-01")
		insertGame(t, 200, "vS", "", "PC", "")
		insertGame(t, 300, "vS", "", "PC", "2000-01-01")

		st := runLane(t, true, "")
		assertPrimary(t, st, w, 300, model.LinkKindProbable, "rule:eg-xlink:egvndb", []int64{100, 200})
	})
	t.Run("id", func(t *testing.T) {
		requireDB(t)
		w := seedPrimaryWork(t, "vI")
		insertGame(t, 100, "vI", "", "PC", "2000-01-01")
		insertGame(t, 200, "vI", "", "PC", "2000-01-01")
		insertGame(t, 300, "vI", "", "PC", "2000-01-01")

		st := runLane(t, true, "")
		assertPrimary(t, st, w, 100, model.LinkKindProbable, "rule:eg-xlink:egvndb", []int64{200, 300})
	})
}

func seedPrimaryWork(t *testing.T, vid string) int64 {
	t.Helper()
	w := mkWork(t, "primary-"+vid)
	mkRef(t, model.EntityTypeWork, w, sourceID(t, "vndb"), vid, model.LinkKindExact)
	return w
}

func assertPrimary(t *testing.T, st *Stats, workID, primaryID int64, kind int16, matchedBy string, editions []int64) {
	t.Helper()
	assert.Equal(t, 1, st.ExactPlanned+st.ProbablePlanned)
	assert.Equal(t, len(editions), st.RelatedPlanned)
	prim := refsFor(t, workID, primaryID)
	require.Len(t, prim, 1)
	assert.Equal(t, kind, prim[0].LinkKind)
	assert.Equal(t, matchedBy, prim[0].MatchedBy)
	for _, id := range editions {
		got := refsFor(t, workID, id)
		require.Len(t, got, 1, "edition %d", id)
		assert.Equal(t, model.LinkKindRelated, got[0].LinkKind, "edition %d", id)
	}
}
