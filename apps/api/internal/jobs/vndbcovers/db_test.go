package vndbcovers

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
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed vndbcovers tests will skip individually")
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
	require.NoError(t, testDB.Exec("TRUNCATE catalog_external_ref CASCADE").Error)
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

func mkWork(t *testing.T, reg registry, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: reg.galgameMedium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, reg registry, workID int64, vndbID string, linkKind int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (?,?,?,?,?,?)`,
		model.EntityTypeWork, workID, reg.vndbSource, vndbID, linkKind, "test").Error)
}

func mkCover(t *testing.T, workID int64, source int16, kind, hash string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, Kind: kind, SourceID: source,
	}).Error)
}

func candidateIDs(t *testing.T, reg registry, ids []int64) []int64 {
	t.Helper()
	cands, err := loadCandidates(context.Background(), testDB, reg, ids)
	require.NoError(t, err)
	out := make([]int64, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.WorkID)
	}
	return out
}

func TestLoadCandidatesOfficialGap(t *testing.T) {
	clean(t)
	reg, err := resolveRegistry(context.Background(), testDB)
	require.NoError(t, err)
	curated, upscale, bangumi := sourceID(t, "curated"), sourceID(t, "upscale"), sourceID(t, "bangumi")

	coverless := mkWork(t, reg, "coverless")
	mkAnchor(t, reg, coverless, "v1", model.LinkKindExact)

	curatedOnly := mkWork(t, reg, "curated-only")
	mkAnchor(t, reg, curatedOnly, "v2", model.LinkKindExact)
	mkCover(t, curatedOnly, curated, "main", "hash-cur")

	wikiLineage := mkWork(t, reg, "curated+upscale")
	mkAnchor(t, reg, wikiLineage, "v3", model.LinkKindExact)
	mkCover(t, wikiLineage, curated, "", "hash-cur2")
	mkCover(t, wikiLineage, upscale, "main", "hash-ups")

	pkgOnly := mkWork(t, reg, "official-pkg-only")
	mkAnchor(t, reg, pkgOnly, "v4", model.LinkKindExact)
	mkCover(t, pkgOnly, reg.vndbSource, "pkgfront", "hash-pkg")

	hasVNDB := mkWork(t, reg, "vndb-covered")
	mkAnchor(t, reg, hasVNDB, "v5", model.LinkKindExact)
	mkCover(t, hasVNDB, reg.vndbSource, "main", "hash-vndb")

	hasBangumi := mkWork(t, reg, "bangumi-covered")
	mkAnchor(t, reg, hasBangumi, "v6", model.LinkKindExact)
	mkCover(t, hasBangumi, bangumi, "main", "hash-bgm")
	mkCover(t, hasBangumi, curated, "main", "hash-cur3")

	probable := mkWork(t, reg, "probable-anchor")
	mkAnchor(t, reg, probable, "v7", model.LinkKindProbable)

	assert.ElementsMatch(t,
		[]int64{coverless, curatedOnly, wikiLineage, pkgOnly},
		candidateIDs(t, reg, nil))

	assert.ElementsMatch(t, []int64{curatedOnly},
		candidateIDs(t, reg, []int64{curatedOnly, hasVNDB}),
		"--ids must not bypass the official-cover gate")
}
