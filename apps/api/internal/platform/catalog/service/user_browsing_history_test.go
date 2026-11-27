package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/catalog/model"

	"github.com/stretchr/testify/require"
)

func newBrowsingHistorySvc(t *testing.T) *BrowsingHistoryService {
	t.Helper()
	require.NoError(t, testDB.Exec("TRUNCATE catalog_user_work_view, catalog_user_privacy, catalog_user_history_control RESTART IDENTITY").Error)
	return NewBrowsingHistoryService(testDB)
}

func historyWorkIDs(t *testing.T, svc *BrowsingHistoryService, uid int64, f BrowsingHistoryFilter) []int64 {
	t.Helper()
	rows, err := svc.List(context.Background(), uid, f, time.Time{}, 0, 100)
	require.NoError(t, err)
	ids := make([]int64, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.WorkID)
	}
	return ids
}

func TestBrowsingHistoryIsPerAppAndNewestFirst(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	a, b, c := createWork(t, "hist-a"), createWork(t, "hist-b"), createWork(t, "hist-c")

	require.NoError(t, svc.Record(ctx, 7, "forum", a.ID))
	require.NoError(t, svc.Record(ctx, 7, "forum", b.ID))
	require.NoError(t, svc.Record(ctx, 7, "moyu", c.ID))
	require.NoError(t, svc.Record(ctx, 8, "forum", c.ID))
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_view SET last_viewed_at = last_viewed_at - interval '1 hour' WHERE work_id = ?`, a.ID).Error)

	require.Equal(t, []int64{b.ID, a.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{ClientID: "forum"}))
	require.Equal(t, []int64{c.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{ClientID: "moyu"}))
	require.ElementsMatch(t, []int64{a.ID, b.ID, c.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))

	n, err := svc.Count(ctx, 7, BrowsingHistoryFilter{ClientID: "forum"})
	require.NoError(t, err)
	require.EqualValues(t, 2, n)
}

func TestBrowsingHistoryAllAppsMergesPerWork(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	w := createWork(t, "hist-merge")

	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	require.NoError(t, svc.Record(ctx, 7, "moyu", w.ID))

	rows, err := svc.List(ctx, 7, BrowsingHistoryFilter{}, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, rows[0].ViewCount)
}

func TestBrowsingHistoryRevisitWithinAMinuteCountsOnce(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	w := createWork(t, "hist-revisit")

	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	rows, err := svc.List(ctx, 7, BrowsingHistoryFilter{ClientID: "forum"}, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 1, rows[0].ViewCount)

	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_view SET last_viewed_at = last_viewed_at - interval '2 minutes'`).Error)
	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	rows, err = svc.List(ctx, 7, BrowsingHistoryFilter{ClientID: "forum"}, time.Time{}, 0, 10)
	require.NoError(t, err)
	require.EqualValues(t, 2, rows[0].ViewCount)
	require.True(t, rows[0].LastViewedAt.After(rows[0].FirstViewedAt))
}

func TestBrowsingHistoryOffErasesAndStopsRecording(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	w := createWork(t, "hist-off")
	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	require.NoError(t, svc.Record(ctx, 7, "moyu", w.ID))
	require.NoError(t, svc.Record(ctx, 8, "forum", w.ID))

	off := true
	got, err := svc.SetSettings(ctx, 7, &off, nil)
	require.NoError(t, err)
	require.True(t, got.Off)

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserWorkView{}).Where("actor_uid = 7").Count(&n).Error)
	require.Zero(t, n, "switching off erases every app's rows")

	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID), "recording while off succeeds and stores nothing")
	require.NoError(t, testDB.Model(&model.CatalogUserWorkView{}).Where("actor_uid = 7").Count(&n).Error)
	require.Zero(t, n)
	require.Equal(t, []int64{w.ID}, historyWorkIDs(t, svc, 8, BrowsingHistoryFilter{}), "another account is untouched")

	on := false
	_, err = svc.SetSettings(ctx, 7, &on, nil)
	require.NoError(t, err)
	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	require.Equal(t, []int64{w.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))
}

func TestBrowsingHistorySettingsKeepPlayRecordsAndEachOther(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	_, err := NewUserWorkRecordService(testDB).SetPrivacy(ctx, 7, PrivacyChange{PlayRecordsPrivate: ptrBool(true)})
	require.NoError(t, err)

	shared := true
	_, err = svc.SetSettings(ctx, 7, nil, &shared)
	require.NoError(t, err)
	off := true
	got, err := svc.SetSettings(ctx, 7, &off, nil)
	require.NoError(t, err)
	require.True(t, got.Off)
	require.True(t, got.Shared)
	require.NotNil(t, got.ClearedAt)

	p, err := NewUserWorkRecordService(testDB).Privacy(ctx, 7)
	require.NoError(t, err)
	require.True(t, p.Private, "a history switch leaves play_records alone")

	fresh, err := svc.Settings(ctx, 9)
	require.NoError(t, err)
	require.Equal(t, BrowsingHistorySettings{}, fresh, "no row means on and per app")
}

