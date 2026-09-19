package vndbtitles

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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				testDSN = dsn
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
		"catalog_work_title", "catalog_external_ref", "catalog_work",
		"src_vndb.vn_titles", "src_vndb.vn",
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

func mkWork(t *testing.T, olang, display string, status int16, prov string) int64 {
	t.Helper()
	if prov == "" {
		prov = "{}"
	}
	var id int64
	require.NoError(t, testDB.Raw(`
		INSERT INTO catalog_work (medium_id, olang, display_name, content_rating, status, extra, field_provenance, display_nsfw)
		VALUES (?, ?, ?, 0, ?, '{}', ?::jsonb, false)
		RETURNING id`, mediumID(t), olang, display, status, prov).Scan(&id).Error)
	return id
}

func mkRef(t *testing.T, workID int64, source int16, vid string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: vid, LinkKind: kind, MatchedBy: "rule:test",
	}).Error)
}

func mkDeadRef(t *testing.T, workID int64, source int16, vid string) {
	t.Helper()
	dead := time.Now()
	require.NoError(t, testDB.Create(&model.CatalogExternalRef{
		EntityType: model.EntityTypeWork, EntityID: workID, SourceID: source,
		ExternalID: vid, LinkKind: model.LinkKindExact, MatchedBy: "rule:test", DeadAt: &dead,
	}).Error)
}

func mkTitle(t *testing.T, workID int64, lang, title string, kind int16) {
	t.Helper()
	require.NoError(t, testDB.Exec(`
		INSERT INTO catalog_work_title (work_id, lang, title, kind, provenance)
		VALUES (?, ?, ?, ?, ?)`,
		workID, lang, title, kind, model.WorkTitleProvenanceSource).Error)
}

func insVN(t *testing.T, id, olang string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.vn
		(id, olang, image, c_image, description, c_votecount, c_lengthnum, length, devstatus, alias, ingested_at)
		VALUES (?,?,'','','',0,0,0,0,'',now())`, id, olang).Error)
}

func insVNTitle(t *testing.T, id, lang string, official bool, title string) {
	t.Helper()
	require.NoError(t, testDB.Exec(`INSERT INTO src_vndb.vn_titles (id, lang, official, title, latin)
		VALUES (?,?,?,?, '')`, id, lang, official, title).Error)
}

func titleCount(t *testing.T, workID int64) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_work_title WHERE work_id = ?`, workID).Scan(&n).Error)
	return n
}

func displayOf(t *testing.T, workID int64) string {
	t.Helper()
	var name string
	require.NoError(t, testDB.Raw(`SELECT display_name FROM catalog_work WHERE id = ?`, workID).Scan(&name).Error)
	return name
}

func hasTitleRow(t *testing.T, workID int64, lang, title string, kind, provenance int16) {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`
		SELECT count(*) FROM catalog_work_title
		WHERE work_id = ? AND lang = ? AND title = ? AND kind = ? AND provenance = ?`,
		workID, lang, title, kind, provenance).Scan(&n).Error)
	assert.Equal(t, int64(1), n, "work %d title %s/%q kind=%d", workID, lang, title, kind)
}

