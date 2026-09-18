package eganchors

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeletedReleaseDateDoesNotCorroborate(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "deleted-release-host")
	mkRef(t, model.EntityTypeWork, w, vndb, "vDelRel", model.LinkKindExact)
	rel := mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	require.NoError(t, testDB.Delete(&model.CatalogRelease{}, rel).Error)
	insertGame(t, 3500, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vDelRel", "3500")

	st := runLane(t, true, "")
	assert.Zero(t, st.Corroborated)
	assert.Equal(t, 1, st.ProbablePlanned)
	got := refsFor(t, w, 3500)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
}

func TestDeletedReleaseIsNotDLsiteEvidence(t *testing.T) {
	requireDB(t)
	dlsite := sourceID(t, "dlsite")
	w := mkWork(t, "deleted-dlsite-release")
	rel := mkRelease(t, w)
	mkRef(t, model.EntityTypeRelease, rel, dlsite, "RJ3501", model.LinkKindExact)
	require.NoError(t, testDB.Delete(&model.CatalogRelease{}, rel).Error)
	insertGame(t, 3501, "", "RJ3501", "PC", "2001-01-01")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.NoEvidenceGames)
	assert.Empty(t, allEGRefs(t))
}

func TestDeadEGRefIsNotAHolding(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	x := mkWork(t, "dead-eg-holder")
	mkDeadRef(t, model.EntityTypeWork, x, eg, "3502")
	w := mkWork(t, "vndb-named")
	mkRef(t, model.EntityTypeWork, w, vndb, "vDeadEG", model.LinkKindExact)
	insertGame(t, 3502, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vDeadEG", "3502")

	st := runLane(t, true, "")
	assert.Zero(t, st.AnchoredGames)
	assert.Zero(t, st.TwinGames)
	got := refsFor(t, w, 3502)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
}
