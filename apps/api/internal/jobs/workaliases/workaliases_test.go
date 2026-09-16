package workaliases

import (
	"context"
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
		dbtest.SkipMain("jobs/workaliases")
	}
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/workaliases", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/workaliases", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/workaliases", "catalog seed failed: %v", err)
	}
	if err := srcb.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/workaliases", "src_bangumi schema failed: %v", err)
	}
	if err := srcv.EnsureSchema(db); err != nil {
		dbtest.SkipMainf("jobs/workaliases", "src_vndb schema failed: %v", err)
	}
	for _, ddl := range []string{
		`CREATE SCHEMA IF NOT EXISTS workaliases_dl`,
		`CREATE TABLE IF NOT EXISTS workaliases_dl.works (workno text PRIMARY KEY, product_json jsonb, info_json jsonb)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			dbtest.SkipMainf("jobs/workaliases", "mirror fixture failed: %v", err)
		}
	}
	dlTestDSN = testDSN + " options='-csearch_path=workaliases_dl'"
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"catalog_work_title", "catalog_external_ref", "catalog_release", "catalog_work",
		"src_bangumi.subject", "workaliases_dl.works", "src_vndb.releases_vn",
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

func mkWork(t *testing.T, medium int16, name string, site *string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: medium, OLang: "ja", DisplayName: name, Site: site}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func mkTitle(t *testing.T, workID int64, lang, title string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
		VALUES (?, ?, ?, ?, 0)`, workID, lang, title, kind).Error)
}

func mkBgmAnchor(t *testing.T, workID int64, subjectID string) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: 3,
		ExternalID: subjectID, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
}

func mkSubject(t *testing.T, id int64, infobox string) {
	t.Helper()
	sub := srcb.Subject{
		ID: id, Type: 4, Name: fmt.Sprintf("subject-%d", id), NameCN: "",
		InfoboxRaw: "", ParseError: "", Summary: "", Date: "",
		ParserVersion: srcb.ParserVersion, IngestedAt: time.Now(),
	}
	if infobox != "" {
		sub.InfoboxParsed = []byte(infobox)
	}
	require.NoError(t, testDB.Create(&sub).Error)
}

func mkDlsiteRelAnchor(t *testing.T, workID int64, workno string) int64 {
	t.Helper()
	rel := model.CatalogRelease{WorkID: workID, Kind: model.ReleaseKindDigital}
	require.NoError(t, testDB.Create(&rel).Error)
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeRelease, EntityID: rel.ID, SourceID: 4,
		ExternalID: workno, LinkKind: model.LinkKindExact, MatchedBy: "rule:test",
	}).Error)
	return rel.ID
}

func strPtr(s string) *string { return &s }

