package imagegradesync

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	imgmodel "api/internal/platform/image/model"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	catalogDSN string
	imagesDSN  string
	catalogDB  *gorm.DB
	imagesDB   *gorm.DB
	mediumID   int16
)

func TestMain(m *testing.M) {
	var ok bool
	catalogDSN, ok = dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/imagegradesync")
	}
	// REQUIRE_DB_TESTS deliberately does not reach TEST_IMAGES_DSN: the
	// integration job in test.yml runs a Postgres service and no images
	// database, so failing hard on it would turn that job permanently red.
	imagesDSN = os.Getenv("TEST_IMAGES_DSN")
	if imagesDSN == "" {
		fmt.Fprintln(os.Stderr, "SKIP: TEST_IMAGES_DSN is unset — jobs/imagegradesync not run")
		os.Exit(0)
	}
	open := func(dsn string) *gorm.DB {
		db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
		if err != nil {
			dbtest.SkipMainf("jobs/imagegradesync", "cannot connect to test database: %v", err)
		}
		return db
	}
	catalogDB = open(catalogDSN)
	imagesDB = open(imagesDSN)
	if err := migrate.Run(catalogDB); err != nil {
		dbtest.SkipMainf("jobs/imagegradesync", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(catalogDB); err != nil {
		dbtest.SkipMainf("jobs/imagegradesync", "catalog seed failed: %v", err)
	}
	if err := imagesDB.AutoMigrate(&imgmodel.Image{}); err != nil {
		dbtest.SkipMainf("jobs/imagegradesync", "images migrate failed: %v", err)
	}
	if err := catalogDB.Raw(`SELECT id FROM catalog_medium WHERE key = 'galgame'`).Scan(&mediumID).Error; err != nil || mediumID == 0 {
		dbtest.SkipMainf("jobs/imagegradesync", "galgame medium not seeded: %v", err)
	}
	os.Exit(m.Run())
}

func reset(t *testing.T) {
	t.Helper()
	for _, tbl := range []string{"catalog_work_screenshot", "catalog_work_cover", "catalog_work"} {
		require.NoError(t, catalogDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	require.NoError(t, imagesDB.Exec("TRUNCATE images RESTART IDENTITY").Error)
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, catalogDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "source %q must be seeded", key)
	return id
}

func mkWork(t *testing.T, name string) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: mediumID, OLang: "ja", DisplayName: name}
	require.NoError(t, catalogDB.Create(&w).Error)
	return w.ID
}

func hashOf(seedText string) string {
	return strings.Repeat("0", 64-len(seedText)) + seedText
}

func mkShot(t *testing.T, workID int64, hash, source string, sexual, violence int16) int64 {
	t.Helper()
	row := model.CatalogWorkScreenshot{
		WorkID: workID, ImageHash: hash, Sexual: sexual, Violence: violence, SourceID: sourceID(t, source),
	}
	require.NoError(t, catalogDB.Create(&row).Error)
	return row.ID
}

func mkCover(t *testing.T, workID int64, hash, source string, sexual, violence int16) int64 {
	t.Helper()
	row := model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, Kind: "main", Sexual: sexual, Violence: violence, SourceID: sourceID(t, source),
	}
	require.NoError(t, catalogDB.Create(&row).Error)
	return row.ID
}

// level < 0 stores an image row whose review_labels carry no grade at all.
func mkImage(t *testing.T, hash string, level int) {
	t.Helper()
	labels := `{"nsfw": {"provider": "omni"}}`
	if level >= 0 {
		labels = fmt.Sprintf(`{"grade": {"provider": "cloudflare-workers-ai", "model": "moondream", "level": %d, "answers": {"act": false, "nude": false, "underwear": false}}}`, level)
	}
	require.NoError(t, imagesDB.Exec(
		`INSERT INTO images (hash, storage_key, mime, ext, width, height, size_bytes, variants, review_labels)
		 VALUES (?, ?, 'image/webp', 'webp', 100, 100, 1000, '[]'::jsonb, ?::jsonb)`,
		hash, "k/"+hash, labels).Error)
}

func sexualOf(t *testing.T, table string, id int64) (int16, int16) {
	t.Helper()
	var row struct {
		Sexual   int16 `gorm:"column:sexual"`
		Violence int16 `gorm:"column:violence"`
	}
	require.NoError(t, catalogDB.Raw(`SELECT sexual, violence FROM `+table+` WHERE id = ?`, id).Scan(&row).Error)
	return row.Sexual, row.Violence
}

type fixture struct {
	downgrade   int64
	bangumiUp   int64
	unchanged   int64
	ungraded    int64
	missing     int64
	vndbRow     int64
	curatedRow  int64
	underwear   int64
	explicitAct int64
}

func seedFixture(t *testing.T) fixture {
	t.Helper()
	reset(t)
	w := mkWork(t, "fixture work")

	var f fixture
	// dlsite screenshot stamped 2 from the work's age rating, graded clean.
	mkImage(t, hashOf("a1"), 0)
	f.downgrade = mkShot(t, w, hashOf("a1"), "dlsite", 2, 1)
	// bangumi cover hardcoded 0, graded as an explicit act (level 3 -> 2).
	mkImage(t, hashOf("a2"), 3)
	f.bangumiUp = mkCover(t, w, hashOf("a2"), "bangumi", 0, 0)
	// getchu screenshot already agrees with its grade.
	mkImage(t, hashOf("a3"), 2)
	f.unchanged = mkShot(t, w, hashOf("a3"), "getchu", 2, 0)
	// present in the image service but never graded.
	mkImage(t, hashOf("a4"), -1)
	f.ungraded = mkShot(t, w, hashOf("a4"), "getchu", 2, 0)
	// dangling reference: no images row at all.
	f.missing = mkShot(t, w, hashOf("a5"), "dlsite", 2, 0)
	// human-authored lanes must never move.
	mkImage(t, hashOf("a6"), 0)
	f.vndbRow = mkShot(t, w, hashOf("a6"), "vndb", 2, 0)
	mkImage(t, hashOf("a7"), 0)
	f.curatedRow = mkCover(t, w, hashOf("a7"), "curated", 2, 0)
	// level 1 maps to 1.
	mkImage(t, hashOf("a8"), 1)
	f.underwear = mkShot(t, w, hashOf("a8"), "dlsite", 0, 0)
	// upscale is a machine source too, and gets the same treatment.
	mkImage(t, hashOf("a9"), 3)
	f.explicitAct = mkCover(t, w, hashOf("a9"), "upscale", 0, 0)
	return f
}

