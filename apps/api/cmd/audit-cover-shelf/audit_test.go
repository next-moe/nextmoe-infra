package main

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

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/audit-cover-shelf")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/audit-cover-shelf", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("cmd/audit-cover-shelf", "catalog migration failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("cmd/audit-cover-shelf", "catalog seeding failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_work_cover, catalog_work RESTART IDENTITY CASCADE`).Error)
}

func sourceID(t *testing.T, key string) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = ?`, key).Scan(&id).Error)
	require.NotZero(t, id, "catalog_source has no %q row", key)
	return id
}

type work struct {
	name    string
	rating  int16
	shelf   bool // display_nsfw
	claimed bool
	status  int16
}

var nextProductWorkID int64

func seedWork(t *testing.T, w work) int64 {
	t.Helper()
	row := model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: w.name,
		ContentRating: w.rating, Status: w.status, DisplayNSFW: w.shelf,
	}
	if w.claimed {
		nextProductWorkID++
		site, product := "kungal", nextProductWorkID
		row.Site, row.ProductWorkID = &site, &product
	}
	require.NoError(t, testDB.Create(&row).Error)
	return row.ID
}

func seedCover(t *testing.T, workID int64, source, kind, hash string, sexual int16) {
	t.Helper()
	require.NoError(t, testDB.Create(&model.CatalogWorkCover{
		WorkID: workID, ImageHash: hash, Kind: kind, Sexual: sexual, SourceID: sourceID(t, source),
	}).Error)
}

// withoutTheTrigger writes cover rows with the derived-column triggers off,
// which is the only way to manufacture the drift this job exists to catch.
func withoutTheTrigger(t *testing.T, write func()) {
	t.Helper()
	for _, on := range []string{"DISABLE", "ENABLE"} {
		if on == "ENABLE" {
			write()
		}
		require.NoError(t, testDB.Exec(
			`ALTER TABLE catalog_work_cover `+on+` TRIGGER USER`).Error)
	}
}

func storedGrade(t *testing.T, id int64) bool {
	t.Helper()
	var got bool
	require.NoError(t, testDB.Raw(
		`SELECT cover_art_all_explicit FROM catalog_work WHERE id = ?`, id).Scan(&got).Error)
	return got
}

func ids(f []finding) []int64 {
	out := make([]int64, len(f))
	for i := range f {
		out[i] = f[i].ID
	}
	return out
}