func TestBrowsingHistoryForget(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	a, b := createWork(t, "hist-forget-a"), createWork(t, "hist-forget-b")
	for _, client := range []string{"forum", "moyu"} {
		require.NoError(t, svc.Record(ctx, 7, client, a.ID))
		require.NoError(t, svc.Record(ctx, 7, client, b.ID))
	}

	_, err := svc.Forget(ctx, 7, "forum", a.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{b.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{ClientID: "forum"}))
	require.Len(t, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{ClientID: "moyu"}), 2, "one app's delete leaves the other's")

	_, err = svc.Forget(ctx, 7, "", b.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{a.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))

	_, err = svc.Forget(ctx, 7, "", 0)
	require.NoError(t, err)
	require.Empty(t, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))
}

func TestBrowsingHistoryKeepsTheNewestPerApp(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	w := createWork(t, "hist-cap-new")
	old := createWork(t, "hist-cap-old")
	require.NoError(t, svc.Record(ctx, 7, "forum", old.ID))
	filler := make([]model.CatalogWork, BrowsingHistoryPerApp-1)
	for i := range filler {
		filler[i] = model.CatalogWork{MediumID: 1, OLang: "ja", DisplayName: "hist-cap-filler",
			ContentRating: model.ContentRatingAllAges, Status: model.WorkStatusLive}
	}
	require.NoError(t, testDB.CreateInBatches(filler, 200).Error)
	views := make([]model.CatalogUserWorkView, len(filler))
	dayAgo := time.Now().Add(-24 * time.Hour)
	for i, f := range filler {
		views[i] = model.CatalogUserWorkView{ActorUID: 7, ClientID: "forum", WorkID: f.ID,
			FirstViewedAt: dayAgo, LastViewedAt: dayAgo, ViewCount: 1}
	}
	require.NoError(t, testDB.CreateInBatches(views, 200).Error)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_view SET last_viewed_at = now() - interval '2 days' WHERE work_id = ?`, old.ID).Error)

	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))

	var n int64
	require.NoError(t, testDB.Model(&model.CatalogUserWorkView{}).Where("actor_uid = 7 AND client_id = 'forum'").Count(&n).Error)
	require.EqualValues(t, BrowsingHistoryPerApp, n)
	require.NoError(t, testDB.Model(&model.CatalogUserWorkView{}).Where("work_id = ?", old.ID).Count(&n).Error)
	require.Zero(t, n, "the oldest entry made room")
}

func TestBrowsingHistoryRetention(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	fresh, stale := createWork(t, "hist-fresh"), createWork(t, "hist-stale")
	require.NoError(t, svc.Record(ctx, 7, "forum", fresh.ID))
	require.NoError(t, svc.Record(ctx, 7, "forum", stale.ID))
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_work_view SET last_viewed_at = now() - interval '91 days' WHERE work_id = ?`, stale.ID).Error)

	require.Equal(t, []int64{fresh.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}), "reads hide what the sweep has not reached yet")

	n, err := svc.SweepExpired(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
}

func TestBrowsingHistoryRefusesDeadWorksAndHidesThem(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	dead := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusQuarantine, "hist-dead")
	require.ErrorIs(t, svc.Record(ctx, 7, "forum", dead.ID), ErrHistoryWorkUnavailable)

	w := createWork(t, "hist-later-dead")
	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	require.NoError(t, testDB.Model(&model.CatalogWork{}).Where("id = ?", w.ID).Update("status", model.WorkStatusMerged).Error)
	require.Empty(t, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))
}

func TestBrowsingHistoryDisplayLimit(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	safe := createWorkX(t, galgameMediumID, model.ContentRatingAllAges, model.WorkStatusLive, "hist-sfw")
	adult := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "hist-nsfw")
	require.NoError(t, svc.Record(ctx, 7, "forum", safe.ID))
	require.NoError(t, svc.Record(ctx, 7, "forum", adult.ID))

	require.Equal(t, []int64{safe.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{DisplayLimits: []string{model.DisplayLimitKeySFW}}))
	require.Equal(t, []int64{adult.ID}, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{DisplayLimits: []string{model.DisplayLimitKeyNSFW}}))
}

