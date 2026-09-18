package eganchors

import (
	"context"
	"fmt"
	"os"
	"strconv"
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
	testDB    *gorm.DB
	testDSN   string
	egTestDSN string
)

func TestMain(m *testing.M) {
	if dsn, ok := dbtest.DSN(); ok {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: gormlogger.Default.LogMode(gormlogger.Silent)})
		switch {
		case err != nil:
			fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: cannot connect: %v\n", err)
		default:
			if err := migrate.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog migrate failed: %v\n", err)
			} else if err := srcvndb.EnsureSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: src_vndb migrate failed: %v\n", err)
			} else if err := seed.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog seed failed: %v\n", err)
			} else {
				for _, ddl := range []string{
					`CREATE SCHEMA IF NOT EXISTS eganchors_eg`,
					`CREATE TABLE IF NOT EXISTS eganchors_eg.games (
						id bigint PRIMARY KEY,
						gamename text,
						vndb text,
						dlsite_id text,
						model text,
						sellday text,
						steam int)`,
					`ALTER TABLE eganchors_eg.games ADD COLUMN IF NOT EXISTS gamename text`,
					`CREATE TABLE IF NOT EXISTS eganchors_eg.game_relations (
						game_subject bigint,
						game_object bigint,
						raw jsonb)`,
				} {
					if err := db.Exec(ddl).Error; err != nil {
						fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: mirror fixture failed: %v\n", err)
						os.Exit(m.Run())
						return
					}
				}
				testDSN = dsn
				egTestDSN = dsn + " options='-csearch_path=eganchors_eg'"
				testDB = db
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "DB TESTS SKIPPED: TEST_DATABASE_DSN is unset")
	}
	os.Exit(m.Run())
}

func requireDB(t *testing.T) *gorm.DB {
	t.Helper()
	if testDB == nil {
		dbtest.Skipf(t, "the catalog test database is unavailable")
	}
	clean(t)
	return testDB
}

func clean(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{
		"catalog_match_rejection", "catalog_external_ref", "catalog_work_title", "catalog_release", "catalog_work",
		"src_vndb.releases_extlinks", "src_vndb.vn_extlinks", "src_vndb.releases_vn", "src_vndb.extlinks",
		"eganchors_eg.games", "eganchors_eg.game_relations",
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

func mkRelease(t *testing.T, workID int64) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: 0, Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`))}
	require.NoError(t, testDB.Create(&rel).Error)
	return rel.ID
}

func mkRef(t *testing.T, entityType int16, entityID int64, source int16, ext string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: source,
		ExternalID: ext, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
}

func mkDeadRef(t *testing.T, entityType int16, entityID int64, source int16, ext string) {
	t.Helper()
	dead := time.Now()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: source,
		ExternalID: ext, LinkKind: model.LinkKindExact, MatchedBy: "rule:test", DeadAt: &dead,
	}).Error)
}

func insertGame(t *testing.T, id int64, vndb, dlsite, modelName, sellday string) {
	t.Helper()
	insertNamedGame(t, id, "", vndb, dlsite, modelName, sellday)
}

func insertNamedGame(t *testing.T, id int64, gamename, vndb, dlsite, modelName, sellday string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO eganchors_eg.games (id, gamename, vndb, dlsite_id, model, sellday) VALUES (?, ?, ?, ?, ?, ?)`,
		id, nullIfEmpty(gamename), nullIfEmpty(vndb), nullIfEmpty(dlsite), nullIfEmpty(modelName), nullIfEmpty(sellday),
	).Error)
}

func mkWorkTitle(t *testing.T, workID int64, title string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkTitle{
		WorkID: workID, Lang: "ja", Title: title, Kind: kind, Provenance: model.WorkTitleProvenanceSource,
	}).Error)
}

func mkReleaseYMD(t *testing.T, workID int64, y, m, d *int16) int64 {
	t.Helper()
	rel := model.CatalogRelease{
		WorkID: workID, Kind: 0, ReleasedY: y, ReleasedM: m, ReleasedD: d,
		Extra: datatypes.JSON([]byte(`{}`)), FieldProvenance: datatypes.JSON([]byte(`{}`)),
	}
	require.NoError(t, testDB.Create(&rel).Error)
	return rel.ID
}

func i16(v int16) *int16 { return &v }

func insertSteamGame(t *testing.T, id int64, steam int) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO eganchors_eg.games (id, steam) VALUES (?, ?)`, id, steam).Error)
}

func insertTransplant(t *testing.T, subject, object int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO eganchors_eg.game_relations (game_subject, game_object, raw) VALUES (?, ?, '{"kind":"transplant"}')`,
		subject, object).Error)
}

func addVNExtlink(t *testing.T, vid, egID string) {
	t.Helper()
	var id int
	require.NoError(t, testDB.Raw(
		`INSERT INTO src_vndb.extlinks (id, site, value)
		 VALUES ((SELECT coalesce(max(id),0)+1 FROM src_vndb.extlinks), 'egs', ?) RETURNING id`,
		egID).Scan(&id).Error)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_vndb.vn_extlinks (id, link) VALUES (?, ?)`, vid, id).Error)
}

func addReleaseExtlink(t *testing.T, rid, vid, egID string) {
	t.Helper()
	var id int
	require.NoError(t, testDB.Raw(
		`INSERT INTO src_vndb.extlinks (id, site, value)
		 VALUES ((SELECT coalesce(max(id),0)+1 FROM src_vndb.extlinks), 'egs', ?) RETURNING id`,
		egID).Scan(&id).Error)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_vndb.releases_extlinks (id, link) VALUES (?, ?)`, rid, id).Error)
	require.NoError(t, testDB.Exec(
		`INSERT INTO src_vndb.releases_vn (id, vid, rtype) VALUES (?, ?, 'complete') ON CONFLICT DO NOTHING`,
		rid, vid).Error)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func runLane(t *testing.T, apply bool, receipts string) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{Apply: apply, DSN: testDSN, EGDSN: egTestDSN, Receipts: receipts})
	require.NoError(t, err)
	return st
}

func refsFor(t *testing.T, workID int64, egID int64) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?",
		model.EntityTypeWork, workID, sourceID(t, "erogamescape"), strconv.FormatInt(egID, 10),
	).Find(&out).Error)
	return out
}

func allEGRefs(t *testing.T) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND source_id = ?", model.EntityTypeWork, sourceID(t, "erogamescape"),
	).Order("external_id, entity_id").Find(&out).Error)
	return out
}

func refCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_external_ref`).Scan(&n).Error)
	return n
}
