package hltbrefs

import (
	"context"
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

var (
	testDB      *gorm.DB
	testDSN     string
	hltbTestDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/hltbrefs")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/hltbrefs", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/hltbrefs", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/hltbrefs", "catalog seed failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS hltbrefs_hltb`,
		`CREATE TABLE IF NOT EXISTS hltbrefs_hltb.games (hltb_id bigint PRIMARY KEY, title text, status text, raw jsonb)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/hltbrefs", "mirror fixture failed: %v", err)
		}
	}
	hltbTestDSN = testDSN + " options='-csearch_path=hltbrefs_hltb'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_match_rejection", "catalog_external_ref", "catalog_release", "catalog_work",
		"hltbrefs_hltb.games",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func ids(t *testing.T) registryIDs {
	t.Helper()
	r, err := resolveIDs(context.Background(), testDB)
	require.NoError(t, err)
	return r
}

func mkWorkWithSteamRelease(t *testing.T, medium, steam int16, name, appid string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	rel := model.CatalogRelease{WorkID: w.ID, Kind: model.ReleaseKindDigital}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: steam,
		ExternalID: appid, LinkKind: model.LinkKindExact, MatchedBy: "rule:vndb-extlink-steam",
	}).Error)
	return w.ID
}

func mkWorkWithSteamWorkRef(t *testing.T, medium, steam int16, name, appid string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: w.ID, SourceID: steam,
		ExternalID: appid, LinkKind: model.LinkKindExact, MatchedBy: "rule:eg-steam",
	}).Error)
	return w.ID
}

func mkMirrorGame(t *testing.T, id int64, status, appid string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO hltbrefs_hltb.games (hltb_id, title, status, raw)
		VALUES (?, 'g', ?, jsonb_build_object('data', jsonb_build_object('game',
			jsonb_build_array(jsonb_build_object('profile_steam', ?::text)))))`, id, status, appid).Error)
}

func refCount(t *testing.T, where string, args ...any) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw("SELECT count(*) FROM catalog_external_ref "+where, args...).Scan(&n).Error)
	return n
}

func TestHltbRefs(t *testing.T) {
	clean(t)
	ctx := context.Background()
	r := ids(t)

	wMatch := mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "matched", "324160")
	mkMirrorGame(t, 1736, "fetched", "324160")

	mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "no-mirror", "111")

	wAmb := mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "shared-appid", "222")
	mkMirrorGame(t, 200, "fetched", "222")
	mkMirrorGame(t, 201, "fetched", "222")

	wWorkGrain := mkWorkWithSteamWorkRef(t, r.galgameMedium, r.steamSource, "work-grain", "666")
	mkMirrorGame(t, 600, "fetched", "666")

	mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "two-works-a", "777")
	mkWorkWithSteamWorkRef(t, r.galgameMedium, r.steamSource, "two-works-b", "777")
	mkMirrorGame(t, 700, "fetched", "777")

	wRej := mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "rejected", "333")
	mkMirrorGame(t, 300, "fetched", "333")
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: wRej, SourceID: r.hltbSource,
		ExternalID: "300", Reason: "test",
	}).Error)

	mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "not-fetched", "444")
	mkMirrorGame(t, 400, "discovered", "444")

	st, err := Run(ctx, Opts{DSN: testDSN, HltbDSN: hltbTestDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 2, st.AmbiguousSteam)
	assert.Equal(t, 1, st.Rejected)
	assert.Equal(t, 2, st.Planned)
	assert.Equal(t, 2, st.Written)
	assert.Zero(t, st.Errors)

	assert.Equal(t, int64(1), refCount(t,
		`WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = '1736' AND link_kind = ? AND matched_by = 'rule:hltb-steam'`,
		model.EntityTypeWork, wMatch, r.hltbSource, model.LinkKindProbable))
	assert.Equal(t, int64(1), refCount(t,
		`WHERE entity_type = ? AND entity_id = ? AND source_id = ? AND external_id = '600'`,
		model.EntityTypeWork, wWorkGrain, r.hltbSource), "a work-grain steam anchor bridges to HLTB too")
	assert.Zero(t, refCount(t, `WHERE source_id = ? AND external_id = '700'`, r.hltbSource),
		"an appid anchored to two works is ambiguous whichever grain each anchor sits at")
	assert.Zero(t, refCount(t, `WHERE source_id = ? AND entity_id = ? AND entity_type = ?`,
		r.hltbSource, wAmb, model.EntityTypeWork))
	assert.Zero(t, refCount(t, `WHERE source_id = ? AND entity_id = ? AND entity_type = ?`,
		r.hltbSource, wRej, model.EntityTypeWork))

	st2, err := Run(ctx, Opts{DSN: testDSN, HltbDSN: hltbTestDSN, Apply: true})
	require.NoError(t, err)
	assert.Zero(t, st2.Written)
	assert.Equal(t, 2, st2.Exists)
}

func TestHltbRefsDry(t *testing.T) {
	clean(t)
	r := ids(t)
	mkWorkWithSteamRelease(t, r.galgameMedium, r.steamSource, "dry", "555")
	mkMirrorGame(t, 500, "fetched", "555")

	st, err := Run(context.Background(), Opts{DSN: testDSN, HltbDSN: hltbTestDSN, Apply: false})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Planned)
	assert.Zero(t, st.Written)
	assert.Zero(t, refCount(t, `WHERE source_id = ?`, r.hltbSource))
}