// settleHistoryChanges moves the rows changed just now past the feed's settle
// window.
func settleHistoryChanges(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_history_control
		SET changed_at = changed_at - interval '1 minute' WHERE changed_at > now() - interval '30 seconds'`).Error)
}

func historyChanges(t *testing.T, svc *BrowsingHistoryService) []BrowsingHistoryChange {
	t.Helper()
	rows, err := svc.Changes(context.Background(), time.Time{}, 0, 100)
	require.NoError(t, err)
	return rows
}

func TestBrowsingHistoryChangesFollowOffOnAndClearAll(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	w := createWork(t, "hist-changes")
	yes, no := true, false

	_, err := svc.SetSettings(ctx, 7, nil, &yes)
	require.NoError(t, err)
	_, err = svc.Forget(ctx, 7, "forum", 0)
	require.NoError(t, err)
	_, err = svc.Forget(ctx, 7, "", w.ID)
	require.NoError(t, err)
	_, err = svc.SetSettings(ctx, 7, &no, nil)
	require.NoError(t, err)
	settleHistoryChanges(t)
	require.Empty(t, historyChanges(t, svc), "sharing, one app's clear, one work's removal and a no-op on reach no other app")
	got, err := svc.Settings(ctx, 7)
	require.NoError(t, err)
	require.Nil(t, got.ClearedAt)

	_, err = svc.SetSettings(ctx, 7, &yes, nil)
	require.NoError(t, err)
	got, err = svc.Settings(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, got.ClearedAt)
	require.Empty(t, historyChanges(t, svc), "a change inside the settle window is not served yet")
	settleHistoryChanges(t)
	rows := historyChanges(t, svc)
	require.Len(t, rows, 1)
	require.True(t, rows[0].Off)
	require.EqualValues(t, 7, rows[0].UID)
	offClearedAt := rows[0].ClearedAt

	_, err = svc.SetSettings(ctx, 7, &yes, nil)
	require.NoError(t, err)
	settleHistoryChanges(t)
	again := historyChanges(t, svc)
	require.Len(t, again, 1)
	require.True(t, again[0].ChangedAt.Equal(rows[0].ChangedAt), "switching off twice moves nothing")

	_, err = svc.SetSettings(ctx, 7, &no, nil)
	require.NoError(t, err)
	settleHistoryChanges(t)
	rows = historyChanges(t, svc)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Off)
	require.True(t, rows[0].ClearedAt.Equal(offClearedAt), "switching back on keeps cleared_at")

	require.NoError(t, svc.Record(ctx, 7, "forum", w.ID))
	_, err = svc.Forget(ctx, 7, "", 0)
	require.NoError(t, err)
	settleHistoryChanges(t)
	rows = historyChanges(t, svc)
	require.Len(t, rows, 1)
	require.False(t, rows[0].Off)
	require.True(t, rows[0].ClearedAt.After(offClearedAt), "a clear across apps moves cleared_at")
	require.Empty(t, historyWorkIDs(t, svc, 7, BrowsingHistoryFilter{}))

	_, err = svc.Forget(ctx, 9, "", 0)
	require.NoError(t, err)
	settleHistoryChanges(t)
	require.Len(t, historyChanges(t, svc), 2, "an account with no privacy row can clear too")
	fresh, err := svc.Settings(ctx, 9)
	require.NoError(t, err)
	require.False(t, fresh.Off)
	require.NotNil(t, fresh.ClearedAt)
}

func TestBrowsingHistoryChangesResumeInOrder(t *testing.T) {
	svc := newBrowsingHistorySvc(t)
	ctx := context.Background()
	for _, uid := range []int64{31, 32, 33} {
		_, err := svc.Forget(ctx, uid, "", 0)
		require.NoError(t, err)
	}
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_history_control
		SET changed_at = now() - make_interval(mins => 40 - actor_uid::int)`).Error)

	first, err := svc.Changes(ctx, time.Time{}, 0, 2)
	require.NoError(t, err)
	require.Len(t, first, 2)
	require.EqualValues(t, 31, first[0].UID)
	require.EqualValues(t, 32, first[1].UID)
	rest, err := svc.Changes(ctx, first[1].ChangedAt, first[1].UID, 2)
	require.NoError(t, err)
	require.Len(t, rest, 1)
	require.EqualValues(t, 33, rest[0].UID)

	_, err = svc.Forget(ctx, 31, "", 0)
	require.NoError(t, err)
	require.NoError(t, testDB.Exec(`UPDATE catalog_user_history_control SET changed_at = now() - interval '1 minute' WHERE actor_uid = 31`).Error)
	moved, err := svc.Changes(ctx, rest[0].ChangedAt, rest[0].UID, 10)
	require.NoError(t, err)
	require.Len(t, moved, 1, "an account that changes again comes back once, after the cursor")
	require.EqualValues(t, 31, moved[0].UID)
}
