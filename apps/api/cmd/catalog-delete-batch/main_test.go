package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed catalog-delete-batch tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL: cannot connect to the assigned test database")
		os.Exit(1)
	}
	if err := migrate.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog migrate failed: %v\n", err)
		os.Exit(1)
	}
	if err := seed.Run(db); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: catalog seed failed: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	for _, table := range []string{"catalog_work", "catalog_deletion_request", "catalog_revision"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mkWork(t *testing.T, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

type requestRow struct {
	EntityID int64
	Status   int16
	Reason   string
	Note     string
}

func requests(t *testing.T) map[int64]requestRow {
	t.Helper()
	var rows []requestRow
	require.NoError(t, testDB.Raw(`SELECT entity_id, status, reason, note FROM catalog_deletion_request`).Scan(&rows).Error)
	out := make(map[int64]requestRow, len(rows))
	for _, r := range rows {
		out[r.EntityID] = r
	}
	return out
}

func TestDeleteBatchWaivedExecutesAndSkipsTheRest(t *testing.T) {
	clean(t)
	ctx := t.Context()
	live := mkWork(t, "Blue Archive")
	gone := mkWork(t, "already gone")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET status = ?, deleted_at = now() WHERE id = ?`, model.WorkStatusDeleted, gone).Error)
	pending := mkWork(t, "someone asked first")
	require.NoError(t, testDB.Create(&model.CatalogDeletionRequest{
		EntityType: model.EntityTypeWork, EntityID: pending, Reason: model.DeletionReasonOther,
		Status: model.ProposalStatusOpen, ProposedBy: 9,
	}).Error)
	ids := []int64{live, gone, pending, live + 1000}

	dry, err := deleteBatch(ctx, testDB, ids, model.DeletionReasonOutOfScope, "scope purge", 1, true, false)
	require.NoError(t, err)
	assert.Equal(t, stats{Listed: 4, Alive: 1, Gone: 2, Pending: 1}, dry)
	assert.Len(t, requests(t), 1, "the dry run files nothing")

	st, err := deleteBatch(ctx, testDB, ids, model.DeletionReasonOutOfScope, "scope purge", 1, true, true)
	require.NoError(t, err)
	assert.Equal(t, stats{Listed: 4, Alive: 1, Gone: 2, Pending: 1, Executed: 1}, st)

	got := requests(t)
	assert.Equal(t, requestRow{EntityID: live, Status: model.ProposalStatusExecuted, Reason: model.DeletionReasonOutOfScope, Note: "scope purge"}, got[live])
	assert.Equal(t, model.ProposalStatusOpen, got[pending].Status)
	var w struct {
		Status  int16
		Deleted bool
	}
	require.NoError(t, testDB.Raw(`SELECT status, deleted_at IS NOT NULL AS deleted FROM catalog_work WHERE id = ?`, live).Scan(&w).Error)
	assert.Equal(t, model.WorkStatusDeleted, w.Status)
	assert.True(t, w.Deleted)
	var revs int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_revision WHERE entity_type = ? AND entity_id = ? AND action = ?`,
		model.EntityTypeWork, live, model.RevisionActionDeleted).Scan(&revs).Error)
	assert.EqualValues(t, 1, revs)
}

func TestDeleteBatchWithoutWaiverLeavesTheCoolingOffToTheJob(t *testing.T) {
	clean(t)
	w := mkWork(t, "Python")
	st, err := deleteBatch(t.Context(), testDB, []int64{w}, model.DeletionReasonNotAWork, "not a game", 1, false, true)
	require.NoError(t, err)
	assert.Equal(t, stats{Listed: 1, Alive: 1}, st)
	assert.Equal(t, model.ProposalStatusApproved, requests(t)[w].Status)
	var deleted bool
	require.NoError(t, testDB.Raw(`SELECT deleted_at IS NOT NULL FROM catalog_work WHERE id = ?`, w).Scan(&deleted).Error)
	assert.False(t, deleted)
}

func TestReadIDsTakesTheFirstFieldOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ids.tsv")
	require.NoError(t, os.WriteFile(p, []byte("# work\tname\n222930\tPython\n\n235189 Blue Archive\n222930\n"), 0o600))
	ids, err := readIDs(p)
	require.NoError(t, err)
	assert.Equal(t, []int64{222930, 235189}, ids)

	require.NoError(t, os.WriteFile(p, []byte("Python\n"), 0o600))
	_, err = readIDs(p)
	require.Error(t, err)
}