// The state this job used to repair — a claimed work on the SFW shelf with no
// safe cover to elect — is now unreachable, and this is the pin that says so.
// Restoring the old query would make it fail.
func TestTheUnrenderableSFWShelfIsGoneByConstruction(t *testing.T) {
	clean(t)
	w := seedWork(t, work{name: "explicit only, r18, editorially sfw", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, w, "vndb", "", "honlyexplicit", model.SexualExplicit)

	var row struct {
		Site                *string `gorm:"column:site"`
		ProductWorkID       *int64  `gorm:"column:product_work_id"`
		ContentRating       int16   `gorm:"column:content_rating"`
		DisplayNSFW         bool    `gorm:"column:display_nsfw"`
		CoverArtAllExplicit bool    `gorm:"column:cover_art_all_explicit"`
	}
	require.NoError(t, testDB.Raw(`SELECT site, product_work_id, content_rating, display_nsfw,
		cover_art_all_explicit FROM catalog_work WHERE id = ?`, w).Scan(&row).Error)
	require.False(t, row.DisplayNSFW, "the editorial column is untouched — that is the point")
	assert.Equal(t, model.DisplayLimitKeyNSFW, model.DisplayLimitKey(model.WorkShelf{
		Site: row.Site, ProductWorkID: row.ProductWorkID, DisplayNSFW: row.DisplayNSFW,
		ContentRating: row.ContentRating, CoverArtAllExplicit: row.CoverArtAllExplicit,
	}), "a work owning only explicit cover art is off the sfw shelf without anyone editing it")

	st, err := audit(context.Background(), testDB, false, defaultMaxFix)
	require.NoError(t, err)
	assert.Empty(t, st.Drift, "a work the trigger handled is not drift")
}

// The clean cases are the positive control for the finding cases: without them
// a passing audit is indistinguishable from a predicate that matches nothing.
func TestAuditSeparatesDriftFromTheRatingQuestion(t *testing.T) {
	clean(t)
	ctx := context.Background()

	var drifted, healed int64
	withoutTheTrigger(t, func() {
		drifted = seedWork(t, work{name: "explicit only, column never derived", rating: model.ContentRatingR18, claimed: true})
		seedCover(t, drifted, "vndb", "", "hdrift", model.SexualExplicit)
	})

	healed = seedWork(t, work{name: "explicit only, column derived", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, healed, "vndb", "", "hhealed", model.SexualExplicit)

	misrated := seedWork(t, work{name: "explicit only but rated all ages", rating: model.ContentRatingAllAges, claimed: true})
	seedCover(t, misrated, "bangumi", "", "hallages", model.SexualExplicit)

	packaging := seedWork(t, work{name: "only safe row is a box shot", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, packaging, "vndb", "", "hpkgexpl", model.SexualExplicit)
	seedCover(t, packaging, "vndb", "pkgfront", "hpkgsafe", model.SexualSafe)

	unhashed := seedWork(t, work{name: "safe cover carries no image", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, unhashed, "vndb", "", "hunhashexpl", model.SexualExplicit)
	seedCover(t, unhashed, "vndb", "", "", model.SexualSafe)

	safe := seedWork(t, work{name: "has a safe cover", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, safe, "vndb", "", "hsafeexpl", model.SexualExplicit)
	seedCover(t, safe, "vndb", "", "hsafesafe", model.SexualSafe)

	suggestive := seedWork(t, work{name: "safest cover is suggestive", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, suggestive, "vndb", "", "hsuggestive", model.SexualSuggestive)

	ghostOnly := seedWork(t, work{name: "no real cover art at all", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, ghostOnly, "censored", "", "hghost", model.SexualSafe)

	bare := seedWork(t, work{name: "no cover rows at all", rating: model.ContentRatingAllAges, claimed: true})

	st, err := audit(ctx, testDB, false, defaultMaxFix)
	require.NoError(t, err)
	assert.Equal(t, []int64{drifted}, ids(st.Drift))
	assert.Equal(t, []int64{misrated}, ids(st.Misrated))
	assert.Zero(t, st.Fixed, "a report-only run must not write")

	for _, id := range []int64{safe, suggestive, ghostOnly, bare} {
		assert.False(t, storedGrade(t, id), "work %d owns electable safe art", id)
	}
	for _, id := range []int64{healed, packaging, unhashed, misrated} {
		assert.True(t, storedGrade(t, id), "work %d owns no electable safe art", id)
	}
}

func TestFixRepairsDriftInBothDirectionsAndIsIdempotent(t *testing.T) {
	clean(t)
	ctx := context.Background()

	var stuckFalse, stuckTrue, healthy int64
	withoutTheTrigger(t, func() {
		stuckFalse = seedWork(t, work{name: "explicit only, column says false", rating: model.ContentRatingR18, claimed: true})
		seedCover(t, stuckFalse, "vndb", "", "hstuckfalse", model.SexualExplicit)
		stuckTrue = seedWork(t, work{name: "safe cover, column will say true", rating: model.ContentRatingR18, claimed: true})
		seedCover(t, stuckTrue, "vndb", "", "hstucktrue", model.SexualSafe)
	})
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET cover_art_all_explicit = true WHERE id = ?`, stuckTrue).Error)
	healthy = seedWork(t, work{name: "healthy", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, healthy, "vndb", "", "hhealthy", model.SexualSafe)

	st, err := audit(ctx, testDB, true, defaultMaxFix)
	require.NoError(t, err)
	assert.EqualValues(t, 2, st.Fixed)
	assert.True(t, storedGrade(t, stuckFalse), "a column stuck false must be repaired up")
	assert.False(t, storedGrade(t, stuckTrue), "a column stuck true must be repaired down")
	assert.False(t, storedGrade(t, healthy))

	st, err = audit(ctx, testDB, true, defaultMaxFix)
	require.NoError(t, err)
	assert.Zero(t, st.Fixed, "a second run has nothing left to fix")
	assert.Empty(t, st.Drift)
}

func TestFixRefusesARunawayBeforeWritingAnything(t *testing.T) {
	clean(t)
	ctx := context.Background()

	var first, second int64
	withoutTheTrigger(t, func() {
		first = seedWork(t, work{name: "runaway one", rating: model.ContentRatingR18, claimed: true})
		seedCover(t, first, "vndb", "", "hrun1", model.SexualExplicit)
		second = seedWork(t, work{name: "runaway two", rating: model.ContentRatingR18, claimed: true})
		seedCover(t, second, "vndb", "", "hrun2", model.SexualExplicit)
	})

	_, err := audit(ctx, testDB, true, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to fix")
	assert.False(t, storedGrade(t, first), "the refusal must happen before any write")
	assert.False(t, storedGrade(t, second), "the refusal must happen before any write")

	st, err := audit(ctx, testDB, false, 1)
	require.NoError(t, err, "a report-only run is safe at any size")
	assert.Len(t, st.Drift, 2)
}
