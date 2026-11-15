package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"api/internal/platform/catalog/model"
	"api/pkg/communityclient"

	"github.com/stretchr/testify/require"
)

func cleanFeed(t *testing.T) {
	t.Helper()
	cleanTables(t)
	for _, table := range []string{"catalog_activity_item", "catalog_user_work_state", "catalog_user_playtime", "catalog_user_privacy"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+table+" RESTART IDENTITY").Error)
	}
}

type feedState struct {
	model.CatalogActivityItem
	body FeedItem
}

func feedItems(t *testing.T) map[string]feedState {
	t.Helper()
	var rows []model.CatalogActivityItem
	require.NoError(t, testDB.Find(&rows).Error)
	out := map[string]feedState{}
	for _, r := range rows {
		var body FeedItem
		require.NoError(t, json.Unmarshal(r.Item, &body))
		out[r.Key] = feedState{r, body}
	}
	return out
}

func liveFeedKeys(t *testing.T) []string {
	t.Helper()
	var keys []string
	for k, r := range feedItems(t) {
		if !r.Removed {
			keys = append(keys, k)
		}
	}
	return keys
}

func TestFeedPlayStates(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	states := NewUserWorkStateService(testDB)
	w := createWork(t, "Feed  State\tWork")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "Feed R18")
	ws := func(work int64, state string) string { return fmt.Sprintf("ws:7:%d:%s", work, state) }

	_, err := states.Set(ctx, 7, w.ID, model.WorkStateDoing, nil)
	require.NoError(t, err)
	it := feedItems(t)[ws(w.ID, "doing")]
	require.False(t, it.Removed)
	require.Equal(t, FeedItem{
		Verb: "mark", ObjectKind: "work_doing", ObjectLabel: "作品", Title: "Feed State Work", WorkID: &w.ID,
		ObjectRef: fmt.Sprintf("work:%d", w.ID), ContentLimit: "sfw", OccurredAt: it.body.OccurredAt,
	}, it.body)
	first := it.Revision

	comp := model.WorkCompletionAll
	_, err = states.Set(ctx, 7, w.ID, model.WorkStateDoing, &comp)
	require.NoError(t, err)
	require.Equal(t, first, feedItems(t)[ws(w.ID, "doing")].Revision, "a completion change alone is not a new mark")

	_, err = states.Set(ctx, 7, w.ID, model.WorkStateDone, &comp)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{ws(w.ID, "doing"), ws(w.ID, "done")}, liveFeedKeys(t), "doing then done keeps both")

	_, err = states.Set(ctx, 7, r18.ID, model.WorkStateWish, nil)
	require.NoError(t, err)
	require.Equal(t, "nsfw", feedItems(t)[ws(r18.ID, "wish")].body.ContentLimit)

	require.NoError(t, states.DeleteMine(ctx, 7, w.ID))
	require.Equal(t, []string{ws(r18.ID, "wish")}, liveFeedKeys(t), "deleting a state retracts every mark of that work")

	_, err = NewUserWorkRecordService(testDB).SetPrivacy(ctx, 7, PrivacyChange{PlayRecordsPrivate: new(true)})
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t), "going private retracts every mark")
	_, err = states.Set(ctx, 7, w.ID, model.WorkStateDone, nil)
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t), "a private account writes no marks")

	_, err = NewUserWorkRecordService(testDB).SetPrivacy(ctx, 7, PrivacyChange{PlayRecordsPrivate: new(false)})
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t), "going public again brings nothing back")
	_, err = states.Set(ctx, 7, w.ID, model.WorkStateDropped, nil)
	require.NoError(t, err)
	require.Equal(t, []string{ws(w.ID, "dropped")}, liveFeedKeys(t))
}

