package censoredcovers

import (
	"context"
	"fmt"
	"os"
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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed censoredcovers tests will skip individually")
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
	for _, table := range []string{"catalog_work_cover", "catalog_work"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "source %q not seeded", key)
	return id
}

func mkWork(t *testing.T, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkCover(t *testing.T, workID int64, source int16, kind, hash string, sexual int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, Kind: kind, SourceID: source, Sexual: sexual,
	}).Error)
}

func TestLoadCandidatesGhostGate(t *testing.T) {
	clean(t)
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	vndb, curated, upscale := sourceID(t, "vndb"), sourceID(t, "curated"), sourceID(t, "upscale")

	allExplicit := mkWork(t, "official-all-explicit")
	mkCover(t, allExplicit, vndb, "main", "h-ex1", 2)
	mkCover(t, allExplicit, curated, "main", "h-cur1", 0)
	mkCover(t, allExplicit, upscale, "main", "h-ups1", 0)

	hasSafe := mkWork(t, "official-safe-exists")
	mkCover(t, hasSafe, vndb, "main", "h-ex2", 2)
	mkCover(t, hasSafe, vndb, "dig", "h-safe2", 1)

	pkgSafeOnly := mkWork(t, "safe-only-behind-pkg")
	mkCover(t, pkgSafeOnly, vndb, "main", "h-ex3", 2)
	mkCover(t, pkgSafeOnly, vndb, "pkgfront", "h-pkg3", 0)

	staged := mkWork(t, "ghost-already-staged")
	mkCover(t, staged, vndb, "main", "h-ex4", 2)
	mkCover(t, staged, reg.censoredSource, "main", "h-ghost4", 0)

	noExplicit := mkWork(t, "curated-only")
	mkCover(t, noExplicit, curated, "main", "h-cur5", 0)

	cands, err := loadCandidates(context.Background(), testDB, reg, nil)
	require.NoError(t, err)
	got := make([]int64, 0, len(cands))
	for _, c := range cands {
		got = append(got, c.WorkID)
	}
	assert.ElementsMatch(t, []int64{allExplicit, pkgSafeOnly}, got,
		"curated/upscale rows must not count as safe art, pkg safe rows must not either, and a staged ghost is final")

	rows, err := loadExplicitRows(context.Background(), testDB, allExplicit)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "h-ex1", rows[0].ImageHash)
}
