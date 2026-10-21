package service

import (
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeRequestLifecycle(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	src := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "requested loser").ID
	dst := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "requested survivor").ID

	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, src, dst, 42, "same game, two entries", MergeChoices{})
	require.NoError(t, err)
	assert.Equal(t, OriginEditor, p.Origin)
	assert.Equal(t, model.ProposalStatusOpen, p.Status)

	_, err = testMerge.RequestMerge(ctx, model.EntityTypeWork, src, dst, 43, "again", MergeChoices{})
	require.ErrorIs(t, err, ErrDuplicateOpenProposal)

	n, err := testMerge.CountMergeRequestsSince(ctx, 42, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, n)

	require.NoError(t, testMerge.ApproveMerge(ctx, p.ID, 7))
	rep, err := testMerge.ExecuteDueRequests(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, DueMergeReport{}, rep, "an approval inside its cooling-off is not due")

	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 minute' WHERE id = ?`, p.ID).Error)
	rep, err = testMerge.ExecuteDueRequests(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, DueMergeReport{Due: 1, Executed: 1}, rep)

	got, err := testMerge.GetMergeRequest(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusExecuted, got.Status)
	var deleted bool
	require.NoError(t, testDB.Raw(`SELECT deleted_at IS NOT NULL FROM catalog_work WHERE id = ?`, src).Scan(&deleted).Error)
	assert.True(t, deleted, "the executed request retired the source")

	mine, err := testMerge.ListMergeRequests(ctx, MergeRequestFilter{ProposedBy: &[]int64{42}[0]})
	require.NoError(t, err)
	require.Len(t, mine, 1)
}

func TestMergeRequestJobLeavesBatchProposalsAlone(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	a := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "batch a").ID
	b := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "batch b").ID
	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, a, b, 7, "batch tool")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)

	rep, err := testMerge.ExecuteDueRequests(ctx, 0)
	require.NoError(t, err)
	assert.Zero(t, rep.Due, "a batch tool executes its own proposals")
}

func TestMergeRequestRefusesTwoMediums(t *testing.T) {
	cleanTables(t)
	game := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "a game").ID
	var novelMedium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'novel'`).Scan(&novelMedium).Error)
	novel := createWorkX(t, novelMedium, model.ContentRatingAllAges, model.WorkStatusLive, "its novel").ID

	_, err := testMerge.RequestMerge(t.Context(), model.EntityTypeWork, novel, game, 42, "adaptation", MergeChoices{})
	require.ErrorIs(t, err, ErrCrossMedium)
}

func TestMergeRequestRejected(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	a := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "reject a").ID
	b := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "reject b").ID
	p, err := testMerge.RequestMerge(ctx, model.EntityTypeWork, a, b, 42, "maybe", MergeChoices{})
	require.NoError(t, err)
	require.NoError(t, testMerge.RejectMerge(ctx, p.ID, 7, "different editions"))
	got, err := testMerge.GetMergeRequest(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, model.ProposalStatusRejected, got.Status)
	assert.Contains(t, got.Note, "different editions")
}
