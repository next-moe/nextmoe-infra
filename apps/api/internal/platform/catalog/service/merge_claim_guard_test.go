package service

import (
	"testing"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeRefusesALiveClaimIntoAHiddenOne(t *testing.T) {
	cleanTables(t)
	live := createWork(t, "live page").ID
	hidden := createWork(t, "hidden page").ID
	claimWork(t, live, "kungal", live)
	claimWork(t, hidden, "kungal", hidden)
	setClaimState(t, hidden, i16(model.ClaimStateHidden))

	_, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, live, hidden, 7, "wrong way round")
	require.ErrorIs(t, err, ErrHiddenSurvivor)
	_, err = testMerge.ProposeAbsorb(t.Context(), live, hidden, 7, "wrong way round")
	require.ErrorIs(t, err, ErrHiddenSurvivor)
	require.ErrorIs(t, testMerge.CheckSurvivor(t.Context(), model.EntityTypeWork, live, hidden), ErrHiddenSurvivor)

	p, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, hidden, live, 7, "the live page survives")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(t.Context(), p.ID, nil))
}

func TestMergeGuardChecksAgainAtApproveAndExecute(t *testing.T) {
	cleanTables(t)
	a := createWork(t, "a").ID
	b := createWork(t, "b").ID
	c := createWork(t, "c").ID
	for _, id := range []int64{a, b, c} {
		claimWork(t, id, "kungal", id)
	}

	open, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, a, b, 7, "filed while b was live")
	require.NoError(t, err)
	setClaimState(t, b, i16(model.ClaimStateHidden))
	require.ErrorIs(t, testMerge.ApproveMerge(t.Context(), open.ID, 7), ErrHiddenSurvivor)

	approved, err := testMerge.ProposeMerge(t.Context(), model.EntityTypeWork, a, c, 7, "approved while c was live")
	require.NoError(t, err)
	approveAndForceExecutable(t, approved.ID)
	setClaimState(t, c, i16(model.ClaimStateHidden))
	require.ErrorIs(t, testMerge.ExecuteMerge(t.Context(), approved.ID, nil), ErrHiddenSurvivor)

	var liveWorks int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work WHERE id = ? AND deleted_at IS NULL`, a).Scan(&liveWorks).Error)
	assert.EqualValues(t, 1, liveWorks, "a refused execute changes nothing")
}

func TestMergeGuardCoversEveryNonHiddenSource(t *testing.T) {
	cleanTables(t)
	unclaimed := createWork(t, "unclaimed").ID
	hidden := createWork(t, "hidden").ID
	alsoHidden := createWork(t, "also hidden").ID
	draft := createWork(t, "draft").ID
	for _, id := range []int64{hidden, alsoHidden, draft} {
		claimWork(t, id, "kungal", id)
	}
	setClaimState(t, hidden, i16(model.ClaimStateHidden))
	setClaimState(t, alsoHidden, i16(model.ClaimStateHidden))
	setClaimState(t, draft, i16(model.ClaimStateDraft))

	for _, src := range []int64{unclaimed, draft} {
		require.ErrorIs(t, testMerge.CheckSurvivor(t.Context(), model.EntityTypeWork, src, hidden), ErrHiddenSurvivor, "%d into hidden", src)
	}
	for _, pair := range [][2]int64{{hidden, unclaimed}, {hidden, draft}, {alsoHidden, hidden}, {unclaimed, draft}} {
		require.NoError(t, testMerge.CheckSurvivor(t.Context(), model.EntityTypeWork, pair[0], pair[1]), "%d into %d", pair[0], pair[1])
	}
	require.NoError(t, testMerge.CheckSurvivor(t.Context(), model.EntityTypeCharacter, unclaimed, hidden))
}

func TestMergeRefusesFoldingAPublishedClaimIntoADraft(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	draft := createWork(t, "黄莓C")
	claimWork(t, draft.ID, "kungal", draft.ID)
	setClaimState(t, draft.ID, i16(model.ClaimStateDraft))
	published := createWork(t, "黄莓C")
	claimWork(t, published.ID, "kungal", published.ID)

	_, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, published.ID, draft.ID, 7, "wrong way")
	require.ErrorIs(t, err, ErrWeakerSurvivorClaim)
	require.ErrorIs(t, err, ErrSurvivorClaim)

	issues, err := testMerge.PreviewMerge(ctx, model.EntityTypeWork, published.ID, draft.ID)
	require.NoError(t, err)
	var codes []string
	for _, b := range issues.Blockers {
		codes = append(codes, b.Code)
	}
	assert.Contains(t, codes, "weaker_survivor_claim")

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, draft.ID, published.ID, 7, "right way")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogWork
	require.NoError(t, testDB.First(&merged, published.ID).Error)
	assert.Equal(t, model.ClaimStateKeyLive, model.ClaimStateKey(merged.Site, merged.ProductWorkID, merged.ClaimState))
}

func TestMergeCarriesTheClaimStateAndOwnerWithAMovingClaim(t *testing.T) {
	cleanTables(t)
	ctx := t.Context()
	dst := createWork(t, "unclaimed target")
	src := createWork(t, "draft source")
	claimWork(t, src.ID, "kungal", src.ID)
	setClaimState(t, src.ID, i16(model.ClaimStateDraft))
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET owner_user_id = 77 WHERE id = ?`, src.ID).Error)

	p, err := testMerge.ProposeMerge(ctx, model.EntityTypeWork, src.ID, dst.ID, 7, "claim moves")
	require.NoError(t, err)
	approveAndForceExecutable(t, p.ID)
	require.NoError(t, testMerge.ExecuteMerge(ctx, p.ID, nil))

	var merged model.CatalogWork
	require.NoError(t, testDB.First(&merged, dst.ID).Error)
	assert.Equal(t, model.ClaimStateKeyDraft, model.ClaimStateKey(merged.Site, merged.ProductWorkID, merged.ClaimState),
		"a draft that moves stays a draft, not a published page")
	require.NotNil(t, merged.OwnerUserID)
	assert.Equal(t, int64(77), *merged.OwnerUserID)

	_, err = testMerge.UndoMerge(ctx, p.ID, 9, "undo")
	require.NoError(t, err)
	require.NoError(t, testDB.First(&merged, dst.ID).Error)
	assert.Nil(t, merged.Site)
	assert.Nil(t, merged.ClaimState)
	assert.Nil(t, merged.OwnerUserID)
}
