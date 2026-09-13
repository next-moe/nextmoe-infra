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

func shelf(t *testing.T, id int64) bool {
	t.Helper()
	var nsfw bool
	require.NoError(t, testDB.Raw(`SELECT display_nsfw FROM catalog_work WHERE id = ?`, id).Scan(&nsfw).Error)
	return nsfw
}

func ids(f []finding) []int64 {
	out := make([]int64, len(f))
	for i := range f {
		out[i] = f[i].ID
	}
	return out
}

// The clean cases are the positive control for the finding cases: without them
// a passing audit is indistinguishable from a predicate that matches nothing.
func TestAuditSeparatesTheShelfErrorFromEverythingItMustNotTouch(t *testing.T) {
	clean(t)
	ctx := context.Background()

	broken := seedWork(t, work{name: "explicit only, r18, sfw shelf", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, broken, "vndb", "", "hexplicit", model.SexualExplicit)

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
	seedCover(t, ghostOnly, censoredSourceKey, "", "hghost", model.SexualSafe)

	unclaimed := seedWork(t, work{name: "explicit only but unclaimed", rating: model.ContentRatingR18})
	seedCover(t, unclaimed, "vndb", "", "hunclaimed", model.SexualExplicit)

	alreadyNSFW := seedWork(t, work{name: "explicit only, already nsfw shelf", rating: model.ContentRatingR18, claimed: true, shelf: true})
	seedCover(t, alreadyNSFW, "vndb", "", "hnsfwshelf", model.SexualExplicit)

	merged := seedWork(t, work{name: "explicit only but merged away", rating: model.ContentRatingR18, claimed: true, status: model.WorkStatusMerged})
	seedCover(t, merged, "vndb", "", "hmerged", model.SexualExplicit)

	st, err := audit(ctx, testDB, false, defaultMaxFix)
	require.NoError(t, err)
	assert.Equal(t, []int64{broken, packaging, unhashed}, ids(st.Mislabelled))
	assert.Equal(t, []int64{misrated}, ids(st.Misrated))
	assert.Zero(t, st.Fixed, "a report-only run must not write")

	for _, id := range []int64{safe, suggestive, ghostOnly, unclaimed, merged} {
		assert.False(t, shelf(t, id), "work %d must be left on the shelf it was on", id)
	}
}

func TestFixMovesOnlyTheR18FindingsAndIsIdempotent(t *testing.T) {
	clean(t)
	ctx := context.Background()

	broken := seedWork(t, work{name: "explicit only, r18", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, broken, "vndb", "", "hfixme", model.SexualExplicit)
	misrated := seedWork(t, work{name: "explicit only, all ages", rating: model.ContentRatingAllAges, claimed: true})
	seedCover(t, misrated, "vndb", "", "hleaveme", model.SexualExplicit)
	safe := seedWork(t, work{name: "healthy", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, safe, "vndb", "", "hhealthy", model.SexualSafe)

	st, err := audit(ctx, testDB, true, defaultMaxFix)
	require.NoError(t, err)
	assert.EqualValues(t, 1, st.Fixed)
	assert.True(t, shelf(t, broken), "the r18 finding must move to the nsfw shelf")
	assert.False(t, shelf(t, misrated), "an all-ages finding is a rating question and must never be moved")
	assert.False(t, shelf(t, safe), "a work with a safe cover must not move")

	st, err = audit(ctx, testDB, true, defaultMaxFix)
	require.NoError(t, err)
	assert.Zero(t, st.Fixed, "a second run has nothing left to fix")
	assert.Len(t, st.Misrated, 1, "the all-ages finding stays outstanding until a human rules on it")
}

func TestFixRefusesARunawayBeforeWritingAnything(t *testing.T) {
	clean(t)
	ctx := context.Background()

	first := seedWork(t, work{name: "runaway one", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, first, "vndb", "", "hrun1", model.SexualExplicit)
	second := seedWork(t, work{name: "runaway two", rating: model.ContentRatingR18, claimed: true})
	seedCover(t, second, "vndb", "", "hrun2", model.SexualExplicit)

	_, err := audit(ctx, testDB, true, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to fix")
	assert.False(t, shelf(t, first), "the refusal must happen before any write")
	assert.False(t, shelf(t, second), "the refusal must happen before any write")

	st, err := audit(ctx, testDB, false, 1)
	require.NoError(t, err, "a report-only run is safe at any size")
	assert.Len(t, st.Mislabelled, 2)
}