func TestFeedPlaytimeDays(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	pt := NewUserPlaytimeService(testDB)
	w := createWork(t, "Feed Playtime")
	report := func(client string, minutes int) {
		t.Helper()
		_, err := pt.Report(ctx, PlaytimeReport{ActorUID: 7, WorkID: w.ID, ClientID: client, Minutes: minutes})
		require.NoError(t, err)
	}
	key := fmt.Sprintf("pt:7:%d:%s", w.ID, time.Now().In(beijing).Format("20060102"))
	require.NoError(t, testDB.Exec(`INSERT INTO catalog_user_playtime (actor_uid, work_id, client_id, minutes, created_at, updated_at)
		VALUES (7, ?, 'old', 60, now() - interval '3 days', now() - interval '3 days')`, w.ID).Error)

	report("a", 90)
	require.Equal(t, "当日 +30 分钟 · 累计 1 小时 30 分钟", feedItems(t)[key].body.Excerpt)
	report("b", 20)
	require.Equal(t, "当日 +30 分钟 · 累计 1 小时 30 分钟", feedItems(t)[key].body.Excerpt, "another client below the total adds nothing")
	report("a", 210)
	it := feedItems(t)[key]
	require.Equal(t, "当日 +2 小时 30 分钟 · 累计 3 小时 30 分钟", it.body.Excerpt, "the day counts from the total it started at")
	require.Equal(t, "play", it.body.Verb)
	require.Equal(t, 60, *it.Baseline)

	require.NoError(t, pt.DeleteMine(ctx, 7, w.ID, "a"))
	require.NoError(t, pt.DeleteMine(ctx, 7, w.ID, "b"))
	require.False(t, feedItems(t)[key].Removed, "another client's report is left, so the day stays")
	require.NoError(t, pt.DeleteMine(ctx, 7, w.ID, "old"))
	require.True(t, feedItems(t)[key].Removed, "with no report left the day goes")

	_, err := NewUserWorkRecordService(testDB).SetPrivacy(ctx, 7, PrivacyChange{PlayRecordsPrivate: new(true)})
	require.NoError(t, err)
	report("a", 300)
	require.True(t, feedItems(t)[key].Removed, "a private account writes no playtime days")
}

func TestFeedFolders(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	folders := NewUserFolderService(testDB)
	w1, w2 := createWork(t, "Folder Feed One"), createWork(t, "Folder Feed Two")

	private, err := folders.Create(ctx, FolderCreate{OwnerUID: 7, Name: "Secret", Visibility: model.FolderVisibilityPrivate})
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, 7, private.ID, w1.ID)
	require.NoError(t, err)
	require.Empty(t, feedItems(t), "a private folder writes nothing")

	pub, err := folders.Create(ctx, FolderCreate{OwnerUID: 7, Name: "Shelf", Visibility: model.FolderVisibilityPublic})
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, 7, pub.ID, w1.ID)
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, 7, pub.ID, w1.ID)
	require.NoError(t, err)
	_, err = folders.PutItem(ctx, 7, pub.ID, w2.ID)
	require.NoError(t, err)
	fd, fi1, fi2 := fmt.Sprintf("fd:%d", pub.ID), fmt.Sprintf("fi:%d:%d", pub.ID, w1.ID), fmt.Sprintf("fi:%d:%d", pub.ID, w2.ID)
	items := feedItems(t)
	require.ElementsMatch(t, []string{fd, fi1, fi2}, liveFeedKeys(t))
	require.Equal(t, FeedItem{Verb: "publish", ObjectKind: "folder", ObjectLabel: "收藏夹", Title: "Shelf",
		ObjectRef: fmt.Sprintf("folder:%d", pub.ID), ContentLimit: "sfw", OccurredAt: items[fd].body.OccurredAt}, items[fd].body)
	require.Equal(t, "favorite", items[fi1].body.Verb)
	require.Equal(t, "Shelf", items[fi1].body.Excerpt)

	require.NoError(t, folders.DeleteItem(ctx, 7, pub.ID, w2.ID))
	require.ElementsMatch(t, []string{fd, fi1}, liveFeedKeys(t))

	unlisted := model.FolderVisibilityUnlisted
	_, err = folders.Patch(ctx, 7, pub.ID, FolderPatch{Visibility: &unlisted})
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t), "a folder leaving public retracts all of its items")
	public := model.FolderVisibilityPublic
	_, err = folders.Patch(ctx, 7, pub.ID, FolderPatch{Visibility: &public})
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t), "a folder is announced once")

	_, err = folders.Patch(ctx, 7, private.ID, FolderPatch{Visibility: &public})
	require.NoError(t, err)
	require.Equal(t, []string{fmt.Sprintf("fd:%d", private.ID)}, liveFeedKeys(t), "a folder first made public is announced then, without its works")

	require.NoError(t, folders.Delete(ctx, 7, private.ID))
	require.Empty(t, liveFeedKeys(t))
}

