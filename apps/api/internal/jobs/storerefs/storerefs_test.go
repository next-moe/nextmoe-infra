package storerefs

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcbangumi"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB    *gorm.DB
	testDSN   string
	egTestDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/storerefs")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/storerefs", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/storerefs", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/storerefs", "catalog seed failed: %v", err)
	}
	if err := srcbangumi.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/storerefs", "src_bangumi migrate failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS storerefs_eg`,
		`CREATE TABLE IF NOT EXISTS storerefs_eg.games (id bigint PRIMARY KEY, steam bigint, dmm text)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/storerefs", "mirror fixture failed: %v", err)
		}
	}
	egTestDSN = testDSN + " options='-csearch_path=storerefs_eg'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_match_rejection", "catalog_external_ref", "catalog_release", "catalog_work",
		"storerefs_eg.games", "src_bangumi.subject",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "source %s must be seeded", key)
	return id
}

func mediumID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func mkWork(t *testing.T, medium int16, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkAnchor(t *testing.T, workID int64, externalID string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: externalID, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
}

func TestImportStoreRefs(t *testing.T) {
	clean(t)
	medium := mediumID(t)
	egSrc := sourceID(t, "erogamescape")
	steamSrc := sourceID(t, "steam")
	dmmSrc := sourceID(t, "dmm")

	wA := mkWork(t, medium, "both-stores")
	wB := mkWork(t, medium, "dmm-rejected")
	wC := mkWork(t, medium, "steam-preexisting")
	mkAnchor(t, wA, "201", egSrc)
	mkAnchor(t, wB, "202", egSrc)
	mkAnchor(t, wC, "203", egSrc)
	require.NoError(t, testDB.Exec(`INSERT INTO storerefs_eg.games (id, steam, dmm) VALUES
		(201, 4710010, '1564apc14970'), (202, NULL, 'elf_0035'), (203, 1689910, NULL)`).Error)

	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: wB, SourceID: dmmSrc,
		ExternalID: "elf_0035", Reason: "test rejection",
	}).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: wC, SourceID: steamSrc,
		ExternalID: "1689910", LinkKind: model.LinkKindExact, MatchedBy: "human:1",
	}).Error)

	ctx := context.Background()
	opts := Opts{DSN: testDSN, EGDSN: egTestDSN}

	st, err := Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 3, st.Anchored)
	assert.Equal(t, 2, st.SteamPlanned, "A + C (existence is discovered at write time)")
	assert.Equal(t, 1, st.DmmPlanned, "A only — B is rejection-blocked")
	assert.Equal(t, 1, st.Rejected)
	assert.Zero(t, st.SteamWritten+st.DmmWritten)

	opts.Apply = true
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st.SteamWritten, "A written; C already asserted")
	assert.Equal(t, 1, st.SteamExists, "C skipped — never re-grade")
	assert.Equal(t, 1, st.DmmWritten)
	assert.Zero(t, st.Errors)

	var ref model.CatalogExternalRef
	require.NoError(t, testDB.Where(
		"entity_type = ? AND entity_id = ? AND source_id = ?", model.EntityTypeWork, wA, steamSrc).First(&ref).Error)
	assert.Equal(t, model.LinkKindProbable, ref.LinkKind)
	assert.Equal(t, "rule:eg-steam", ref.MatchedBy)
	ref = model.CatalogExternalRef{}
	require.NoError(t, testDB.Where(
		"entity_type = ? AND entity_id = ? AND source_id = ?", model.EntityTypeWork, wA, dmmSrc).First(&ref).Error)
	assert.Equal(t, "1564apc14970", ref.ExternalID)
	assert.Equal(t, "rule:eg-dmm", ref.MatchedBy)
	ref = model.CatalogExternalRef{}
	require.NoError(t, testDB.Where(
		"entity_type = ? AND entity_id = ? AND source_id = ?", model.EntityTypeWork, wC, steamSrc).First(&ref).Error)
	assert.Equal(t, model.LinkKindExact, ref.LinkKind, "existing assertion never re-graded")
	err = testDB.Where("entity_type = ? AND entity_id = ? AND source_id = ?",
		model.EntityTypeWork, wB, dmmSrc).First(&model.CatalogExternalRef{}).Error
	assert.Error(t, err)

	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Zero(t, st.SteamWritten+st.DmmWritten)
	assert.Equal(t, 2, st.SteamExists)
	assert.Equal(t, 1, st.DmmExists)
}

func mkSubject(t *testing.T, id int64, infobox string) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcbangumi.Subject{
		ID: id, Type: 4, Name: "s", InfoboxParsed: []byte(infobox), IngestedAt: time.Now(),
	}).Error)
}

