package forumratings

import (
	"context"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/testsupport/dbtest"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("jobs/forumratings")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/forumratings", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/forumratings", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/forumratings", "catalog seed failed: %v", err)
	}
	if err := db.Exec(`CREATE TABLE IF NOT EXISTS galgame_rating (
		id serial primary key,
		work_id bigint not null,
		user_id int not null,
		overall int not null,
		unique(user_id, work_id)
	)`).Error; err != nil {
		dbtest.SkipMainf("jobs/forumratings", "galgame_rating fixture: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func clean(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec("TRUNCATE catalog_work_rating, catalog_work RESTART IDENTITY CASCADE").Error)
	require.NoError(t, testDB.Exec("TRUNCATE galgame_rating RESTART IDENTITY").Error)
}

func run(t *testing.T, apply bool) *Stats {
	t.Helper()
	st, err := Run(context.Background(), Opts{DB: testDB, ForumDB: testDB, Apply: apply})
	require.NoError(t, err)
	return st
}

func nextmoeID(t *testing.T) int16 {
	t.Helper()
	var id int16
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_source WHERE key = 'nextmoe'`).Scan(&id).Error)
	require.NotZero(t, id)
	return id
}

func createWork(t *testing.T, name string) *model.CatalogWork {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, testDB.Create(w).Error)
	return w
}

func createClaimedWork(t *testing.T, name string, state int16) *model.CatalogWork {
	t.Helper()
	w := createWork(t, name)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET site = 'kungal', product_work_id = id, claim_state = ? WHERE id = ?`,
		state, w.ID).Error)
	return w
}

func seedRating(t *testing.T, workID int64, uid, overall int) {
	t.Helper()
	require.NoError(t, testDB.Exec(
		`INSERT INTO galgame_rating (work_id, user_id, overall) VALUES (?,?,?)`,
		workID, uid, overall).Error)
}

type storedRating struct {
	Score        float64
	VoteCount    int
	Rank         *int
	Distribution []byte
	Stats        []byte
}

func loadRating(t *testing.T, workID int64, sourceID int16) (storedRating, bool) {
	t.Helper()
	if ratingCount(t, workID, sourceID) == 0 {
		return storedRating{}, false
	}
	var row storedRating
	require.NoError(t, testDB.Raw(`SELECT score, vote_count, rank, distribution, stats
		FROM catalog_work_rating WHERE work_id = ? AND source_id = ?`, workID, sourceID).Scan(&row).Error)
	return row, true
}

func ratingCount(t *testing.T, workID int64, sourceID int16) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM catalog_work_rating WHERE work_id = ? AND source_id = ?`,
		workID, sourceID).Scan(&n).Error)
	return n
}

func TestHappyPathWritesScoreDistributionStats(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createClaimedWork(t, "forum-rated", model.ClaimStateLive)
	seedRating(t, w.ID, 1, 8)
	seedRating(t, w.ID, 2, 9)
	seedRating(t, w.ID, 3, 10)

	dry := run(t, false)
	require.Equal(t, 1, dry.Candidates)
	require.Equal(t, 1, dry.Eligible)
	require.Zero(t, dry.Written)
	require.Zero(t, ratingCount(t, w.ID, src))

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET updated_at = now() - interval '1 hour' WHERE id = ?`, w.ID).Error)
	var before time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&before).Error)

	st := run(t, true)
	require.Equal(t, 1, st.Candidates)
	require.Equal(t, 1, st.Eligible)
	require.Equal(t, 1, st.Written)
	require.Zero(t, st.Unchanged)
	require.Zero(t, st.Deleted)
	require.Zero(t, st.Errors)

	got, ok := loadRating(t, w.ID, src)
	require.True(t, ok)
	require.Equal(t, 9.0, got.Score)
	require.Equal(t, 3, got.VoteCount)
	require.Nil(t, got.Rank)
	require.JSONEq(t, `{"8":1,"9":1,"10":1}`, string(got.Distribution))
	require.JSONEq(t, `{"average":9,"stdev":1}`, string(got.Stats))

	var after time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&after).Error)
	require.True(t, after.After(before), "written work must be touched")

	again := run(t, true)
	require.Equal(t, 1, again.Eligible)
	require.Zero(t, again.Written)
	require.Equal(t, 1, again.Unchanged)
	require.Zero(t, again.Deleted)
}

