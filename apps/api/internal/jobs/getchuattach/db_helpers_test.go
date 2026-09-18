package getchuattach

import (
	"context"
	"fmt"
	"os"
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
	gcTestDSN string
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
					`CREATE SCHEMA IF NOT EXISTS getchuattach_gc`,
					`CREATE TABLE IF NOT EXISTS getchuattach_gc.items (
						getchu_id text PRIMARY KEY,
						status text,
						title text,
						brand text,
						release_date text,
						parsed_json jsonb)`,
				} {
					if err := db.Exec(ddl).Error; err != nil {
						fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: staging fixture failed: %v\n", err)
						os.Exit(m.Run())
						return
					}
				}
				testDSN = dsn
				gcTestDSN = dsn + " options='-csearch_path=getchuattach_gc'"
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
		"catalog_match_rejection", "catalog_external_ref",
		"catalog_work_title", "catalog_work_label",
		"catalog_release", "catalog_revision", "catalog_work", "catalog_label",
		"src_bangumi.subject", "getchuattach_gc.items",
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
	src := sourceID(t, "getchu")
	require.NoError(t, testDB.Create(&model.CatalogWorkLabel{
		WorkID: workID, LabelID: labelID, Kind: model.WorkLabelKindDeveloper, SourceID: &src,
	}).Error)
}

func insertFetched(t *testing.T, id, title, brand, releaseDate string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO getchuattach_gc.items (getchu_id, status, title, brand, release_date)
		 VALUES (?, 'fetched', ?, ?, ?)`,
		id, nullIfEmpty(title), nullIfEmpty(brand), nullIfEmpty(releaseDate),
	).Error)
}

func insertBgmSubject(t *testing.T, id int64, date string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_bangumi.subject
		(id, type, name, name_cn, infobox_raw, parse_error, platform, summary, nsfw, date, series, score, rank, parser_version, ingested_at)
		VALUES (?, 4, 's', '', '', '', 0, '', false, ?, false, 0, 0, 'v', now())`, id, date).Error)
}

func rejectGetchu(t *testing.T, entityType int16, entityID int64, getchuID string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: entityType, EntityID: entityID, SourceID: sourceID(t, "getchu"),
		ExternalID: getchuID, Reason: "not this work",
	}).Error)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func runLane(t *testing.T, apply bool, receipts string) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{
		Apply: apply, DSN: testDSN, GetchuDSN: gcTestDSN, Receipts: receipts,
		Now: time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	return st
}

func getchuRefs(t *testing.T, getchuID string) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND source_id = ? AND external_id = ?",
		model.EntityTypeRelease, sourceID(t, "getchu"), getchuID,
	).Find(&out).Error)
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

func releaseCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_release`).Scan(&n).Error)
	return n
}

func attachedRelease(t *testing.T, getchuID string) model.CatalogRelease {
	t.Helper()
	refs := getchuRefs(t, getchuID)
	require.Len(t, refs, 1)
	var rel model.CatalogRelease
	require.NoError(t, testDB.First(&rel, refs[0].EntityID).Error)
	return rel
}
