package vndbtags

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	testDB  *gorm.DB
	testDSN string
)

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		if err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: cannot connect to the assigned test database: %v\n", err)
			os.Exit(1)
		}
		if err := migrate.Run(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: catalog migrate failed: %v\n", err)
			os.Exit(1)
		}
		if err := srcvndb.EnsureSchema(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: src_vndb migrate failed: %v\n", err)
			os.Exit(1)
		}
		if err := seed.Run(db); err != nil {
			fmt.Fprintf(os.Stderr, "FAIL: catalog seed failed: %v\n", err)
			os.Exit(1)
		}
		testDSN = dsn
		testDB = db
	} else {
		fmt.Fprintln(os.Stderr, "SKIP: no TEST_DATABASE_DSN — DB-backed vndbtags tests will skip individually")
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skip(t)
	}
	clean(t)
	return testDB
}

func clean(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{
		"catalog_work_tag", "catalog_external_ref", "catalog_work",
		"src_vndb.tags_vn", "src_vndb.tags", "src_vndb.vn",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "the seed must carry the %q source row", key)
	return id
}

func mediumID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func mkWork(t *testing.T, name string) int64 {
	t.Helper()
	return mkWorkStatus(t, name, model.WorkStatusLive)
}

func mkWorkStatus(t *testing.T, name string, status int16) int64 {
	t.Helper()
	w := model.CatalogWork{
		MediumID: mediumID(t), OLang: "ja", DisplayName: name, Status: status,
		Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
	}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, workID int64, vid string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID(t, "vndb"),
		ExternalID: vid, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
}

func mkDeadAnchor(t *testing.T, workID int64, vid string) {
	t.Helper()
	dead := time.Now()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID(t, "vndb"),
		ExternalID: vid, LinkKind: model.LinkKindExact, MatchedBy: "rule:test", DeadAt: &dead,
	}).Error)
}

func mkVN(t *testing.T, id string) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcvndb.VN{
		ID: id, OLang: "ja", IngestedAt: time.Now(),
	}).Error)
}

func mkSrcTag(t *testing.T, id, cat, name, alias string, spoil int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcvndb.Tag{
		ID: id, Cat: cat, DefaultSpoil: spoil, Name: name, Alias: alias,
		Searchable: true, Applicable: true,
	}).Error)
}

func mkVote(t *testing.T, tag, vid, uid string, score int16, spoiler *int16, ignore bool, lie *bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcvndb.TagVN{
		Date: "2020-01-01", Tag: tag, VID: vid, UID: uid, Vote: score,
		Spoiler: spoiler, Ignore: ignore, Lie: lie,
	}).Error)
}

func mkKeptVotes(t *testing.T, tag, vid string, n int, spoiler int16) {
	t.Helper()
	for i := 0; i < n; i++ {
		mkVote(t, tag, vid, fmt.Sprintf("u-%s-%s-%d", tag, vid, i), 2, pi(spoiler), false, nil)
	}
}

func mkWorkTag(t *testing.T, workID int64, name string, src int16, count int, spoiler int16, sexual bool) int64 {
	t.Helper()
	row := model.CatalogWorkTag{
		WorkID: workID, Name: name, SourceID: src, Count: count, Spoiler: spoiler, Sexual: sexual,
	}
	require.NoError(t, testDB.Create(&row).Error)
	return row.ID
}

func loadWorkTags(t *testing.T, workID int64, src int16) []model.CatalogWorkTag {
	t.Helper()
	var rows []model.CatalogWorkTag
	require.NoError(t, testDB.Where("work_id = ? AND source_id = ?", workID, src).Order("name").Find(&rows).Error)
	return rows
}

func tagCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_tag`).Scan(&n).Error)
	return n
}

func runJob(t *testing.T, apply bool, receipts string) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{
		Apply: apply, DSN: testDSN, Receipts: receipts, MinMirrorRows: 1,
	})
	require.NoError(t, err)
	return st
}
