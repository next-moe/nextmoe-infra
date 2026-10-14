package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeWorklist(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "worklist.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadWorklistPartitionGuard(t *testing.T) {
	cases := []struct {
		name, body, wantErr string
	}{
		{"survivor reused as source",
			`{"class":"character","survivor":10,"sources":[11]}
{"class":"character","survivor":12,"sources":[10]}`, "already claimed"},
		{"id repeated inside one group",
			`{"class":"character","survivor":10,"sources":[11,11]}`, "already claimed"},
		{"unknown class", `{"class":"work","survivor":10,"sources":[11]}`, "unknown class"},
		{"no sources", `{"class":"character","survivor":10,"sources":[]}`, "no sources"},
		{"bad survivor", `{"class":"character","survivor":0,"sources":[11]}`, "positive id"},
		{"not json", `{`, "worklist.jsonl:1"},
		{"empty file", "", "no groups"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := loadWorklist(writeWorklist(t, tc.body))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestLoadWorklistIgnoresEvidenceKeys(t *testing.T) {
	groups, err := loadWorklist(writeWorklist(t,
		`{"class":"character","survivor":30,"sources":[31],"rule":"E-name","match_keys":["あ"]}

{"class":"character","survivor":20,"sources":[21,22],"members":[{"id":20}]}`))
	require.NoError(t, err)
	require.Len(t, groups, 2)
	assert.Equal(t, int64(20), groups[0].survivor)
	assert.Equal(t, []int64{21, 22}, groups[0].sources)
	assert.Equal(t, int64(30), groups[1].survivor)
	assert.Equal(t, classCharacter, groups[1].class)
}

func TestWorklistDrivesTheMergeMachinery(t *testing.T) {
	for _, tbl := range []string{"catalog_merge_proposal", "catalog_redirect", "catalog_work_character", "catalog_character"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve)

	host, dup := mkChar(t, "W1 host"), mkChar(t, "W1 dup")
	other, otherDup := mkChar(t, "W2 host"), mkChar(t, "W2 dup")
	path := writeWorklist(t, `{"class":"character","survivor":`+itoa(host)+`,"sources":[`+itoa(dup)+`]}`)

	foreign, err := merge.ProposeMerge(ctx, model.EntityTypeCharacter, otherDup, other, 1, waveTag106)
	require.NoError(t, err)
	require.NoError(t, merge.ApproveMerge(ctx, foreign.ID, 1))

	require.NoError(t, runPropose(ctx, testDB, io.Discard, merge, 1, "", path, "", 0, true))

	var opened []model.CatalogMergeProposal
	require.NoError(t, testDB.Where("note LIKE ?", "%"+waveTag154+"%").Find(&opened).Error)
	require.Len(t, opened, 1)
	assert.Equal(t, model.ProposalStatusApproved, opened[0].Status)
	assert.Equal(t, dup, opened[0].SourceEntityID)
	assert.Equal(t, host, opened[0].TargetEntityID)
	require.NotNil(t, opened[0].ExecuteAfter)
	assert.True(t, opened[0].ExecuteAfter.Sub(opened[0].ProposedAt) >= 48*time.Hour,
		"the mandatory 48h cooling window must be intact")

	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, waveTag154, 0, true))
	require.NoError(t, testDB.First(&opened[0], opened[0].ID).Error)
	assert.Equal(t, model.ProposalStatusApproved, opened[0].Status)

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour' WHERE note LIKE ?`,
		"%"+waveTag154+"%").Error)
	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, waveTag154, 0, true))

	var executed model.CatalogMergeProposal
	require.NoError(t, testDB.First(&executed, opened[0].ID).Error)
	assert.Equal(t, model.ProposalStatusExecuted, executed.Status)

	var gone model.CatalogCharacter
	require.NoError(t, testDB.Unscoped().First(&gone, dup).Error)
	assert.NotNil(t, gone.DeletedAt, "the source character must be retired")
	require.NoError(t, testDB.First(&model.CatalogCharacter{}, host).Error)

	var untouched model.CatalogMergeProposal
	require.NoError(t, testDB.First(&untouched, foreign.ID).Error)
	assert.Equal(t, model.ProposalStatusApproved, untouched.Status,
		"another wave's proposal must not be executed by a worklist run")

	require.NoError(t, runPropose(ctx, testDB, io.Discard, merge, 1, "", path, "", 0, true))
	var total int64
	require.NoError(t, testDB.Model(&model.CatalogMergeProposal{}).
		Where("note LIKE ?", "%"+waveTag154+"%").Count(&total).Error)
	assert.EqualValues(t, 1, total, "re-proposing a merged pair must be the idempotent no-op path")
}
