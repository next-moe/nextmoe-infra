package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/migrate"
	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func newWorkStateSvc(t *testing.T) *UserWorkStateService {
	t.Helper()
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_state RESTART IDENTITY").Error)
	return NewUserWorkStateService(testDB)
}

func TestWorkStateSetGetMineRoundTrip(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	w := createWork(t, "state-roundtrip")
	comp := model.WorkCompletionMain

	rec, err := svc.Set(ctx, 7, w.ID, model.WorkStateDone, &comp)
	require.NoError(t, err)
	require.Equal(t, w.ID, rec.WorkID)
	require.Equal(t, model.WorkStateDone, rec.State)
	require.NotNil(t, rec.Completion)
	require.Equal(t, model.WorkCompletionMain, *rec.Completion)

	got, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, w.ID, got.WorkID)
	require.Equal(t, model.WorkStateDone, got.State)
	require.NotNil(t, got.Completion)
	require.Equal(t, model.WorkCompletionMain, *got.Completion)
}

func TestWorkStateSetCompletionOnWish(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	w := createWork(t, "state-wish-completion")
	comp := model.WorkCompletionOneRoute

	_, err := svc.Set(ctx, 7, w.ID, model.WorkStateWish, &comp)
	require.ErrorIs(t, err, ErrWorkStateCompletionOnWish)

	got, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestWorkStateSetNonLiveWork(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	dead := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusQuarantine, "state-dead")

	_, err := svc.Set(ctx, 7, dead.ID, model.WorkStateDoing, nil)
	require.ErrorIs(t, err, ErrWorkStateWorkUnavailable)
}

func TestWorkStateSetClearsCompletion(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	w := createWork(t, "state-clear-completion")
	comp := model.WorkCompletionAll

	_, err := svc.Set(ctx, 7, w.ID, model.WorkStateDone, &comp)
	require.NoError(t, err)

	_, err = svc.Set(ctx, 7, w.ID, model.WorkStateDone, nil)
	require.NoError(t, err)

	got, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, model.WorkStateDone, got.State)
	require.Nil(t, got.Completion)
}

func TestWorkStateListMineCursorWalk(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	w1 := createWork(t, "state-cursor-a")
	w2 := createWork(t, "state-cursor-b")
	w3 := createWork(t, "state-cursor-c")
	for _, w := range []*model.CatalogWork{w1, w2, w3} {
		_, err := svc.Set(ctx, 9, w.ID, model.WorkStateDoing, nil)
		require.NoError(t, err)
	}
	stamp := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, testDB.Exec(
		`UPDATE catalog_user_work_state SET updated_at = ? WHERE actor_uid = ?`, stamp, int64(9)).Error)

	n, err := svc.CountMine(ctx, 9)
	require.NoError(t, err)
	require.Equal(t, int64(3), n)

	var seen []int64
	since, sinceID := time.Time{}, int64(0)
	for i := 0; i < 6; i++ {
		page, lerr := svc.ListMine(ctx, 9, since, sinceID, 1)
		require.NoError(t, lerr)
		if len(page) == 0 {
			break
		}
		require.Len(t, page, 1)
		for _, id := range seen {
			require.NotEqual(t, id, page[0].WorkID, "cursor walk repeated work_id %d", page[0].WorkID)
		}
		seen = append(seen, page[0].WorkID)
		since, sinceID = page[0].UpdatedAt, page[0].WorkID
	}
	require.Len(t, seen, 3)
}

func TestWorkStateDeleteMine(t *testing.T) {
	svc := newWorkStateSvc(t)
	ctx := context.Background()
	w := createWork(t, "state-delete")

	_, err := svc.Set(ctx, 7, w.ID, model.WorkStateOnHold, nil)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteMine(ctx, 7, w.ID))

	got, err := svc.GetMine(ctx, 7, w.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	require.NoError(t, svc.DeleteMine(ctx, 7, w.ID))
}

func TestWorkStateBackfillFromPlaytimeStatus(t *testing.T) {
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_state, catalog_user_playtime RESTART IDENTITY").Error)
	w := createWork(t, "state-backfill")
	now := time.Now().UTC()

	require.NoError(t, testDB.Exec(
		`ALTER TABLE catalog_user_playtime ADD COLUMN IF NOT EXISTS status smallint NOT NULL DEFAULT 0`).Error)
	for _, row := range []struct {
		uid    int64
		client string
		status int16
	}{
		{101, "a", 1},
		{101, "b", 2},
		{102, "a", 3},
		{103, "a", 2},
		{104, "a", 0},
	} {
		require.NoError(t, testDB.Exec(
			`INSERT INTO catalog_user_playtime (actor_uid, work_id, client_id, minutes, status, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			row.uid, w.ID, row.client, 10, row.status, now, now,
		).Error)
	}

	require.NoError(t, migrate.Run(testDB))

	var statusCols int64
	require.NoError(t, testDB.Raw(
		`SELECT count(*) FROM information_schema.columns WHERE table_name = 'catalog_user_playtime' AND column_name = 'status'`,
	).Scan(&statusCols).Error)
	require.Equal(t, int64(0), statusCols)

	stateOf := func(uid int64) (int16, bool) {
		t.Helper()
		var row model.CatalogUserWorkState
		err := testDB.Where("actor_uid = ? AND work_id = ?", uid, w.ID).Take(&row).Error
		if err != nil {
			return 0, false
		}
		return row.State, true
	}
	st, ok := stateOf(101)
	require.True(t, ok)
	require.Equal(t, model.WorkStateDone, st)
	st, ok = stateOf(102)
	require.True(t, ok)
	require.Equal(t, model.WorkStateOnHold, st)
	st, ok = stateOf(103)
	require.True(t, ok)
	require.Equal(t, model.WorkStateDropped, st)
	_, ok = stateOf(104)
	require.False(t, ok)

	require.NoError(t, testDB.Exec(
		`UPDATE catalog_user_work_state SET state = ? WHERE actor_uid = ? AND work_id = ?`,
		model.WorkStateDoing, int64(101), w.ID).Error)
	require.NoError(t, migrate.Run(testDB))
	st, ok = stateOf(101)
	require.True(t, ok)
	require.Equal(t, model.WorkStateDoing, st)
}
