package userplaytime

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
		dbtest.SkipMain("jobs/userplaytime")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("jobs/userplaytime", "cannot connect to test database: %v", err)
	}
	if err := migrate.Run(db); err != nil {
		dbtest.SkipMainf("jobs/userplaytime", "catalog migrate failed: %v", err)
	}
	if err := seed.Run(db); err != nil {
		dbtest.SkipMainf("jobs/userplaytime", "catalog seed failed: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

func createAggWork(t *testing.T, name string) *model.CatalogWork {
	t.Helper()
	w := &model.CatalogWork{
		MediumID: 1, OLang: "ja", DisplayName: name,
		ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive,
	}
	require.NoError(t, testDB.Create(w).Error)
	return w
}

func seedPlaytime(t *testing.T, uid, workID int64, minutes int, client string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, testDB.Create(&model.CatalogUserPlaytime{
		ActorUID: uid, WorkID: workID, ClientID: client, Minutes: minutes,
		Status: model.PlaytimeStatusPlaying, CreatedAt: now, UpdatedAt: now,
	}).Error)
}

func seedState(t *testing.T, uid, workID int64, state int16) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, testDB.Create(&model.CatalogUserWorkState{
		ActorUID: uid, WorkID: workID, State: state, CreatedAt: now, UpdatedAt: now,
	}).Error)
}

func TestAggregateCountsDoneStateOnly(t *testing.T) {
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_state, catalog_user_playtime RESTART IDENTITY").Error)
	a := createAggWork(t, "agg-done-median")
	b := createAggWork(t, "agg-two-reporters")

	seedPlaytime(t, 1, a.ID, 10, "c1")
	seedPlaytime(t, 2, a.ID, 20, "c1")
	seedPlaytime(t, 3, a.ID, 30, "c1")
	seedPlaytime(t, 4, a.ID, 40, "c1")
	seedState(t, 1, a.ID, model.WorkStateDone)
	seedState(t, 2, a.ID, model.WorkStateDone)
	seedState(t, 3, a.ID, model.WorkStateDone)
	seedState(t, 4, a.ID, model.WorkStateDoing)

	seedPlaytime(t, 1, b.ID, 15, "c1")
	seedPlaytime(t, 2, b.ID, 25, "c1")
	seedState(t, 1, b.ID, model.WorkStateDone)
	seedState(t, 2, b.ID, model.WorkStateDone)

	cands, err := aggregate(context.Background(), testDB, model.PlaytimeMinReporters, nil)
	require.NoError(t, err)

	byWork := map[int64]candidate{}
	for _, c := range cands {
		byWork[c.WorkID] = c
	}
	got, ok := byWork[a.ID]
	require.True(t, ok, "work with 3 done reporters must be a candidate")
	require.Equal(t, 20, got.Median)
	require.Equal(t, 3, got.Reporters)
	_, ok = byWork[b.ID]
	require.False(t, ok, "work with 2 done reporters must not appear")
}
