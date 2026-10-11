package ratingimport

import (
	"context"
	"os"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/seed"
	"api/internal/platform/catalog/service"
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
		dbtest.SkipMain("jobs/ratingimport")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/ratingimport", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/ratingimport", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/ratingimport", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

const sourceTables = `
DROP TABLE IF EXISTS galgame_rating, game_rating;
CREATE TABLE galgame_rating (
    id bigint PRIMARY KEY, user_id bigint NOT NULL, work_id bigint NOT NULL, overall int NOT NULL,
    art int DEFAULT 0, story int DEFAULT 0, music int DEFAULT 0, "character" int DEFAULT 0,
    route int DEFAULT 0, "system" int DEFAULT 0, voice int DEFAULT 0, replay_value int DEFAULT 0,
    recommend text NOT NULL, spoiler_level text NOT NULL DEFAULT 'none', short_summary text NOT NULL DEFAULT '',
    play_status text NOT NULL DEFAULT 'not_started', created timestamptz NOT NULL, updated timestamptz NOT NULL);
CREATE TABLE game_rating (
    id bigint PRIMARY KEY, game_id bigint NOT NULL, user_id bigint NOT NULL, overall smallint NOT NULL,
    art smallint, story smallint, music smallint, "character" smallint, route smallint, "system" smallint,
    voice smallint, replay_value smallint, recommend text NOT NULL DEFAULT 'neutral',
    play_status text NOT NULL DEFAULT 'finished', spoiler_level text NOT NULL DEFAULT 'none',
    short_summary varchar(2000) NOT NULL DEFAULT '', created timestamptz NOT NULL, updated timestamptz NOT NULL,
    all_routes boolean NOT NULL DEFAULT false);`

func reset(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(sourceTables).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_user_work_rating, catalog_user_work_rating_tombstone,
		catalog_user_work_rating_import`).Error)
	require.NoError(t, testDB.Exec(`TRUNCATE catalog_user_work_state, catalog_work_rating, catalog_redirect,
		catalog_work RESTART IDENTITY CASCADE`).Error)
}

func work(t *testing.T, name string, status int16) int64 {
	t.Helper()
	w := model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: name, Status: status}
	require.NoError(t, testDB.Create(&w).Error)
	return w.ID
}

func run(t *testing.T, apply bool) Result {
	t.Helper()
	res, err := Run(context.Background(), Opts{
		Catalog: testDB,
		Sources: []Source{
			{Site: SiteKungal, ClientID: "forum-client", DB: testDB},
			{Site: SiteLetmoe, ClientID: "letmoe-client", DB: testDB},
		},
		Apply: apply,
	})
	require.NoError(t, err)
	require.Len(t, res.Sites, 2)
	return res
}

func count(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, testDB.Table(table).Count(&n).Error)
	return n
}

func TestImportCarriesBothSitesAndReconcilesOnRerun(t *testing.T) {
	reset(t)
	w1, w2, w3 := work(t, "w1", model.WorkStatusLive), work(t, "w2", model.WorkStatusLive), work(t, "w3", model.WorkStatusLive)
	merged := work(t, "merged", model.WorkStatusMerged)
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_redirect (entity_type, old_id, current_id, merged_at)
		VALUES (?, ?, ?, now())`, model.EntityTypeWork, merged, w1).Error)
	t0 := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	t1 := t0.Add(48 * time.Hour)
	require.NoError(t, testDB.Exec(`INSERT INTO galgame_rating
		(id, user_id, work_id, overall, art, story, recommend, spoiler_level, short_summary, play_status, created, updated) VALUES
		(10, 1, ?, 8, 0, 9, 'yes', 'portion', 'a', 'done_main', ?, ?),
		(11, 2, ?, 5, 0, 0, 'no', 'none', '', 'wish', ?, ?),
		(12, 3, ?, 7, 0, 0, 'neutral', 'serious', '', 'dropped', ?, ?),
		(13, 4, 999999, 7, 0, 0, 'yes', 'none', '', 'doing', ?, ?),
		(14, 5, ?, 11, 0, 0, 'yes', 'none', '', 'doing', ?, ?)`,
		w1, t0, t1, w2, t0, t1, merged, t0, t1, t0, t1, w3, t0, t1).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO game_rating
		(id, game_id, user_id, overall, music, recommend, play_status, all_routes, spoiler_level, short_summary, created, updated) VALUES
		(1, ?, 6, 9, 8, 'strong_yes', 'finished', true, 'none', 'letmoe', ?, ?),
		(2, ?, 7, 6, NULL, 'neutral', 'not_started', false, 'none', '', ?, ?)`,
		w1, t0, t1, w2, t0, t1).Error)
	require.NoError(t, testDB.Create(&model.CatalogUserWorkState{ActorUID: 2, WorkID: w2, State: model.WorkStateDoing}).Error)

	dry := run(t, false)
	require.Equal(t, 3, dry.Sites[0].Inserted)
	require.Zero(t, count(t, "catalog_user_work_rating"), "a dry run rolls everything back")
	require.Zero(t, count(t, "catalog_user_work_rating_import"))

	res := run(t, true)
	forum, letmoe := res.Sites[0], res.Sites[1]
	require.Equal(t, Stats{Site: SiteKungal, Read: 5, Inserted: 3, SkippedWork: 1, Redirected: 1, Invalid: 1,
		StatesSeeded: 2, StatesKept: 1}, forum)
	require.Equal(t, Stats{Site: SiteLetmoe, Read: 2, Inserted: 2, StatesSeeded: 1}, letmoe)

	var r10 model.CatalogUserWorkRating
	require.NoError(t, testDB.Where("id = 10").Take(&r10).Error)
	require.Equal(t, w1, r10.WorkID)
	require.Nil(t, r10.Art, "the forum's 0 is a skipped aspect")
	require.Equal(t, int16(9), *r10.Story)
	require.Equal(t, model.RecommendYes, r10.Recommend)
	require.Equal(t, model.SpoilerMild, r10.SpoilerLevel)
	require.Equal(t, "forum-client", r10.ClientID)
	require.Equal(t, SiteKungal, r10.Site)
	require.True(t, r10.CreatedAt.Equal(t0))
	require.True(t, r10.UpdatedAt.Equal(t1))
	require.True(t, r10.ChangedAt.After(t1), "the feed sees an import when it happens, not when the source was edited")

	var r12 model.CatalogUserWorkRating
	require.NoError(t, testDB.Where("id = 12").Take(&r12).Error)
	require.Equal(t, w1, r12.WorkID, "a merged work's rating follows the redirect")

	var letmoeIDs []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_user_work_rating WHERE site = ? ORDER BY id`, SiteLetmoe).Scan(&letmoeIDs).Error)
	require.Len(t, letmoeIDs, 2)
	require.Greater(t, letmoeIDs[0], int64(1_000_000), "the catalog's own ids start above the forum's")

	states := map[[2]int64]model.CatalogUserWorkState{}
	var rows []model.CatalogUserWorkState
	require.NoError(t, testDB.Find(&rows).Error)
	for _, s := range rows {
		states[[2]int64{s.ActorUID, s.WorkID}] = s
	}
	require.Len(t, states, 4)
	require.Equal(t, model.WorkStateDone, states[[2]int64{1, w1}].State)
	require.Equal(t, model.WorkCompletionMain, *states[[2]int64{1, w1}].Completion)
	require.True(t, states[[2]int64{1, w1}].CreatedAt.Equal(t0), "a seeded state keeps the rating's creation time")
	require.True(t, states[[2]int64{1, w1}].UpdatedAt.After(t1), "and syncs from when it reached the catalog")
	require.Equal(t, model.WorkStateDoing, states[[2]int64{2, w2}].State, "an existing state wins over the rating's")
	require.Equal(t, model.WorkStateDropped, states[[2]int64{3, w1}].State)
	require.Equal(t, model.WorkCompletionAll, *states[[2]int64{6, w1}].Completion)
	require.NotContains(t, states, [2]int64{7, w2}, "not_started is no state")

	var agg struct{ VoteCount int }
	require.NoError(t, testDB.Raw(`SELECT vote_count FROM catalog_work_rating WHERE work_id = ?
		AND source_id = (SELECT id FROM catalog_source WHERE key = 'nextmoe')`, w1).Scan(&agg).Error)
	require.Equal(t, 3, agg.VoteCount)

	again := run(t, true)
	require.Zero(t, again.Sites[0].Inserted+again.Sites[0].Updated+again.Sites[1].Inserted+again.Sites[1].Updated)
	require.Equal(t, 3, again.Sites[0].Unchanged)
	require.Zero(t, again.Aggregates.Written, "an unchanged rerun publishes nothing new")

	t2 := t1.Add(24 * time.Hour)
	require.NoError(t, testDB.Exec(`UPDATE galgame_rating SET overall = 6, updated = ? WHERE id = 10`, t2).Error)
	require.NoError(t, testDB.Exec(`DELETE FROM galgame_rating WHERE id = 11`).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO galgame_rating (id, user_id, work_id, overall, recommend, created, updated)
		VALUES (15, 8, ?, 4, 'no', ?, ?)`, w3, t2, t2).Error)
	require.NoError(t, service.NewUserRatingService(testDB).DeleteMine(context.Background(), 3, w1))
	require.NoError(t, testDB.Exec(`INSERT INTO game_rating (id, game_id, user_id, overall, recommend, created, updated)
		VALUES (3, ?, 1, 2, 'strong_no', ?, ?)`, w1, t2, t2.Add(time.Hour)).Error)

	final := run(t, true)
	forum, letmoe = final.Sites[0], final.Sites[1]
	require.Equal(t, 1, forum.Updated)
	require.Equal(t, 1, forum.Inserted)
	require.Equal(t, 1, forum.Withdrawn)
	require.Equal(t, 1, forum.SkippedGone, "a rating the catalog deleted stays deleted")
	require.Equal(t, 1, letmoe.Adopted)
	require.Equal(t, 1, letmoe.Updated, "the newer copy wins the shared id")

	require.NoError(t, testDB.Where("id = 10").Take(&r10).Error)
	require.Equal(t, int16(2), r10.Overall, "user 1's LetMoe edit is newer than the forum's")
	var r15 model.CatalogUserWorkRating
	require.NoError(t, testDB.Where("id = 15").Take(&r15).Error)
	require.Equal(t, w3, r15.WorkID)
	var gone []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM catalog_user_work_rating_tombstone ORDER BY id`).Scan(&gone).Error)
	require.Equal(t, []int64{11, 12}, gone)
	var back int64
	require.NoError(t, testDB.Raw(`SELECT count(*) FROM catalog_user_work_rating WHERE id = 12`).Scan(&back).Error)
	require.Zero(t, back)

	require.NoError(t, testDB.Exec(`DELETE FROM galgame_rating WHERE id = 10`).Error)
	shared := run(t, true)
	require.Zero(t, shared.Sites[0].Withdrawn, "LetMoe still holds the rating the forum copy shared")
	require.NoError(t, testDB.Where("id = 10").Take(&r10).Error)
}

func TestImportRunsOneSiteAlone(t *testing.T) {
	reset(t)
	w := work(t, "alone", model.WorkStatusLive)
	now := time.Now().UTC()
	require.NoError(t, testDB.Exec(`INSERT INTO game_rating (id, game_id, user_id, overall, recommend, created, updated)
		VALUES (1, ?, 1, 8, 'yes', ?, ?)`, w, now, now).Error)
	res, err := Run(context.Background(), Opts{
		Catalog: testDB, Sources: []Source{{Site: SiteLetmoe, ClientID: "letmoe-client", DB: testDB}}, Apply: true,
	})
	require.NoError(t, err)
	require.Len(t, res.Sites, 1)
	require.Equal(t, 1, res.Sites[0].Inserted)
}