func TestImportWorkAliases(t *testing.T) {
	clean(t)
	medium := mediumID(t)

	wA := mkWork(t, medium, "本体A", nil)
	mkTitle(t, wA, "ja", "オフィシャルA", 0)
	mkBgmAnchor(t, wA, "1001")
	mkSubject(t, 1001, `{"Fields":[{"Key":"别名","Array":true,"Value":"","Items":[{"Value":"アリアスA1"},{"Value":"オフィシャルA"},{"Value":"  "}]}]}`)

	wB := mkWork(t, medium, "本体B", nil)
	mkBgmAnchor(t, wB, "1002")
	mkSubject(t, 1002, `{"Fields":[{"Key":"别名","Array":false,"Value":"别名B","Items":null},{"Key":"平台","Array":false,"Value":"PC","Items":null}]}`)

	wClaimed := mkWork(t, medium, "认领作品", strPtr("galgame_wiki"))
	mkBgmAnchor(t, wClaimed, "1003")
	mkSubject(t, 1003, `{"Fields":[{"Key":"别名","Array":false,"Value":"クレイム別名","Items":null}]}`)

	wK := mkWork(t, medium, "カナ待ち", nil)
	mkDlsiteRelAnchor(t, wK, "RJ900001")
	wHasKana := mkWork(t, medium, "カナ持ち", nil)
	mkTitle(t, wHasKana, "ja", "スデニカナ", 3)
	mkDlsiteRelAnchor(t, wHasKana, "RJ900002")
	wNoKana := mkWork(t, medium, "カナ無し", nil)
	mkDlsiteRelAnchor(t, wNoKana, "RJ900003")
	require.NoError(t, testDB.Exec(`INSERT INTO workaliases_dl.works (workno, product_json) VALUES
		('RJ900001', '{"work_name_kana": "カナマチ"}'),
		('RJ900002', '{"work_name_kana": "モウアル"}'),
		('RJ900003', '{}')`).Error)

	ctx := context.Background()
	opts := Opts{DSN: testDSN, DlsiteDSN: dlTestDSN, Source: "all"}

	st, err := Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 2, st.BgmWorks, "claimed excluded")
	assert.Equal(t, 2, st.BgmPlanned, "アリアスA1 + 别名B")
	assert.Equal(t, 1, st.BgmSkippedDup, "official-colliding alias skipped")
	assert.Equal(t, 2, st.KanaWorks, "wK + wNoKana (wHasKana not a candidate)")
	assert.Equal(t, 1, st.KanaNoKana)
	assert.Equal(t, 1, st.KanaPlanned)
	var n int64
	require.NoError(t, testDB.Table("catalog_work_title").Where("kind = 1").Count(&n).Error)
	assert.Zero(t, n, "dry run must not write")

	opts.Apply = true
	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Equal(t, 2, st.BgmWritten)
	assert.Equal(t, 1, st.KanaWritten)
	assert.Zero(t, st.Errors)

	var titles []model.CatalogWorkTitle
	require.NoError(t, testDB.Where("work_id = ? AND kind = 1", wA).Find(&titles).Error)
	require.Len(t, titles, 1)
	assert.Equal(t, "アリアスA1", titles[0].Title)
	assert.Equal(t, "", titles[0].Lang)
	require.NoError(t, testDB.Where("work_id = ? AND kind = 3", wK).Find(&titles).Error)
	require.Len(t, titles, 1)
	assert.Equal(t, "カナマチ", titles[0].Title)
	assert.Equal(t, "ja", titles[0].Lang)
	require.NoError(t, testDB.Table("catalog_work_title").Where("work_id = ?", wClaimed).Count(&n).Error)
	assert.Zero(t, n)

	st, err = Run(ctx, opts)
	require.NoError(t, err)
	assert.Zero(t, st.BgmWritten+st.KanaWritten, "idempotent re-run")
	assert.Equal(t, 3, st.BgmSkippedDup, "landed aliases now dedup-skipped")
}

func TestBundleProductLendsNoKana(t *testing.T) {
	clean(t)
	medium := mediumID(t)
	bundleOnly := mkWork(t, medium, "同梱のみ", nil)
	rel := mkDlsiteRelAnchor(t, bundleOnly, "RJ910001")
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_external_ref (entity_type, entity_id, source_id, external_id, link_kind, matched_by)
		VALUES (6, ?, (SELECT id FROM catalog_source WHERE key = 'vndb'), 'r910001', 0, 'rule:test')`, rel).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.releases_vn (id, vid, rtype) VALUES ('r910001','v1','complete'),('r910001','v2','complete')`).Error)
	single := mkWork(t, medium, "単品", nil)
	mkDlsiteRelAnchor(t, single, "RJ910002")
	require.NoError(t, testDB.Exec(`INSERT INTO workaliases_dl.works (workno, product_json) VALUES
		('RJ910001', '{"work_name_kana": "パックノナマエ"}'),
		('RJ910002', '{"work_name_kana": "タンピン"}')`).Error)

	st, err := Run(context.Background(), Opts{DSN: testDSN, DlsiteDSN: dlTestDSN, Source: "dlsite-kana", Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 1, st.KanaBundle)
	assert.Equal(t, 1, st.KanaWritten)
	var n int64
	require.NoError(t, testDB.Table("catalog_work_title").Where("work_id = ?", bundleOnly).Count(&n).Error)
	assert.Zero(t, n, "a bundle's kana is the package's name, not the work's")
}