func linkInfobox(urls ...string) string {
	items := make([]string, 0, len(urls))
	for _, u := range urls {
		items = append(items, `{"Key":"","Value":"`+u+`"}`)
	}
	return `{"Type":"Game","Fields":[{"Key":"链接","Value":"","Array":true,"Items":[` + strings.Join(items, ",") + `]}]}`
}

func TestBgmSteamLane(t *testing.T) {
	clean(t)
	medium := mediumID(t)
	bgmSrc := sourceID(t, "bangumi")
	steamSrc := sourceID(t, "steam")

	wOne := mkWork(t, medium, "one-appid")
	mkAnchor(t, wOne, "11", bgmSrc)
	mkSubject(t, 11, linkInfobox("https://store.steampowered.com/app/1001/One/"))

	wBare := mkWork(t, medium, "bare-number")
	mkAnchor(t, wBare, "12", bgmSrc)
	mkSubject(t, 12, `{"Type":"Game","Fields":[{"Key":"Steam","Value":"1002 (2015-06-18)"}]}`)

	wTwo := mkWork(t, medium, "two-appids")
	mkAnchor(t, wTwo, "13", bgmSrc)
	mkSubject(t, 13, linkInfobox("https://store.steampowered.com/app/1003/", "https://steamdb.info/app/1004/"))

	wHas := mkWork(t, medium, "has-release-steam")
	mkAnchor(t, wHas, "14", bgmSrc)
	mkSubject(t, 14, linkInfobox("https://store.steampowered.com/app/1005/"))
	rel := model.CatalogRelease{WorkID: wHas, Kind: model.ReleaseKindDigital}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: steamSrc,
		ExternalID: "1006", LinkKind: model.LinkKindExact, MatchedBy: "rule:vndb-extlink-steam",
	}).Error)

	wNone := mkWork(t, medium, "no-steam")
	mkAnchor(t, wNone, "15", bgmSrc)
	mkSubject(t, 15, linkInfobox("https://example.com/"))

	wRej := mkWork(t, medium, "rejected")
	mkAnchor(t, wRej, "16", bgmSrc)
	mkSubject(t, 16, linkInfobox("https://store.steampowered.com/app/1007/"))
	require.NoError(t, testDB.Create(&model.CatalogMatchRejection{
		EntityType: model.EntityTypeWork, EntityID: wRej, SourceID: steamSrc,
		ExternalID: "1007", Reason: "test rejection",
	}).Error)

	ctx := context.Background()
	st, err := Run(ctx, Opts{DSN: testDSN, Only: LaneBgm, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 6, st.BgmAnchored)
	assert.Equal(t, 5, st.BgmStated)
	assert.Equal(t, 1, st.BgmAmbiguous)
	assert.Equal(t, 1, st.BgmHasSteam)
	assert.Equal(t, 1, st.Rejected)
	assert.Equal(t, 2, st.BgmPlanned)
	assert.Equal(t, 2, st.BgmWritten)
	assert.Zero(t, st.Anchored, "the eg lane did not run")

	for workID, appid := range map[int64]string{wOne: "1001", wBare: "1002"} {
		var ref model.CatalogExternalRef
		require.NoError(t, testDB.Where("entity_type = ? AND entity_id = ? AND source_id = ?",
			model.EntityTypeWork, workID, steamSrc).First(&ref).Error)
		assert.Equal(t, appid, ref.ExternalID)
		assert.Equal(t, model.LinkKindProbable, ref.LinkKind)
		assert.Equal(t, RuleBgmSteam, ref.MatchedBy)
	}
	for _, workID := range []int64{wTwo, wNone, wRej} {
		var n int64
		require.NoError(t, testDB.Model(&model.CatalogExternalRef{}).Where(
			"entity_type = ? AND entity_id = ? AND source_id = ?", model.EntityTypeWork, workID, steamSrc).Count(&n).Error)
		assert.Zero(t, n, "work %d", workID)
	}

	st, err = Run(ctx, Opts{DSN: testDSN, Only: LaneBgm, Apply: true})
	require.NoError(t, err)
	assert.Zero(t, st.BgmWritten)
	assert.Equal(t, 3, st.BgmHasSteam)
}

func TestUnknownLane(t *testing.T) {
	_, err := Run(context.Background(), Opts{DSN: "x", Only: "steam"})
	assert.Error(t, err)
	_, err = Run(context.Background(), Opts{DSN: "x", Only: LaneEG})
	assert.Error(t, err, "the eg lane needs --eg-dsn")
}
