package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecideModeMergesRejectsAndSkipsDecided(t *testing.T) {
	requireDB(t)
	cleanPipeline(t)
	medium := galgameMedium(t)
	live := mkWork(t, medium, "decide live", nil)
	twin := mkWork(t, medium, "decide twin", nil)
	other := mkWork(t, medium, "decide other", nil)
	homonym := mkWork(t, medium, "decide homonym", nil)
	done := mkWork(t, medium, "decide done", nil)
	quarantine(t, twin)
	quarantine(t, homonym)
	file := func(a, b int64, status int16) {
		require.NoError(t, testDB.Create(&model.CatalogMatchCandidate{
			EntityType: model.EntityTypeWork, AID: min(a, b), BID: max(a, b),
			Reason: model.CandidateReasonNameNormEqual, Status: status,
		}).Error)
	}
	file(live, twin, model.CandidateStatusNeedsManual)
	file(other, homonym, model.CandidateStatusDeferred)
	file(live, done, model.CandidateStatusRejected)

	path := filepath.Join(t.TempDir(), "decisions.tsv")
	require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(
		"# a\tb\taction\tsource\ttarget\tevidence\n%d\t%d\tmerge\t%d\t%d\tpanel same\n%d\t%d\treject\t\t\tpanel different\n%d\t%d\treject\t\t\tpanel different\n",
		live, twin, twin, live, homonym, other, live, done)), 0o600))

	queues := service.NewAdminQueueService(testDB, newMerge(t))
	var out bytes.Buffer
	require.NoError(t, runDecide(context.Background(), testDB, &out, queues, 1, "review:test", path, false))
	assert.Contains(t, out.String(), "would_merge:1")
	assert.Zero(t, countRows(t, `SELECT count(*) FROM catalog_merge_proposal`))

	out.Reset()
	require.NoError(t, runDecide(context.Background(), testDB, &out, queues, 1, "review:test", path, true))
	assert.Contains(t, out.String(), "skipped_not_decidable:1")

	var p model.CatalogMergeProposal
	require.NoError(t, testDB.Where("status = ?", model.ProposalStatusOpen).First(&p).Error)
	assert.Equal(t, twin, p.SourceEntityID)
	assert.Equal(t, live, p.TargetEntityID)
	assert.Contains(t, p.Note, "review:test | panel same")

	var status int16
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_match_candidate WHERE a_id = ? AND b_id = ?`,
		min(other, homonym), max(other, homonym)).Scan(&status).Error)
	assert.Equal(t, model.CandidateStatusRejected, status)
	require.NoError(t, testDB.Raw(`SELECT status FROM catalog_work WHERE id = ?`, homonym).Scan(&status).Error)
	assert.Equal(t, model.WorkStatusLive, status, "a rejected pair releases its quarantined side")

	assert.Error(t, runDecide(context.Background(), testDB, &out, queues, 1, "llm:queue-adjudicator", path, true))
}