func TestFeedCompanyFollows(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	follows := NewUserEntityFollowService(testDB)
	records := NewUserWorkRecordService(testDB)
	a := createLabel(t, "Feed Brand A", model.LabelKindGameBrand)
	b := createLabel(t, "Feed Brand B", model.LabelKindGameBrand)

	_, err := follows.FollowCompany(ctx, 7, a, "c", "s")
	require.NoError(t, err)
	require.Empty(t, feedItems(t), "followed companies are private by default")

	_, err = records.SetPrivacy(ctx, 7, PrivacyChange{FollowedCompaniesPublic: new(true)})
	require.NoError(t, err)
	require.Empty(t, feedItems(t), "making the list public does not announce earlier follows")
	_, err = follows.FollowCompany(ctx, 7, b, "c", "s")
	require.NoError(t, err)
	cf := fmt.Sprintf("cf:7:%d", b)
	it := feedItems(t)[cf]
	require.Equal(t, FeedItem{Verb: "follow", ObjectKind: "company", ObjectLabel: "会社", Title: "Feed Brand B",
		ObjectRef: fmt.Sprintf("company:%d", b), ContentLimit: "sfw", OccurredAt: it.body.OccurredAt}, it.body)
	rev := it.Revision
	_, err = follows.FollowCompany(ctx, 7, b, "c", "s")
	require.NoError(t, err)
	require.Equal(t, rev, feedItems(t)[cf].Revision, "following again changes nothing")

	require.NoError(t, follows.UnfollowCompany(ctx, 7, b))
	require.True(t, feedItems(t)[cf].Removed)
	_, err = follows.FollowCompany(ctx, 7, b, "c", "s")
	require.NoError(t, err)
	require.False(t, feedItems(t)[cf].Removed)

	_, err = records.SetPrivacy(ctx, 7, PrivacyChange{FollowedCompaniesPublic: new(false)})
	require.NoError(t, err)
	require.Empty(t, liveFeedKeys(t))
}

func TestBackfillPlayStateFeed(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	states := NewUserWorkStateService(testDB)
	w := createWork(t, "Backfill Work")
	r18 := createWorkX(t, galgameMediumID, model.ContentRatingR18, model.WorkStatusLive, "Backfill R18")
	_, err := states.Set(ctx, 7, w.ID, model.WorkStateDone, nil)
	require.NoError(t, err)
	_, err = states.Set(ctx, 7, r18.ID, model.WorkStateWish, nil)
	require.NoError(t, err)
	_, err = states.Set(ctx, 8, w.ID, model.WorkStateDoing, nil)
	require.NoError(t, err)
	_, err = states.Set(ctx, 9, w.ID, model.WorkStateDoing, nil)
	require.NoError(t, err)
	_, err = NewUserWorkRecordService(testDB).SetPrivacy(ctx, 9, PrivacyChange{PlayRecordsPrivate: new(true)})
	require.NoError(t, err)
	past := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, testDB.Exec("UPDATE catalog_user_work_state SET updated_at = ? WHERE actor_uid = 8", past).Error)
	require.NoError(t, testDB.Exec("TRUNCATE catalog_activity_item").Error)
	live := fmt.Sprintf("ws:7:%d:done", w.ID)
	require.NoError(t, putFeedItemTx(testDB, feedRow{key: live, uid: 7, workID: &w.ID, item: FeedItem{Verb: "mark", Title: "kept"}}))

	n, err := BackfillPlayStateFeed(ctx, testDB, false)
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "a private account and a key already held are left out")
	require.Len(t, feedItems(t), 1, "the dry run writes nothing")

	n, err = BackfillPlayStateFeed(ctx, testDB, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), n)
	items := feedItems(t)
	require.Equal(t, "kept", items[live].body.Title)
	old := items[fmt.Sprintf("ws:8:%d:doing", w.ID)]
	require.True(t, old.body.OccurredAt.Equal(past), "a backfilled mark is dated at its state's updated_at")
	require.Equal(t, FeedItem{Verb: "mark", ObjectKind: "work_doing", ObjectLabel: "作品", Title: "Backfill Work", WorkID: &w.ID,
		ObjectRef: fmt.Sprintf("work:%d", w.ID), ContentLimit: "sfw", OccurredAt: old.body.OccurredAt}, old.body)
	require.Equal(t, "nsfw", items[fmt.Sprintf("ws:7:%d:wish", r18.ID)].body.ContentLimit)

	n, err = BackfillPlayStateFeed(ctx, testDB, true)
	require.NoError(t, err)
	require.Zero(t, n, "a second run queues nothing")
}

