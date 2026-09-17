package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProposeLeavesDecidedPairsAlone(t *testing.T) {
	for _, tbl := range []string{"catalog_merge_proposal", "catalog_redirect", "catalog_work_character", "catalog_character"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve,
		repository.NewProposalRepository(testDB), repository.NewRevisionRepository(testDB))
	const tag = "rule:catalog-dedup history-test"

	file := func(src, dst int64) int64 {
		t.Helper()
		p, err := merge.ProposeMerge(ctx, model.EntityTypeCharacter, src, dst, 1, "earlier night")
		require.NoError(t, err)
		return p.ID
	}

	cooling, coolingDup := mkChar(t, "cooling host"), mkChar(t, "cooling dup")
	require.NoError(t, merge.ApproveMerge(ctx, file(coolingDup, cooling), 1))

	vetoed, vetoedDup := mkChar(t, "vetoed host"), mkChar(t, "vetoed dup")
	require.NoError(t, merge.RejectMerge(ctx, file(vetoedDup, vetoed), 2, "different people"))

	flipped, flippedDup := mkChar(t, "flipped host"), mkChar(t, "flipped dup")
	require.NoError(t, merge.RejectMerge(ctx, file(flipped, flippedDup), 2, "different people"))

	withdrawn, withdrawnDup := mkChar(t, "withdrawn host"), mkChar(t, "withdrawn dup")
	require.NoError(t, merge.WithdrawMerge(ctx, file(withdrawnDup, withdrawn), 1))

	fresh, freshDup := mkChar(t, "fresh host"), mkChar(t, "fresh dup")

	neighbour, neighbourDup, stranger := mkChar(t, "neighbour host"), mkChar(t, "neighbour dup"), mkChar(t, "stranger")
	require.NoError(t, merge.RejectMerge(ctx, file(neighbourDup, stranger), 2, "a different pair"))

	path := writeWorklist(t, strings.Join([]string{
		`{"class":"character","survivor":` + itoa(cooling) + `,"sources":[` + itoa(coolingDup) + `]}`,
		`{"class":"character","survivor":` + itoa(vetoed) + `,"sources":[` + itoa(vetoedDup) + `]}`,
		`{"class":"character","survivor":` + itoa(flipped) + `,"sources":[` + itoa(flippedDup) + `]}`,
		`{"class":"character","survivor":` + itoa(withdrawn) + `,"sources":[` + itoa(withdrawnDup) + `]}`,
		`{"class":"character","survivor":` + itoa(fresh) + `,"sources":[` + itoa(freshDup) + `]}`,
		`{"class":"character","survivor":` + itoa(neighbour) + `,"sources":[` + itoa(neighbourDup) + `]}`,
	}, "\n"))

	countAll := func() int64 {
		t.Helper()
		var n int64
		require.NoError(t, testDB.Model(&model.CatalogMergeProposal{}).Count(&n).Error)
		return n
	}
	before := countAll()

	var dry bytes.Buffer
	require.NoError(t, runPropose(ctx, testDB, &dry, merge, 1, "", path, tag, 0, false))
	assert.Contains(t, dry.String(), " pairs=6 proposals=2 approved=0 skipped=0 decided=4 errors=0",
		"a dry run must count what the real run would file, or a nightly ceiling trips on cooling pairs")
	assert.Equal(t, before, countAll(), "a dry run writes nothing")

	var applied bytes.Buffer
	require.NoError(t, runPropose(ctx, testDB, &applied, merge, 1, "", path, tag, 0, true))
	assert.Contains(t, applied.String(), " pairs=6 proposals=2 approved=2 skipped=0 decided=4 errors=0")

	var filed []model.CatalogMergeProposal
	require.NoError(t, testDB.Where("note LIKE ?", "%"+tag+"%").Order("source_entity_id").Find(&filed).Error)
	require.Len(t, filed, 2)
	got := map[int64]int64{}
	for _, p := range filed {
		got[p.SourceEntityID] = p.TargetEntityID
		assert.Equal(t, model.ProposalStatusApproved, p.Status)
	}
	assert.Equal(t, map[int64]int64{freshDup: fresh, neighbourDup: neighbour}, got,
		"only the undecided pairs are filed; a rejection of another pair decides nothing here")

	var again bytes.Buffer
	require.NoError(t, runPropose(ctx, testDB, &again, merge, 1, "", path, tag, 0, true))
	assert.Contains(t, again.String(), " proposals=0 approved=0 skipped=0 decided=6 errors=0",
		"a second night files nothing: its own cooling proposals count as decided")
	assert.Equal(t, before+2, countAll())
}

func TestProposeCountsAMergedPairAsSkipped(t *testing.T) {
	for _, tbl := range []string{"catalog_merge_proposal", "catalog_redirect", "catalog_work_character", "catalog_character"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve,
		repository.NewProposalRepository(testDB), repository.NewRevisionRepository(testDB))

	host, dup := mkChar(t, "merged host"), mkChar(t, "merged dup")
	p, err := merge.ProposeMerge(ctx, model.EntityTypeCharacter, dup, host, 1, "earlier wave")
	require.NoError(t, err)
	require.NoError(t, merge.ApproveMerge(ctx, p.ID, 1))
	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)
	require.NoError(t, merge.ExecuteMerge(ctx, p.ID, nil))

	path := writeWorklist(t, `{"class":"character","survivor":`+itoa(dup)+`,"sources":[`+itoa(host)+`]}`)
	for _, run := range []bool{false, true} {
		var out bytes.Buffer
		require.NoError(t, runPropose(ctx, testDB, &out, merge, 1, "", path, "rule:catalog-dedup merged-test", 0, run))
		assert.Contains(t, out.String(), " pairs=1 proposals=0 approved=0 skipped=1 decided=0 errors=0")
	}
}
