package repincovers

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"
	"api/pkg/imageclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed repincovers tests will skip individually")
		os.Exit(m.Run())
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
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

func requireDB(t *testing.T) {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
}

func resetCovers(t *testing.T) {
	t.Helper()
	requireDB(t)
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_work_cover, catalog_work RESTART IDENTITY CASCADE`).Error)
}

func srcID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, key)
	return id
}

func mkWork(t *testing.T, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkCover(t *testing.T, workID int64, hash string, source int16, pinned bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, SortOrder: 0, Kind: "main",
		PortraitPinned: pinned, SourceID: source,
	}).Error)
}

func planWorks(t *testing.T) []int64 {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"metas":{}}}`))
	}))
	t.Cleanup(srv.Close)
	r := &runner{
		db:    testDB,
		cli:   imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "x", ClientSecret: "y"}),
		stats: &Stats{},
	}
	plans, err := r.plan(context.Background(), Opts{})
	require.NoError(t, err)
	ids := make([]int64, 0, len(plans))
	for _, p := range plans {
		ids = append(ids, p.WorkID)
	}
	return ids
}

func TestPlanSkipsHumanPinnedWorks(t *testing.T) {
	resetCovers(t)
	curated := srcID(t, "curated")
	bangumi := srcID(t, "bangumi")

	humanPinned := mkWork(t, "human-pinned")
	mkCover(t, humanPinned, "hash-human-pin", curated, true)
	mkCover(t, humanPinned, "hash-human-alt", bangumi, false)

	machinePinned := mkWork(t, "machine-pinned")
	mkCover(t, machinePinned, "hash-machine-pin", bangumi, true)

	humanUnpinnedMachinePinned := mkWork(t, "human-unpinned")
	mkCover(t, humanUnpinnedMachinePinned, "hash-human-loose", curated, false)
	mkCover(t, humanUnpinnedMachinePinned, "hash-machine-on-loose", bangumi, true)

	unpinnedOnly := mkWork(t, "no-pin")
	mkCover(t, unpinnedOnly, "hash-loose", bangumi, false)

	ids := planWorks(t)
	assert.NotContains(t, ids, humanPinned, "a human-pinned cover takes the whole work off the ladder")
	assert.Contains(t, ids, machinePinned, "a machine-pinned work is still a candidate")
	assert.Contains(t, ids, humanUnpinnedMachinePinned, "an unpinned curated cover does not veto a machine pin")
	assert.NotContains(t, ids, unpinnedOnly, "works with no pin stay out of the candidate set")
}

func TestPlanSkipsWorksWhoseUpstreamPinAPersonSet(t *testing.T) {
	resetCovers(t)
	bangumi := srcID(t, "bangumi")
	w := mkWork(t, "pin set by hand")
	mkCover(t, w, "hash-hand-pin", bangumi, true)
	mkCover(t, w, "hash-other", bangumi, false)
	require.NoError(t, testDB.Exec(`UPDATE catalog_work_cover
		SET field_provenance = '{"portrait_pinned":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}'
		WHERE image_hash = 'hash-hand-pin'`).Error)

	assert.NotContains(t, planWorks(t), w, "a pin set through catalog.work.covers_upstream takes the work off the ladder")
}

func TestPlanLeavesMergedInCoversOutWhileTheWorkHasItsOwn(t *testing.T) {
	resetCovers(t)
	bangumi := srcID(t, "bangumi")
	mkMerged := func(workID int64, hash string, pinned bool) {
		require.NoError(t, testDB.Create(&model.CatalogWorkCover{
			WorkID: workID, ImageHash: hash, Kind: "main", PortraitPinned: pinned,
			SourceID: bangumi, MergedIn: true,
		}).Error)
	}

	survivor := mkWork(t, "survivor")
	mkCover(t, survivor, "hash-own-pin", bangumi, true)
	mkMerged(survivor, "hash-loser-sharper", false)

	onlyMerged := mkWork(t, "only merged art")
	mkMerged(onlyMerged, "hash-merged-pin", true)
	mkMerged(onlyMerged, "hash-merged-alt", false)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"metas":{}}}`))
	}))
	t.Cleanup(srv.Close)
	stats := &Stats{}
	r := &runner{db: testDB, stats: stats,
		cli: imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "x", ClientSecret: "y"})}
	plans, err := r.plan(context.Background(), Opts{})
	require.NoError(t, err)
	require.Len(t, plans, 2)
	assert.Equal(t, 3, stats.Covers,
		"the survivor's merged-in cover is no candidate; a work with only merged-in art keeps them")
}
