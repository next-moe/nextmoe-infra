package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refState(t *testing.T, entityID int64, src int16, ext string) (int16, bool) {
	t.Helper()
	var row model.CatalogExternalRef
	require.NoError(t, testDB.Raw(`SELECT * FROM catalog_external_ref
	        WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?`,
		model.EntityTypeWork, entityID, src, ext).Scan(&row).Error)
	require.NotEmpty(t, row.ExternalID)
	return row.LinkKind, row.VerifiedAt != nil
}

func TestVerifyRefAsRelatedClearsARowConfirmCannot(t *testing.T) {
	cleanTables(t)

	holder := createWork(t, "bundle holder")
	fanout := createWork(t, "bundle fanout")
	src := sourceID(t, "vndb")
	addExternalRef(t, model.EntityTypeWork, holder.ID, src, "v900", model.LinkKindExact)
	addExternalRef(t, model.EntityTypeWork, fanout.ID, src, "v900", model.LinkKindProbable)

	settleWorks(t)
	before := workUpdatedAt(t, fanout.ID)

	// positive control: this is the row the exact slot makes unconfirmable, and
	// retrying it is what the nightly lane did 10,684 times a pass
	err := testQueues.ConfirmRef(t.Context(), RefKey{
		EntityType: model.EntityTypeWork, EntityID: fanout.ID, SourceID: src, ExternalID: "v900",
	}, 7)
	require.ErrorIs(t, err, ErrExactTaken)
	kind, verified := refState(t, fanout.ID, src, "v900")
	assert.Equal(t, model.LinkKindProbable, kind)
	assert.False(t, verified)

	require.NoError(t, testQueues.VerifyRefAsRelated(t.Context(), RefKey{
		EntityType: model.EntityTypeWork, EntityID: fanout.ID, SourceID: src, ExternalID: "v900",
	}, 7))

	kind, verified = refState(t, fanout.ID, src, "v900")
	assert.Equal(t, model.LinkKindProbable, kind, "the slot belongs to the holder and must stay there")
	assert.True(t, verified)

	holderKind, _ := refState(t, holder.ID, src, "v900")
	assert.Equal(t, model.LinkKindExact, holderKind)

	assert.True(t, workUpdatedAt(t, fanout.ID).After(before))
}

func TestVerifyRefAsRelatedRefusesToRepeatOrPromote(t *testing.T) {
	cleanTables(t)

	work := createWork(t, "ref host")
	src := sourceID(t, "vndb")
	addExternalRef(t, model.EntityTypeWork, work.ID, src, "v901", model.LinkKindProbable)
	addExternalRef(t, model.EntityTypeWork, work.ID, src, "v902", model.LinkKindExact)

	key := RefKey{EntityType: model.EntityTypeWork, EntityID: work.ID, SourceID: src, ExternalID: "v901"}
	require.NoError(t, testQueues.VerifyRefAsRelated(t.Context(), key, 7))
	require.ErrorIs(t, testQueues.VerifyRefAsRelated(t.Context(), key, 7), ErrProposalState)

	// an already-exact row is not a queue row and must not be touched
	exact := RefKey{EntityType: model.EntityTypeWork, EntityID: work.ID, SourceID: src, ExternalID: "v902"}
	require.ErrorIs(t, testQueues.VerifyRefAsRelated(t.Context(), exact, 7), ErrProposalState)

	missing := RefKey{EntityType: model.EntityTypeWork, EntityID: work.ID, SourceID: src, ExternalID: "v999"}
	require.ErrorIs(t, testQueues.VerifyRefAsRelated(t.Context(), missing, 7), ErrNotFound)
}
