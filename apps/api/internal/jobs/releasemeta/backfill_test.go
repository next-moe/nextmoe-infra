package releasemeta

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	srcb "api/internal/platform/catalog/srcbangumi"
	srcv "api/internal/platform/catalog/srcvndb"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB    *gorm.DB
	testDSN   string
	dlTestDSN string
	egTestDSN string
	gcTestDSN string
)

func TestMain(m *testing.M) {
	var ok bool
	testDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/releasemeta")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/releasemeta", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/releasemeta", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/releasemeta", "catalog seed failed: %v", err)
	}
	if err := srcb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/releasemeta", "src_bangumi schema failed: %v", err)
	}
	if err := srcv.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/releasemeta", "src_vndb schema failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS releasemeta_dl`,
		`CREATE TABLE IF NOT EXISTS releasemeta_dl.works (workno text PRIMARY KEY, regist_date timestamptz, age_category text, product_json jsonb)`,
		`ALTER TABLE releasemeta_dl.works ADD COLUMN IF NOT EXISTS product_json jsonb`,
		`CREATE SCHEMA IF NOT EXISTS releasemeta_eg`,
		`CREATE TABLE IF NOT EXISTS releasemeta_eg.games (id int PRIMARY KEY, sellday text)`,
		`ALTER TABLE releasemeta_eg.games ADD COLUMN IF NOT EXISTS erogame boolean`,
		`CREATE SCHEMA IF NOT EXISTS releasemeta_gc`,
		`CREATE TABLE IF NOT EXISTS releasemeta_gc.items (getchu_id text PRIMARY KEY, release_date text)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/releasemeta", "fixture schema failed: %v", err)
		}
	}
	dlTestDSN = testDSN + " options='-csearch_path=releasemeta_dl'"
	egTestDSN = testDSN + " options='-csearch_path=releasemeta_eg'"
	gcTestDSN = testDSN + " options='-csearch_path=releasemeta_gc'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_external_ref", "catalog_release", "catalog_work", "src_bangumi.subject",
		"src_vndb.releases", "src_vndb.releases_vn",
		"releasemeta_dl.works", "releasemeta_eg.games", "releasemeta_gc.items",
	} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY CASCADE").Error)
	}
}

func galgameMedium(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&id).Error)
	return id
}

func mkWork(t *testing.T, medium int16, name string, site *string, productID *int64, rating int16) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name,
		Site: site, ProductWorkID: productID, ContentRating: rating}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkRelease(t *testing.T, workID int64, y, m, d int16) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDigital}
	if y != 0 {
		rel.ReleasedY, rel.ReleasedM, rel.ReleasedD = &y, &m, &d
	}
	require.NoError(t, testDB.Create(&rel).Error)
	return rel.ID
}

func mkWorkAnchor(t *testing.T, workID int64, externalID string, source, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: externalID, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
}

func mkReleaseAnchor(t *testing.T, releaseID int64, workno string, source int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: releaseID, SourceID: source,
		ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
}

func mkSubject(t *testing.T, id int64, date string, nsfw bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcb.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id),
		Date: date, NSFW: nsfw,
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}).Error)
}

func mkSubjectMeta(t *testing.T, id int64, nsfw bool, meta ...string) {
	t.Helper()
	tags, err := json.Marshal(meta)
	require.NoError(t, err)
	require.NoError(t, testDB.Create(&srcb.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id),
		NSFW: nsfw, MetaTags: datatypes.JSON(tags),
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}).Error)
}

func mkReleaseYMD(t *testing.T, workID int64, y, m, d *int16) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDigital,
		ReleasedY: y, ReleasedM: m, ReleasedD: d}
	require.NoError(t, testDB.Create(&rel).Error)
	return rel.ID
}

func mkVndbRelease(t *testing.T, vid, rid string, minage *int16, hasEro, patch bool) {
	mkVndbReleaseAt(t, vid, rid, 0, minage, hasEro, patch)
}

func mkVndbReleaseAt(t *testing.T, vid, rid string, released int, minage *int16, hasEro, patch bool) {
	t.Helper()
	require.NoError(t, testDB.Create(&srcv.Release{
		ID: rid, OLang: "ja", Released: released, MinAge: minage, HasEro: hasEro, Patch: patch,
	}).Error)
	require.NoError(t, testDB.Create(&srcv.ReleaseVN{ID: rid, VID: vid, RType: "complete"}).Error)
}

func mkDlWork(t *testing.T, workno, regist, age string) {
	mkDlWorkFull(t, workno, "", regist, age)
}

func mkDlWorkFull(t *testing.T, workno, apiRegist, columnRegist, age string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO releasemeta_dl.works (workno, regist_date, age_category, product_json)
		 VALUES (?, NULLIF(?, '')::timestamptz, NULLIF(?, ''),
		         CASE WHEN ? = '' THEN NULL ELSE jsonb_build_object('regist_date', ?::text) END)`,
		workno, columnRegist, age, apiRegist, apiRegist).Error)
}

func mkGcItem(t *testing.T, id, releaseDate string) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO releasemeta_gc.items (getchu_id, release_date) VALUES (?, ?)`, id, releaseDate).Error)
}

func mkEgGame(t *testing.T, id int64, sellday string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO releasemeta_eg.games (id, sellday) VALUES (?, ?)`, id, sellday).Error)
}

func mkEgErogame(t *testing.T, id int64, erogame bool) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO releasemeta_eg.games (id, sellday, erogame) VALUES (?, '', ?)`, id, erogame).Error)
}

func relDate(t *testing.T, id int64) (y, m, d *int16) {
	t.Helper()
	var rel model.CatalogRelease
	require.NoError(t, testDB.Unscoped().First(&rel, id).Error)
	return rel.ReleasedY, rel.ReleasedM, rel.ReleasedD
}

func workRating(t *testing.T, id int64) int16 {
	t.Helper()
	var w model.CatalogWork
	require.NoError(t, testDB.First(&w, id).Error)
	return w.ContentRating
}

func assertDate(t *testing.T, relID int64, y, m, d int16) {
	t.Helper()
	gy, gm, gd := relDate(t, relID)
	require.NotNil(t, gy)
	assert.Equal(t, y, *gy)
	if m == 0 {
		assert.Nil(t, gm, "month should stay NULL")
	} else {
		require.NotNil(t, gm)
		assert.Equal(t, m, *gm)
	}
	if d == 0 {
		assert.Nil(t, gd, "day should stay NULL")
	} else {
		require.NotNil(t, gd)
		assert.Equal(t, d, *gd)
	}
}

func assertNoDate(t *testing.T, relID int64) {
	t.Helper()
	gy, gm, gd := relDate(t, relID)
	assert.Nil(t, gy)
	assert.Nil(t, gm)
	assert.Nil(t, gd)
}

func runOpts(apply bool) Opts {
	return Opts{Apply: apply, DSN: testDSN, DlsiteDSN: dlTestDSN, EGDSN: egTestDSN, GetchuDSN: gcTestDSN}
}

func str(s string) *string { return &s }
func i64(v int64) *int64   { return &v }
func pi16(v int16) *int16  { return &v }

func mkDateWork(t *testing.T, medium int16, name string) int64 {
	t.Helper()
	return mkWork(t, medium, name, nil, nil, model.ContentRatingSensitive)
}
