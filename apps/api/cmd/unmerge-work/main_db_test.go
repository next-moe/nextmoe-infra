package main

import (
	"fmt"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/repository"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
			os.Exit(1)
		}
		for _, step := range []func(*gorm.DB) error{migrate.Run, seed.Run} {
			if err := step(db); err != nil {
				fmt.Fprintf(os.Stderr, "FAIL: catalog test setup: %v\n", err)
				os.Exit(1)
			}
		}
		testDB = db
	}
	os.Exit(m.Run())
}

func setup(t *testing.T) (*service.MergeService, int16) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{
		"catalog_merge_proposal", "catalog_redirect", "catalog_external_ref",
		"catalog_revision", "catalog_release", "catalog_work_title", "catalog_work",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
	var medium int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&medium).Error)
	require.NotZero(t, medium)
	resolve := service.NewResolveService(repository.NewRedirectRepository(testDB))
	return service.NewMergeService(testDB, resolve,
		repository.NewProposalRepository(testDB), repository.NewRevisionRepository(testDB)), medium
}

func mkWork(t *testing.T, medium int16, name string) int64 {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: medium, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, testDB.Create(w).Error)
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: w.ID, Lang: "ja", Title: name, Kind: model.WorkTitleKindOfficial,
	}).Error)
	return w.ID
}

func mkExact(t *testing.T, workID int64, sourceID int16, externalID string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID,
		ExternalID: externalID, LinkKind: model.LinkKindExact, MatchedBy: "test",
	}).Error)
}

func executedMerge(t *testing.T, merge *service.MergeService, source, target int64) int64 {
	t.Helper()
	actor := int64(1)
	p, err := merge.ProposeMerge(t.Context(), model.EntityTypeWork, source, target, actor, "test")
	require.NoError(t, err)
	require.NoError(t, merge.ApproveMerge(t.Context(), p.ID, actor))
	require.NoError(t, testDB.Model(&model.CatalogMergeProposal{}).Where("id = ?", p.ID).
		Update("execute_after", gorm.Expr("now() - interval '1 minute'")).Error)
	require.NoError(t, merge.ExecuteMerge(t.Context(), p.ID, &actor))
	return p.ID
}

// The whole point of the CLI's warning banner. rebuildFromSnapshot restores the
// work row and its titles only, so a redistribution pass is still needed; if
// that ever changes, this test says so instead of the banner quietly lying.
func TestUnmergeRebuildsTheRowAndLeavesTheChildrenOnTheSurvivor(t *testing.T) {
	merge, medium := setup(t)
	source := mkWork(t, medium, "ふたりぐらし 2025")
	target := mkWork(t, medium, "ふたりぐらし 2008")
	mkExact(t, source, model.SourceBangumi, "63090")
	mkExact(t, target, model.SourceBangumi, "7635")

	proposalID := executedMerge(t, merge, source, target)

	p, err := loadExecuted(t.Context(), testDB, proposalID)
	require.NoError(t, err)
	require.NoError(t, describe(t.Context(), testDB, p))

	newID, err := merge.Unmerge(t.Context(), proposalID, &[]int64{1}[0])
	require.NoError(t, err)
	require.NotEqual(t, source, newID, "the rebuild takes a new id")

	var rebuilt model.CatalogWork
	require.NoError(t, testDB.First(&rebuilt, newID).Error)
	require.Equal(t, "ふたりぐらし 2025", rebuilt.DisplayName)
	require.Nil(t, rebuilt.ProductWorkID, "a rebuilt work carries no claim")

	var titles int64
	require.NoError(t, testDB.Model(&model.CatalogWorkTitle{}).Where("work_id = ?", newID).Count(&titles).Error)
	require.EqualValues(t, 1, titles, "titles come back with the row")

	var onNew, onTarget int64
	require.NoError(t, testDB.Model(&model.CatalogExternalRef{}).
		Where("entity_type = ? AND entity_id = ?", model.EntityTypeWork, newID).Count(&onNew).Error)
	require.NoError(t, testDB.Model(&model.CatalogExternalRef{}).
		Where("entity_type = ? AND entity_id = ?", model.EntityTypeWork, target).Count(&onTarget).Error)
	require.EqualValues(t, 0, onNew,
		"refs do NOT come back — if this ever passes with 1, the CLI's banner and the operator's "+
			"redistribution step are both wrong")
	require.EqualValues(t, 2, onTarget, "both upstream ids are still on the survivor")

	var current int64
	require.NoError(t, testDB.Raw(`SELECT current_id FROM catalog_redirect WHERE entity_type = ? AND old_id = ?`,
		model.EntityTypeWork, source).Scan(&current).Error)
	require.Equal(t, newID, current, "the old id now points at the rebuild, not the survivor")
}

func TestLoadExecutedRefusesWhatItCannotUndo(t *testing.T) {
	merge, medium := setup(t)
	source := mkWork(t, medium, "源")
	target := mkWork(t, medium, "目标")

	p, err := merge.ProposeMerge(t.Context(), model.EntityTypeWork, source, target, 1, "test")
	require.NoError(t, err)

	_, err = loadExecuted(t.Context(), testDB, p.ID)
	require.Error(t, err, "an open proposal has nothing to undo")

	_, err = loadExecuted(t.Context(), testDB, p.ID+9999)
	require.Error(t, err, "a proposal that does not exist is an error, not a no-op")
}
