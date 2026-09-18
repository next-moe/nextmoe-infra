package main

import (
	"bytes"
	"context"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func quarantine(t *testing.T, id int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET status = ? WHERE id = ?`,
		model.WorkStatusQuarantine, id).Error)
}

func TestReleaseModeDryWritesNothing(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	id := mkWork(t, medium, "dry release host", nil)
	quarantine(t, id)

	queues := service.NewAdminQueueService(testDB, newMerge(t))
	var out bytes.Buffer
	require.NoError(t, runRelease(context.Background(), &out, queues, 1, "test", false))
	assert.Contains(t, out.String(), "[release] quarantined=1 held=0 released=1")

	var status int16
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_work WHERE id = ?`, id).Scan(&status).Error)
	assert.Equal(t, model.WorkStatusQuarantine, status)
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_revision`))
}

func TestReleaseModeReleasesOnlyUnheldQuarantine(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	free := mkWork(t, medium, "unheld", nil)
	pendingHost := mkWork(t, medium, "pending host", nil)
	pendingOther := mkWork(t, medium, "pending other", nil)
	propHost := mkWork(t, medium, "proposal host", nil)
	propOther := mkWork(t, medium, "proposal other", nil)
	live := mkWork(t, medium, "already live", nil)
	quarantine(t, free)
	quarantine(t, pendingHost)
	quarantine(t, propHost)
	require.NoError(t, testDB.Create(&model.CatalogMatchCandidate{
		EntityType: model.EntityTypeWork, AID: min(pendingHost, pendingOther), BID: max(pendingHost, pendingOther),
		Reason: model.CandidateReasonNameNormEqual, Status: model.CandidateStatusPending,
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogMergeProposal{
		EntityType:     model.EntityTypeWork,
		SourceEntityID: propHost,
		TargetEntityID: propOther,
		Status:         model.ProposalStatusOpen,
		ProposedBy:     1,
		Note:           "test",
	}).Error)

	queues := service.NewAdminQueueService(testDB, newMerge(t))
	var out bytes.Buffer
	require.NoError(t, runRelease(context.Background(), &out, queues, 1, "test", true))
	assert.Contains(t, out.String(), "[release] quarantined=3 held=2 released=1")

	statusOf := func(id int64) int16 {
		var s int16
		require.NoError(t, testDB.Raw(`SELECT status FROM catalog_work WHERE id = ?`, id).Scan(&s).Error)
		return s
	}
	assert.Equal(t, model.WorkStatusLive, statusOf(free))
	assert.Equal(t, model.WorkStatusQuarantine, statusOf(pendingHost))
	assert.Equal(t, model.WorkStatusQuarantine, statusOf(propHost))
	assert.Equal(t, model.WorkStatusLive, statusOf(live))
}
