package main

import (
	"context"
	"io"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/catalog-dedup-batch")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/catalog-dedup-batch", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("cmd/catalog-dedup-batch", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("cmd/catalog-dedup-batch", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func mkChar(t *testing.T, name string) int64 {
	t.Helper()
	c := model.CatalogCharacter{DisplayName: name}
	require.NoError(t, testDB.Create(&c).Error)
	return c.ID
}

func TestExecuteChainAware(t *testing.T) {
	for _, tbl := range []string{"catalog_merge_proposal", "catalog_redirect", "catalog_work_character", "catalog_character"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	ctx := context.Background()
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	merge := service.NewMergeService(testDB, resolve)

	k1, k2, k3 := mkChar(t, "K1"), mkChar(t, "K2"), mkChar(t, "K3")
	k4, k5, k6 := mkChar(t, "K4"), mkChar(t, "K5"), mkChar(t, "K6")

	mkProp := func(src, dst int64) int64 {
		t.Helper()
		p, err := merge.ProposeMerge(ctx, model.EntityTypeCharacter, src, dst, 1, waveTag98+" chain test")
		require.NoError(t, err)
		require.NoError(t, merge.ApproveMerge(ctx, p.ID, 1))
		return p.ID
	}
	p12 := mkProp(k1, k2)
	p13 := mkProp(k1, k3)
	p46 := mkProp(k4, k6)
	p56 := mkProp(k5, k6)
	p45 := mkProp(k4, k5)
	require.NoError(t, testDB.Exec(`UPDATE catalog_merge_proposal SET execute_after = now() - interval '1 hour'`).Error)

	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, waveTag98, 0, true))

	status := func(id int64) int16 {
		var p model.CatalogMergeProposal
		require.NoError(t, testDB.First(&p, id).Error)
		return p.Status
	}
	assert.Equal(t, model.ProposalStatusExecuted, status(p12))
	assert.Equal(t, model.ProposalStatusRejected, status(p13), "K1 moved to K2, K3 alive → chain-residual")
	assert.Equal(t, model.ProposalStatusExecuted, status(p46))
	assert.Equal(t, model.ProposalStatusExecuted, status(p56))
	assert.Equal(t, model.ProposalStatusRejected, status(p45), "both endpoints resolve to K6 → chain-superseded")

	var p model.CatalogMergeProposal
	require.NoError(t, testDB.First(&p, p13).Error)
	assert.Contains(t, p.Note, "chain-residual")
	p = model.CatalogMergeProposal{}
	require.NoError(t, testDB.First(&p, p45).Error)
	assert.Contains(t, p.Note, "chain-superseded")

	require.NoError(t, runExecute(ctx, testDB, io.Discard, merge, resolve, 1, waveTag98, 0, true))
	var left int64
	require.NoError(t, testDB.Model(&model.CatalogMergeProposal{}).Where("status = ?", model.ProposalStatusApproved).Count(&left).Error)
	assert.Zero(t, left)
}