func revisionCount(t *testing.T) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_revision`).Scan(&n).Error)
	return n
}

func TestBackfillVNDBWorkTitles(t *testing.T) {
	requireDB(t)
	vndb := sourceID(t, "vndb")
	humanProv := `{"display_name":[{"source":"curated","at":"2026-07-06T00:00:00Z"}]}`

	insVN(t, "vFill", "ru")
	insVNTitle(t, "vFill", "ru", true, "Сердце")
	insVNTitle(t, "vFill", "en", true, "Heart")
	wFill := mkWork(t, "ja", "", model.WorkStatusLive, "{}")
	mkRef(t, wFill, vndb, "vFill", model.LinkKindExact)

	insVN(t, "vPresent", "ko")
	insVNTitle(t, "vPresent", "ko", true, "마음")
	insVNTitle(t, "vPresent", "en", true, "Mind")
	wPresent := mkWork(t, "ko", "kept-name", model.WorkStatusLive, "{}")
	mkRef(t, wPresent, vndb, "vPresent", model.LinkKindExact)
	mkTitle(t, wPresent, "ko", "마음", model.WorkTitleKindAlias)

	insVN(t, "vHuman", "ja")
	insVNTitle(t, "vHuman", "ja", true, "こころ")
	insVNTitle(t, "vHuman", "en", true, "Kokoro")
	wHuman := mkWork(t, "ja", "", model.WorkStatusLive, humanProv)
	mkRef(t, wHuman, vndb, "vHuman", model.LinkKindExact)
	mkTitle(t, wHuman, "ja", "こころ", model.WorkTitleKindOfficial)
	mkTitle(t, wHuman, "en", "Kokoro", model.WorkTitleKindOfficial)

	insVN(t, "vMultiA", "es")
	insVNTitle(t, "vMultiA", "es", true, "Corazón")
	insVN(t, "vMultiB", "es")
	insVNTitle(t, "vMultiB", "es", true, "Otro")
	wMulti := mkWork(t, "es", "", model.WorkStatusLive, "{}")
	mkRef(t, wMulti, vndb, "vMultiA", model.LinkKindExact)
	mkRef(t, wMulti, vndb, "vMultiB", model.LinkKindExact)

	wMissing := mkWork(t, "fr", "", model.WorkStatusLive, "{}")
	mkRef(t, wMissing, vndb, "vGone", model.LinkKindExact)

	insVN(t, "vMerged", "pt-br")
	insVNTitle(t, "vMerged", "pt-br", true, "Coração")
	wMerged := mkWork(t, "pt-br", "", model.WorkStatusMerged, "{}")
	mkRef(t, wMerged, vndb, "vMerged", model.LinkKindExact)

	insVN(t, "vDead", "uk")
	insVNTitle(t, "vDead", "uk", true, "Серце")
	wDead := mkWork(t, "uk", "", model.WorkStatusLive, "{}")
	mkDeadRef(t, wDead, vndb, "vDead")

	insVN(t, "vMislabel", "ru")
	insVNTitle(t, "vMislabel", "ru", true, "Метаморфоз")
	insVNTitle(t, "vMislabel", "en", true, "Metamorphosis")
	wMislabel := mkWork(t, "ru", "Metamorphosis", model.WorkStatusLive, "{}")
	mkRef(t, wMislabel, vndb, "vMislabel", model.LinkKindExact)
	mkTitle(t, wMislabel, "en", "Метаморфоз", model.WorkTitleKindOfficial)
	mkTitle(t, wMislabel, "", "Metamorphosis", model.WorkTitleKindOfficial)

	insVN(t, "vUnofficial", "ja")
	insVNTitle(t, "vUnofficial", "ja", true, "ひみつ")
	insVNTitle(t, "vUnofficial", "en", false, "Secret")
	wUnofficial := mkWork(t, "ja", "ひみつ", model.WorkStatusLive, "{}")
	mkRef(t, wUnofficial, vndb, "vUnofficial", model.LinkKindExact)
	mkTitle(t, wUnofficial, "ja", "ひみつ", model.WorkTitleKindOfficial)

	ctx := context.Background()
	revsBefore := revisionCount(t)

	dry, err := Run(ctx, Opts{DSN: testDSN})
	require.NoError(t, err)
	assertPlan(t, dry)
	assert.Equal(t, "", displayOf(t, wFill))
	assert.Zero(t, titleCount(t, wFill))
	assert.Equal(t, int64(1), titleCount(t, wPresent))
	assert.Equal(t, "kept-name", displayOf(t, wPresent))
	assert.Equal(t, "", displayOf(t, wHuman))
	assert.Zero(t, titleCount(t, wMulti))
	assert.Zero(t, titleCount(t, wMissing))
	assert.Zero(t, titleCount(t, wMerged))
	assert.Zero(t, titleCount(t, wDead))
	assert.Equal(t, revsBefore, revisionCount(t))

	apply, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assertPlan(t, apply)
	assert.Zero(t, apply.TitlesLost)
	assert.Zero(t, apply.DisplayLost)
	assert.Zero(t, apply.Errors)

	assert.Equal(t, int64(2), titleCount(t, wFill))
	hasTitleRow(t, wFill, "ru", "Сердце", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	hasTitleRow(t, wFill, "en", "Heart", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	assert.Equal(t, "Сердце", displayOf(t, wFill))

	assert.Equal(t, int64(2), titleCount(t, wPresent))
	hasTitleRow(t, wPresent, "ko", "마음", model.WorkTitleKindAlias, model.WorkTitleProvenanceSource)
	hasTitleRow(t, wPresent, "en", "Mind", model.WorkTitleKindOfficial, model.WorkTitleProvenanceSource)
	assert.Equal(t, "kept-name", displayOf(t, wPresent))

	assert.Equal(t, "", displayOf(t, wHuman))
	assert.Equal(t, int64(2), titleCount(t, wHuman))
	assert.Zero(t, titleCount(t, wMulti))
	assert.Equal(t, "", displayOf(t, wMulti))
	assert.Zero(t, titleCount(t, wMissing))
	assert.Zero(t, titleCount(t, wMerged))
	assert.Equal(t, "", displayOf(t, wMerged))
	assert.Zero(t, titleCount(t, wDead))
	assert.Equal(t, "", displayOf(t, wDead))
	assert.Equal(t, int64(2), titleCount(t, wMislabel), "a title filed under another language is not added again")
	assert.Equal(t, int64(1), titleCount(t, wUnofficial), "an unofficial English title is not a candidate")
	assert.Equal(t, revsBefore, revisionCount(t))

	again, err := Run(ctx, Opts{DSN: testDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 5, again.WorksPopulation)
	assert.Equal(t, 1, again.WorksMultiAnchor)
	assert.Equal(t, 1, again.WorksVNMissing)
	assert.Zero(t, again.WorksChanged)
	assert.Zero(t, again.TitlesOLangInserted)
	assert.Zero(t, again.TitlesENInserted)
	assert.Equal(t, 9, again.TitlesPresent)
	assert.Zero(t, again.TitlesLost)
	assert.Zero(t, again.DisplayFilled)
	assert.Equal(t, 1, again.DisplayHumanSkipped)
	assert.Zero(t, again.DisplayLost)
	assert.Zero(t, again.Errors)
	assert.Equal(t, int64(2), titleCount(t, wFill))
	assert.Equal(t, int64(2), titleCount(t, wPresent))
	assert.Equal(t, revsBefore, revisionCount(t))
}

func assertPlan(t *testing.T, st *Stats) {
	t.Helper()
	assert.Equal(t, 5, st.WorksPopulation)
	assert.Equal(t, 1, st.WorksMultiAnchor)
	assert.Equal(t, 1, st.WorksVNMissing)
	assert.Equal(t, 2, st.WorksChanged)
	assert.Equal(t, 1, st.TitlesOLangInserted)
	assert.Equal(t, 2, st.TitlesENInserted)
	assert.Equal(t, 6, st.TitlesPresent)
	assert.Zero(t, st.TitlesLost)
	assert.Equal(t, 1, st.DisplayFilled)
	assert.Equal(t, 1, st.DisplayHumanSkipped)
	assert.Zero(t, st.DisplayLost)
	assert.Zero(t, st.Errors)
}

func TestWriteCountsLostWhenTheRowChangedAfterTheRead(t *testing.T) {
	requireDB(t)
	w := mkWork(t, "ja", "taken", model.WorkStatusLive, "{}")
	mkTitle(t, w, "ja", "先取り", model.WorkTitleKindOfficial)
	stale := []preparedWork{{
		row:  loadedWork{WorkID: w, OLang: "ja", OLangTitle: "先取り"},
		plan: decision{InsertOLang: true, FillDisplay: true},
	}}
	var d writeDelta
	require.NoError(t, testDB.Transaction(func(tx *gorm.DB) error {
		var err error
		d, err = writeChunkTx(tx, stale)
		return err
	}))
	assert.Equal(t, 1, d.titlesLost, "the insert found the row already there")
	assert.Equal(t, 1, d.displayLost, "the display_name was no longer empty")
	assert.Zero(t, d.changed)
	assert.Equal(t, "taken", displayOf(t, w))
}