func TestSyncLeavesAGradeAPersonSet(t *testing.T) {
	reset(t)
	w := mkWork(t, "regraded by hand")
	mkImage(t, hashOf("b1"), 0)
	id := mkCover(t, w, hashOf("b1"), "bangumi", 2, 0)
	require.NoError(t, catalogDB.Exec(`UPDATE catalog_work_cover
		SET field_provenance = '{"sexual":[{"source":"curated","at":"2026-10-19T00:00:00Z"}]}' WHERE id = ?`, id).Error)

	st, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 0, st.Updated)
	sexual, _ := sexualOf(t, "catalog_work_cover", id)
	assert.Equal(t, int16(2), sexual, "a grade set through catalog.work.covers_upstream outranks the image grader")
}

func TestSyncDryRunForecastsWithoutWriting(t *testing.T) {
	f := seedFixture(t)
	st, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN})
	require.NoError(t, err)

	assert.Equal(t, 7, st.Scanned, "vndb + curated rows are out of scope")
	assert.Equal(t, 4, st.Planned)
	assert.Equal(t, 0, st.Updated)
	assert.Equal(t, 1, st.Unchanged)
	assert.Equal(t, 1, st.Ungraded)
	assert.Equal(t, 1, st.Missing)
	assert.Equal(t, 0, st.Errors)

	got, _ := sexualOf(t, "catalog_work_screenshot", f.downgrade)
	assert.Equal(t, int16(2), got, "dry-run must not write")
	got, _ = sexualOf(t, "catalog_work_cover", f.bangumiUp)
	assert.Equal(t, int16(0), got, "dry-run must not write")

	matrix := st.Matrix()
	assert.Contains(t, matrix, "dlsite")
	assert.Contains(t, matrix, "bangumi")
	assert.Contains(t, matrix, "upscale")
	t.Logf("dry-run forecast:\n%s\n%s", matrix, st.String())
}

func TestSyncApplyRefinesOnlyMachineSources(t *testing.T) {
	f := seedFixture(t)
	st, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 4, st.Planned)
	assert.Equal(t, 4, st.Updated)
	assert.Equal(t, 0, st.Errors)

	sexual, violence := sexualOf(t, "catalog_work_screenshot", f.downgrade)
	assert.Equal(t, int16(0), sexual, "a clean image must stop being blurred")
	assert.Equal(t, int16(1), violence, "violence is never touched")

	sexual, _ = sexualOf(t, "catalog_work_cover", f.bangumiUp)
	assert.Equal(t, int16(2), sexual, "level 3 maps to 2 and fixes the hardcoded 0")

	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.underwear)
	assert.Equal(t, int16(1), sexual)

	sexual, _ = sexualOf(t, "catalog_work_cover", f.explicitAct)
	assert.Equal(t, int16(2), sexual)

	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.unchanged)
	assert.Equal(t, int16(2), sexual)

	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.ungraded)
	assert.Equal(t, int16(2), sexual, "an ungraded image keeps the provisional stamp")

	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.missing)
	assert.Equal(t, int16(2), sexual, "a dangling hash keeps the provisional stamp")

	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.vndbRow)
	assert.Equal(t, int16(2), sexual, "vndb community votes are never overwritten")

	sexual, _ = sexualOf(t, "catalog_work_cover", f.curatedRow)
	assert.Equal(t, int16(2), sexual, "curated values are never overwritten")

	again, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Apply: true})
	require.NoError(t, err)
	assert.Equal(t, 0, again.Planned, "a second run is a no-op")
	assert.Equal(t, 5, again.Unchanged)
}

func TestSyncSourceFilterAndPaging(t *testing.T) {
	f := seedFixture(t)
	st, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Apply: true, Source: "bangumi", Batch: 1})
	require.NoError(t, err)
	assert.Equal(t, 1, st.Scanned)
	assert.Equal(t, 1, st.Updated)

	sexual, _ := sexualOf(t, "catalog_work_cover", f.bangumiUp)
	assert.Equal(t, int16(2), sexual)
	sexual, _ = sexualOf(t, "catalog_work_screenshot", f.downgrade)
	assert.Equal(t, int16(2), sexual, "the dlsite lane was out of the filtered scope")

	_, err = Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Source: "vndb"})
	require.Error(t, err)
}

func TestSyncPagesThroughEveryRow(t *testing.T) {
	seedFixture(t)
	st, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Batch: 1})
	require.NoError(t, err)
	assert.Equal(t, 7, st.Scanned)
	assert.Equal(t, 4, st.Planned)

	limited, err := Run(context.Background(), Opts{DSN: catalogDSN, ImagesDSN: imagesDSN, Batch: 1, Limit: 3})
	require.NoError(t, err)
	assert.Equal(t, 3, limited.Scanned)
}