func TestStdevZeroWhenAllVotesEqual(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createClaimedWork(t, "equal-votes", model.ClaimStateLive)
	seedRating(t, w.ID, 1, 7)
	seedRating(t, w.ID, 2, 7)
	seedRating(t, w.ID, 3, 7)

	st := run(t, true)
	require.Equal(t, 1, st.Written)
	got, ok := loadRating(t, w.ID, src)
	require.True(t, ok)
	require.Equal(t, 7.0, got.Score)
	require.Equal(t, 3, got.VoteCount)
	require.JSONEq(t, `{"7":3}`, string(got.Distribution))
	require.JSONEq(t, `{"average":7,"stdev":0}`, string(got.Stats))
}

func TestBelowMinVotersWritesNoRow(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createClaimedWork(t, "two-voters", model.ClaimStateLive)
	seedRating(t, w.ID, 1, 8)
	seedRating(t, w.ID, 2, 9)

	st := run(t, true)
	require.Zero(t, st.Candidates)
	require.Zero(t, st.Eligible)
	require.Zero(t, st.Written)
	require.Zero(t, ratingCount(t, w.ID, src))
}

func TestUnknownWorkCountedAndSkipped(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	seedRating(t, 404404, 1, 8)
	seedRating(t, 404404, 2, 9)
	seedRating(t, 404404, 3, 10)

	st := run(t, true)
	require.Equal(t, 1, st.Candidates)
	require.Equal(t, 1, st.Unmapped)
	require.Zero(t, st.Eligible)
	require.Zero(t, st.Written)
	var n int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM catalog_work_rating WHERE source_id = ?`, src).Scan(&n).Error)
	require.Zero(t, n)
}

func TestDeletedWorkUnmapped(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createWork(t, "merged-away")
	require.NoError(t, testDB.Exec(`UPDATE catalog_work SET deleted_at = now() WHERE id = ?`, w.ID).Error)
	seedRating(t, w.ID, 1, 8)
	seedRating(t, w.ID, 2, 9)
	seedRating(t, w.ID, 3, 10)

	st := run(t, true)
	require.Equal(t, 1, st.Unmapped)
	require.Zero(t, st.Eligible)
	require.Zero(t, ratingCount(t, w.ID, src))
}

func TestUnclaimedWorkIsPublished(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createWork(t, "no-claim")
	seedRating(t, w.ID, 1, 6)
	seedRating(t, w.ID, 2, 8)
	seedRating(t, w.ID, 3, 10)

	st := run(t, true)
	require.Equal(t, 1, st.Eligible)
	require.Equal(t, 1, st.Written)
	got, ok := loadRating(t, w.ID, src)
	require.True(t, ok)
	require.Equal(t, 8.0, got.Score)
}

func TestHiddenClaimUnmapped(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createClaimedWork(t, "hidden", model.ClaimStateHidden)
	seedRating(t, w.ID, 1, 8)
	seedRating(t, w.ID, 2, 9)
	seedRating(t, w.ID, 3, 10)

	st := run(t, true)
	require.Equal(t, 1, st.Candidates)
	require.Equal(t, 1, st.Unmapped)
	require.Zero(t, st.Eligible)
	require.Zero(t, ratingCount(t, w.ID, src))
}

func TestVotersDroppingUnderFloorDeletesRow(t *testing.T) {
	clean(t)
	src := nextmoeID(t)
	w := createClaimedWork(t, "drop-under", model.ClaimStateLive)
	seedRating(t, w.ID, 1, 8)
	seedRating(t, w.ID, 2, 9)
	seedRating(t, w.ID, 3, 10)

	st := run(t, true)
	require.Equal(t, 1, st.Written)
	require.Equal(t, int64(1), ratingCount(t, w.ID, src))

	require.NoError(t, testDB.Exec(
		`DELETE FROM galgame_rating WHERE work_id = ? AND user_id = ?`, w.ID, 3).Error)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_work SET updated_at = now() - interval '1 hour' WHERE id = ?`, w.ID).Error)
	var before time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&before).Error)

	gone := run(t, true)
	require.Zero(t, gone.Candidates)
	require.Zero(t, gone.Eligible)
	require.Zero(t, gone.Written)
	require.Equal(t, 1, gone.Deleted)
	require.Zero(t, ratingCount(t, w.ID, src))

	var after time.Time
	require.NoError(t, testDB.Raw(`SELECT updated_at FROM catalog_work WHERE id = ?`, w.ID).Scan(&after).Error)
	require.True(t, after.After(before), "deleted work must be touched")
}