type fakeCommunity struct {
	batches [][]communityclient.ActivityItem
	fail    bool
}

func (f *fakeCommunity) WriteActivities(_ context.Context, items []communityclient.ActivityItem) ([]communityclient.ActivityOutcome, error) {
	if f.fail {
		return nil, fmt.Errorf("community down")
	}
	f.batches = append(f.batches, items)
	out := make([]communityclient.ActivityOutcome, len(items))
	for i, it := range items {
		out[i] = communityclient.ActivityOutcome{Key: it.Key, Outcome: "created"}
		if strings.HasPrefix(it.Key, "bad") {
			out[i].Outcome = "invalid"
		}
	}
	return out, nil
}

func TestActivityPusherSendsPendingRows(t *testing.T) {
	cleanFeed(t)
	ctx := context.Background()
	w := createWork(t, "Push Work")
	for i := range 150 {
		require.NoError(t, putFeedItemTx(testDB, feedRow{key: fmt.Sprintf("k:%03d", i), uid: 7, workID: &w.ID,
			item: FeedItem{Verb: "mark", ObjectKind: "work_done", ObjectLabel: "作品", ObjectRef: "work:1", ContentLimit: "sfw", OccurredAt: time.Now()}}))
	}
	require.NoError(t, putFeedItemTx(testDB, feedRow{key: "bad:1", uid: 7, item: FeedItem{Verb: "mark"}}))

	fc := &fakeCommunity{fail: true}
	p := NewActivityPusher(testDB, fc)
	_, err := p.PushBatch(ctx)
	require.Error(t, err)
	var pending int64
	require.NoError(t, testDB.Model(&model.CatalogActivityItem{}).Where("pushed_revision < revision").Count(&pending).Error)
	require.Equal(t, int64(151), pending, "a failed push acknowledges nothing")

	fc.fail = false
	n, err := p.PushBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, 100, n)
	require.Equal(t, "k:000", fc.batches[0][0].Key, "oldest first")
	sent := fc.batches[0][0]
	require.Equal(t, "mark", sent.Verb)
	require.Equal(t, "work:1", sent.ObjectRef)
	require.NotNil(t, sent.OccurredAt)
	n, err = p.PushBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, 51, n, "an item community refused is acknowledged too, not retried forever")
	n, err = p.PushBatch(ctx)
	require.NoError(t, err)
	require.Zero(t, n)

	require.NoError(t, retractFeedItemsTx(testDB, "key = ?", "k:000"))
	n, err = p.PushBatch(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, n)
	tomb := fc.batches[len(fc.batches)-1][0]
	require.True(t, tomb.Removed)
	require.Empty(t, tomb.Verb, "a tombstone carries only its identity")
	require.Greater(t, tomb.Revision, sent.Revision)

	require.NoError(t, testDB.Exec("UPDATE catalog_activity_item SET updated_at = now() - interval '31 days' WHERE key = 'k:000'").Error)
	pruned, err := p.Prune(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), pruned, "an acknowledged tombstone past 30 days goes")
}
