package eganchors

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeadExactOnDifferentLiveWorkPlansProbable(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	x := mkWork(t, "dead-exact-other")
	mkDeadRef(t, model.EntityTypeWork, x, eg, "4100")
	w := mkWork(t, "slot-named-live")
	mkRef(t, model.EntityTypeWork, w, vndb, "vSlotLive", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, 4100, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vSlotLive", "4100")

	st := runLane(t, true, "")
	assert.Zero(t, st.ExactPlanned)
	assert.Equal(t, 1, st.ExactSlotTaken)
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.SlotHeld)
	got := refsFor(t, w, 4100)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	held := refsFor(t, x, 4100)
	require.Len(t, held, 1)
	assert.NotNil(t, held[0].DeadAt)

	second := runLane(t, true, "")
	assert.Zero(t, second.ExactPlanned)
	assert.Zero(t, second.ProbablePlanned)
	assert.Zero(t, second.RelatedPlanned)
	assert.Zero(t, second.Written)
}

func TestExactOnSoftDeletedWorkPlansProbable(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	d := mkWork(t, "deleted-exact-holder")
	mkRef(t, model.EntityTypeWork, d, eg, "4101", model.LinkKindExact)
	require.NoError(t, testDB.Delete(&model.CatalogWork{}, d).Error)
	w := mkWork(t, "slot-named-deleted")
	mkRef(t, model.EntityTypeWork, w, vndb, "vSlotDel", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, 4101, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vSlotDel", "4101")

	st := runLane(t, true, "")
	assert.Zero(t, st.ExactPlanned)
	assert.Equal(t, 1, st.ExactSlotTaken)
	assert.Equal(t, 1, st.ProbablePlanned)
	assert.Zero(t, st.SlotHeld)
	got := refsFor(t, w, 4101)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindProbable, got[0].LinkKind)
	held := refsFor(t, d, 4101)
	require.Len(t, held, 1)
	assert.Equal(t, model.LinkKindExact, held[0].LinkKind)
	assert.Nil(t, held[0].DeadAt)

	second := runLane(t, true, "")
	assert.Zero(t, second.ExactPlanned)
	assert.Zero(t, second.ProbablePlanned)
	assert.Zero(t, second.RelatedPlanned)
	assert.Zero(t, second.Written)
}

func TestDeadExactOnNamedWorkIsSlotHeld(t *testing.T) {
	requireDB(t)
	vndb, eg := sourceID(t, "vndb"), sourceID(t, "erogamescape")
	w := mkWork(t, "slot-named-same")
	mkDeadRef(t, model.EntityTypeWork, w, eg, "4102")
	mkRef(t, model.EntityTypeWork, w, vndb, "vSlotSame", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, 4102, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vSlotSame", "4102")

	st := runLane(t, true, "")
	assert.Zero(t, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	assert.Zero(t, st.RelatedPlanned)
	assert.Equal(t, 1, st.SlotHeld)
	assert.Zero(t, st.ExactSlotTaken)
	assert.Zero(t, st.Written)
	got := refsFor(t, w, 4102)
	require.Len(t, got, 1)
	assert.NotNil(t, got[0].DeadAt)

	second := runLane(t, true, "")
	assert.Zero(t, second.ExactPlanned)
	assert.Zero(t, second.ProbablePlanned)
	assert.Zero(t, second.RelatedPlanned)
	assert.Equal(t, 1, second.SlotHeld)
	assert.Zero(t, second.Written)
}

func TestCleanSlotStillWritesExact(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	w := mkWork(t, "slot-named-clean")
	mkRef(t, model.EntityTypeWork, w, vndb, "vSlotClean", model.LinkKindExact)
	mkReleaseYMD(t, w, i16(2001), i16(1), i16(1))
	insertGame(t, 4103, "", "", "PC", "2001-01-01")
	addVNExtlink(t, "vSlotClean", "4103")

	st := runLane(t, true, "")
	assert.Equal(t, 1, st.ExactPlanned)
	assert.Zero(t, st.ProbablePlanned)
	assert.Zero(t, st.ExactSlotTaken)
	assert.Zero(t, st.SlotHeld)
	assert.Equal(t, 1, st.Written)
	got := refsFor(t, w, 4103)
	require.Len(t, got, 1)
	assert.Equal(t, model.LinkKindExact, got[0].LinkKind)
}
