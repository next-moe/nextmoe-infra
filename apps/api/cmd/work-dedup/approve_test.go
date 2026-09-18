package main

import (
	"bytes"
	"context"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/require"
)

const (
	vndbSource    int16 = 2
	curatedSource int16 = 12
)

func approvePair(t *testing.T, conflictSource int16, redirectAToC bool) (int64, int16) {
	t.Helper()
	requireDB(t)
	cleanPipeline(t)

	medium := galgameMedium(t)
	merge := newMerge(t)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	ctx := context.Background()

	a := mkWork(t, medium, "same title", nil)
	b := mkWork(t, medium, "same title", nil)
	holder := a
	if redirectAToC {
		c := mkWork(t, medium, "same title", nil)
		actor := int64(1)
		require.NoError(t, repository.InsertRedirect(testDB, model.EntityTypeWork, a, c, &actor, "test"))
		holder = c
	}
	if conflictSource != 0 {
		mkAnchor(t, holder, conflictSource, "x1")
		mkAnchor(t, b, conflictSource, "x2")
	}

	p, err := merge.ProposeMerge(ctx, model.EntityTypeWork, a, b, 1, "llm:queue-adjudicator 1 conf=0.95")
	require.NoError(t, err)

	var out bytes.Buffer
	require.NoError(t, runApprove(ctx, testDB, &out, merge, resolve, 1, "llm:queue-adjudicator", 0, true))
	t.Log(out.String())

	var status int16
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_merge_proposal WHERE id = ?`, p.ID).Scan(&status).Error)
	return p.ID, status
}

func TestApproveRejectsAnExternalRegistryConflict(t *testing.T) {
	_, status := approvePair(t, vndbSource, false)
	require.Equal(t, model.ProposalStatusRejected, status,
		"two works holding different vndb ids are provably different works")
}

func TestApproveIgnoresAFirstPartyConflict(t *testing.T) {
	_, status := approvePair(t, curatedSource, false)
	require.Equal(t, model.ProposalStatusApproved, status,
		"two curated ids on one game is our own duplicate, which is what the merge fixes")
}

func TestApproveIgnoresAnEditionSplittingWorkConflict(t *testing.T) {
	for _, src := range model.EditionSplittingSourceIDs {
		_, status := approvePair(t, src, false)
		require.Equal(t, model.ProposalStatusApproved, status,
			"ErogameScape and Bangumi list editions separately, so their work ids do not prove two games")
	}
}

func TestEditionSplittingSourcesStillVetoOtherEntities(t *testing.T) {
	for _, src := range model.EditionSplittingSourceIDs {
		require.Contains(t, model.IdentityVetoExemptSourceIDsFor(model.EntityTypeWork), src)
		require.NotContains(t, model.IdentityVetoExemptSourceIDsFor(model.EntityTypePerson), src)
		require.NotContains(t, model.IdentityVetoExemptSourceIDsFor(model.EntityTypeLabel), src)
	}
	require.NotContains(t, model.IdentityVetoExemptSourceIDsFor(model.EntityTypeWork), vndbSource)
}

func TestApproveWithNoConflictIsTheControl(t *testing.T) {
	_, status := approvePair(t, 0, false)
	require.Equal(t, model.ProposalStatusApproved, status)
}

// The proposal is filed on (a, b) but a has already been merged into c, so the
// merge would land on (c, b). Screening the ids the proposal carries sees no
// conflict; screening what the operation actually touches does.
func TestApproveScreensTheResolvedEndpoints(t *testing.T) {
	_, status := approvePair(t, vndbSource, true)
	require.Equal(t, model.ProposalStatusRejected, status)
}

func TestApproveRecordsTheConflictingIdsInTheNote(t *testing.T) {
	id, _ := approvePair(t, vndbSource, false)
	var note string
	require.NoError(t, testDB.Raw(`SELECT note FROM catalog_merge_proposal WHERE id = ?`, id).Scan(&note).Error)
	require.Contains(t, note, "ref-conflict: vndb x1 vs x2")
}
