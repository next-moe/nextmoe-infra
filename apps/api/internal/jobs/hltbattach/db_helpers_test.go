package hltbattach

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

var (
	testDB      *gorm.DB
	testDSN     string
	hltbTestDSN string
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
			} else if err := seed.Run(db); err != nil {
				fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: catalog seed failed: %v\n", err)
			} else {
				for _, ddl := range []string{
					`CREATE SCHEMA IF NOT EXISTS hltbattach_hltb`,
					`CREATE TABLE IF NOT EXISTS hltbattach_hltb.games (
						hltb_id bigint PRIMARY KEY,
						title text,
						status text,
						raw jsonb)`,
				} {
					if err := db.Exec(ddl).Error; err != nil {
						fmt.Fprintf(os.Stderr, "DB TESTS SKIPPED: mirror fixture failed: %v\n", err)
						os.Exit(m.Run())
						return
					}
				}
				testDSN = dsn
				hltbTestDSN = dsn + " options='-csearch_path=hltbattach_hltb'"
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
		"catalog_work_title", "catalog_release", "catalog_work",
		"hltbattach_hltb.games",
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

func rejectHLTB(t *testing.T, workID, hltbID int64) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: sourceID(t, "howlongtobeat"),
		ExternalID: strconv.FormatInt(hltbID, 10), Reason: "not this work",
	}).Error)
}

func insertGame(t *testing.T, g game) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"data": map[string]any{
			"game": []any{map[string]string{
				"game_name":     g.Name,
				"game_alias":    g.Alias,
				"release_world": g.World,
				"release_jp":    g.Japan,
			}},
		},
	})
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(
		`INSERT INTO hltbattach_hltb.games (hltb_id, title, status, raw) VALUES (?, ?, 'fetched', ?::jsonb)`,
		g.ID, g.Name, string(raw),
	).Error)
}

func runLane(t *testing.T, apply bool, receipts string) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{
		Apply: apply, DSN: testDSN, HltbDSN: hltbTestDSN, Receipts: receipts,
	})
	require.NoError(t, err)
	return st
}

func refsFor(t *testing.T, workID, hltbID int64) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = ?",
		model.EntityTypeWork, workID, sourceID(t, "howlongtobeat"), strconv.FormatInt(hltbID, 10),
	).Find(&out).Error)
	return out
}

func allHLTBRefs(t *testing.T) []model.CatalogExternalRef {
	t.Helper()
	var out []model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND source_id = ?", model.EntityTypeWork, sourceID(t, "howlongtobeat"),
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

func deadNow() *time.Time {
	now := time.Now()
	return &now
}
