package workseries

import (
	"context"
	"os"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/srcvndb"
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
	dlTestDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/workseries")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/workseries", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/workseries", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/workseries", "catalog seed failed: %v", err)
	}
	if err := srcvndb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/workseries", "src_vndb schema failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS workseries_dl`,
		`CREATE TABLE IF NOT EXISTS workseries_dl.works (workno text PRIMARY KEY, product_json jsonb)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/workseries", "mirror fixture failed: %v", err)
		}
	}
	dlTestDSN = testDSN + " options='-csearch_path=workseries_dl'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_series_member", "catalog_series", "catalog_external_ref",
		"catalog_release", "catalog_work", "workseries_dl.works", "src_vndb.releases_vn",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func mediumID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func mkAnchoredWork(t *testing.T, medium int16, name, workno string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name}
	require.NoError(t, testDB.Create(&w).Error)
	rel := model.CatalogRelease{WorkID: w.ID, Kind: model.ReleaseKindDigital}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: 4,
		ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
	return w.ID
}

func mkMirrorWork(t *testing.T, workno, seriesID, seriesName string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO workseries_dl.works (workno, product_json)
		VALUES (?, jsonb_build_object('series_id', ?::text, 'series_name', ?::text))`, workno, seriesID, seriesName).Error)
}

func TestImportWorkSeries(t *testing.T) {
	clean(t)
	medium := mediumID(t)

	wA := mkAnchoredWork(t, medium, "s1-a", "RJ100")
	wB := mkAnchoredWork(t, medium, "s1-b", "RJ101")
	wC := mkAnchoredWork(t, medium, "solo", "RJ200")
	mkAnchoredWork(t, medium, "noseries", "RJ300")
	mkMirrorWork(t, "RJ100", "SRI001", "テスト系列")
	mkMirrorWork(t, "RJ101", "SRI001", "テスト系列")
	mkMirrorWork(t, "RJ200", "SRI002", "一人系列")
	mkMirrorWork(t, "RJ300", "", "")

	ctx := context.Background()
	opts := Opts{DSN: testDSN, DlsiteDSN: dlTestDSN}

	st, err := Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 4, st.AnchoredWorks)
	assert.Equal(t, 1, st.SeriesEligible, "single-member series gated out")
	assert.Equal(t, 2, st.MembersWanted)
	assert.Equal(t, 1, st.SeriesCreated)
	assert.Equal(t, 2, st.MembersAdded)
	var n int64
	require.NoError(t, testDB.Table("catalog_series").Count(&n).Error)
	assert.Zero(t, n, "dry run must not write")

	opts.Apply = true
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st.SeriesCreated)
	assert.Equal(t, 2, st.MembersAdded)
	assert.Zero(t, st.Errors)
	var se model.CatalogSeries
	require.NoError(t, testDB.Where("external_id = 'SRI001'").First(&se).Error)
	assert.Equal(t, "テスト系列", se.DisplayName)
	require.NoError(t, testDB.Table("catalog_series_member").Where("series_id = ?", se.ID).Count(&n).Error)
	assert.EqualValues(t, 2, n)

	var positions []int16
	require.NoError(t, testDB.Table("catalog_series_member").Where("series_id = ?", se.ID).
		Order("position").Pluck("position", &positions).Error)
	assert.Equal(t, []int16{1, 2}, positions)
	assert.Equal(t, 2, st.OrderChanged)

	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Zero(t, st.SeriesCreated+st.SeriesRenamed+st.SeriesDeleted+st.MembersAdded+st.MembersStale)
	assert.Zero(t, st.OrderChanged)

	require.NoError(t, testDB.Exec(`UPDATE workseries_dl.works SET product_json =
		jsonb_set(product_json, '{series_name}', '"新名前"') WHERE workno = 'RJ100'`).Error)
	require.NoError(t, testDB.Exec(`UPDATE workseries_dl.works SET product_json =
		jsonb_build_object('series_id', '', 'series_name', '') WHERE workno = 'RJ101'`).Error)
	require.NoError(t, testDB.Exec(`UPDATE workseries_dl.works SET product_json =
		jsonb_build_object('series_id', 'SRI001', 'series_name', '新名前') WHERE workno = 'RJ200'`).Error)
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st.SeriesRenamed)
	assert.Equal(t, 1, st.MembersAdded, "wC joins")
	assert.Equal(t, 1, st.MembersStale, "wB leaves")
	se = model.CatalogSeries{}
	require.NoError(t, testDB.Where("external_id = 'SRI001'").First(&se).Error)
	assert.Equal(t, "新名前", se.DisplayName)
	var members []int64
	require.NoError(t, testDB.Table("catalog_series_member").Where("series_id = ?", se.ID).Order("work_id").Pluck("work_id", &members).Error)
	assert.Equal(t, []int64{wA, wC}, members)
	_ = wB

	require.NoError(t, testDB.Exec(`UPDATE workseries_dl.works SET product_json =
		jsonb_build_object('series_id', '', 'series_name', '') WHERE workno = 'RJ200'`).Error)
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 1, st.SeriesDeleted)
	require.NoError(t, testDB.Table("catalog_series").Count(&n).Error)
	assert.Zero(t, n)
	require.NoError(t, testDB.Table("catalog_series_member").Count(&n).Error)
	assert.Zero(t, n, "members cascade with the series")
}

func TestBundleProductJoinsNoSeries(t *testing.T) {
	clean(t)
	medium := mediumID(t)
	mkAnchoredWork(t, medium, "s-a", "RJ400")
	mkAnchoredWork(t, medium, "s-b", "RJ401")
	bundled := mkAnchoredWork(t, medium, "bundled", "RJ402")
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		SELECT 6, r.entity_id, (SELECT id FROM catalog_source WHERE key = 'vndb'), 'r402', 0, 'rule:test'
		FROM catalog_external_ref r WHERE r.source_id = 4 AND r.external_id = 'RJ402'`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_vn (id, vid, rtype) VALUES ('r402','v1','complete'),('r402','v2','complete')`).Error)
	for _, wn := range []string{"RJ400", "RJ401", "RJ402"} {
		mkMirrorWork(t, wn, "SRI004", "パック系列")
	}

	st, err := Run(context.Background(), Opts{DSN: testDSN, DlsiteDSN: dlTestDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.SkippedBundle)
	assert.Equal(t, 2, st.AnchoredWorks)
	assert.Equal(t, 2, st.MembersAdded)
	var n int64
	require.NoError(t, testDB.Table("catalog_series_member").Where("work_id = ?", bundled).Count(&n).Error)
	assert.Zero(t, n, "a work reached only through a bundle product is not a series member")
}
