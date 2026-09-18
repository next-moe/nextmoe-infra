package egworks

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
	"api/internal/platform/catalog/srcbangumi"
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
			} else if err := srcbangumi.EnsureSchema(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: src_bangumi migrate failed: %v\n", err)
			} else if err := seed.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog seed failed: %v\n", err)
			} else {
				for _, ddl := range []string{
					`CREATE SCHEMA IF NOT EXISTS egworks_eg`,
					`CREATE TABLE IF NOT EXISTS egworks_eg.games (
						id bigint PRIMARY KEY,
						gamename text,
						furigana text,
						sellday text,
						model text,
						brand_id bigint,
						erogame boolean,
						vndb text,
						dlsite_id text)`,
					`CREATE TABLE IF NOT EXISTS egworks_eg.game_relations (
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
				egTestDSN = dsn + " options='-csearch_path=egworks_eg'"
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
		"catalog_match_candidate", "catalog_match_rejection", "catalog_external_ref",
		"catalog_work_title", "catalog_work_platform", "catalog_work_label",
		"catalog_release", "catalog_revision", "catalog_work", "catalog_label",
		"src_bangumi.subject", "egworks_eg.games", "egworks_eg.game_relations",
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

func mkRef(t *testing.T, entityType int16, entityID int64, source int16, ext string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: entityType, EntityID: entityID, SourceID: source,
		ExternalID: ext, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
}

func mkLabel(t *testing.T, name string) int64 {
	t.Helper()
	l := model.CatalogLabel{
		DisplayName: name, Lang: "ja", Kind: model.LabelKindGameBrand,
		FieldProvenance: datatypes.JSON([]byte(`{}`)),
	}
	require.NoError(t, testDB.Create(&l).Error)
	return l.ID
}

func mkWorkLabel(t *testing.T, workID, labelID int64) {
	t.Helper()
	src := sourceID(t, "erogamescape")
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
		WorkID: workID, LabelID: labelID, Kind: model.WorkLabelKindDeveloper, SourceID: &src,
	}).Error)
}

func insertGame(t *testing.T, g game) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO egworks_eg.games (id, gamename, furigana, sellday, model, brand_id, erogame)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		g.ID, nullIfEmpty(g.Gamename), nullIfEmpty(g.Furigana), nullIfEmpty(g.Sellday),
		nullIfEmpty(g.Model), nullIfZero(g.BrandID), g.Erogame,
	).Error)
}

func insertBundling(t *testing.T, subject, object int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO egworks_eg.game_relations (game_subject, game_object, raw) VALUES (?, ?, '{"kind":"bundling"}')`,
		subject, object).Error)
}

func insertTransplant(t *testing.T, subject, object int64) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO egworks_eg.game_relations (game_subject, game_object, raw) VALUES (?, ?, '{"kind":"transplant"}')`,
		subject, object).Error)
}

func insertBgmSubject(t *testing.T, id int64, date string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, parser_version, ingested_at)
		VALUES (?, 4, 's', '', '', '', 0, '', false, ?, false, 0, 0, 'v', now())`, id, date).Error)
}

func rejectEG(t *testing.T, workID, egID int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID(t, "erogamescape"),
		ExternalID: strconv.FormatInt(egID, 10), Reason: "not this work",
	}).Error)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func runLane(t *testing.T, apply bool, receipts string) *Stats {
	t.Helper()
	return runLaneLimit(t, apply, receipts, 0)
}

func runLaneLimit(t *testing.T, apply bool, receipts string, limit int) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{
		Apply: apply, DSN: testDSN, EGDSN: egTestDSN, Receipts: receipts, Limit: limit,
		Now: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	return st
}

func refsFor(t *testing.T, workID, egID int64) []model.CatalogExternalRef {
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

func workCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work`).Scan(&n).Error)
	return n
}

func refCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_external_ref`).Scan(&n).Error)
	return n
}

func egRefByGame(t *testing.T, egID int64) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND source_id = ? AND external_id = ?",
		model.EntityTypeWork, sourceID(t, "erogamescape"), strconv.FormatInt(egID, 10),
	).Find(&out).Error)
	return out
}
