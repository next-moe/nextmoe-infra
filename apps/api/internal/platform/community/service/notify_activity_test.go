package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"

	"gorm.io/gorm"
)

func notifyingPublish(key string, actor int64, at time.Time, rev int64) ActivityInput {
	in := publishInput(key, actor, at, rev)
	in.Notify = true
	return in
}

func followeeRows(t *testing.T, site string, userID int64) []model.CommunityNotification {
	t.Helper()
	var rows []model.CommunityNotification
	if err := testDB.Where("site = ? AND user_id = ? AND kind = ?", site, userID, model.NotificationKindFolloweeActivity).
		Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("kind-10 rows: %v", err)
	}
	return rows
}

func setNotify(t *testing.T, follower, followee int64, level int16) {
	t.Helper()
	if err := NewFollowService(testDB).SetNotifyLevel(context.Background(), follower, followee, level); err != nil {
		t.Fatalf("set notify %d->%d: %v", follower, followee, err)
	}
}

func TestFolloweeActivityNotifiesOnTheActivitySite(t *testing.T) {
	cleanTables(t)
	const author, loud, quiet int64 = 7, 1, 2
	mustFollow(t, "moyu", loud, author)
	mustFollow(t, "kungal", quiet, author)
	setNotify(t, quiet, author, model.FollowNotifyFeed)
	processBatch(t)

	t1 := time.Now().Add(-2 * time.Hour)
	writeActivities(t, "kungal", notifyingPublish("r:1", author, t1, 1))
	processBatch(t)

	rows := followeeRows(t, "kungal", loud)
	r1 := getActivity(t, "kungal", "r:1")
	if len(rows) != 1 {
		t.Fatalf("the follower is told on the activity's site, whatever site they followed on: %+v", rows)
	}
	n := rows[0]
	if n.ThreadID != 0 || n.AnchorKind != 0 || n.AnchorID != "" || n.PostID != nil || n.PostNumber != nil ||
		n.FoldKey == nil || *n.FoldKey != "followee:7" || n.ActorID == nil || *n.ActorID != author ||
		n.ItemCount != 1 || n.ActorCount != 1 || n.ActivityID == nil || *n.ActivityID != r1.ID {
		t.Fatalf("kind-10 row: %+v", n)
	}
	if got := followeeRows(t, "kungal", quiet); len(got) != 0 {
		t.Fatalf("a feed-only follower gets no notification: %+v", got)
	}
	if got := followeeRows(t, "kungal", author); len(got) != 0 {
		t.Fatal("the author is not told about their own work")
	}

	writeActivities(t, "kungal", notifyingPublish("r:2", author, t1.Add(time.Minute), 1))
	processBatch(t)
	rows = followeeRows(t, "kungal", loud)
	r2 := getActivity(t, "kungal", "r:2")
	if len(rows) != 1 || rows[0].ItemCount != 2 || *rows[0].ActivityID != r2.ID || rows[0].Seq <= n.Seq {
		t.Fatalf("a second publication folds into the row and moves it: %+v", rows)
	}

	moyu := notifyingPublish("m:1", author, t1, 1)
	writeActivities(t, "moyu", moyu)
	processBatch(t)
	if got := followeeRows(t, "moyu", loud); len(got) != 1 || got[0].ItemCount != 1 {
		t.Fatalf("each site folds on its own: %+v", got)
	}

	if _, _, err := NewNotificationService(testDB).MarkRead("kungal", loud, []int64{rows[0].ID}, false); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	writeActivities(t, "kungal", notifyingPublish("r:3", author, t1.Add(2*time.Minute), 1))
	processBatch(t)
	rows = followeeRows(t, "kungal", loud)
	if len(rows) != 2 || rows[1].ReadAt != nil || rows[1].ItemCount != 1 {
		t.Fatalf("after a read the next publication starts a new row: %+v", rows)
	}
}

func TestFolloweeActivityBurstFansOutOnce(t *testing.T) {
	cleanTables(t)
	const author, follower int64 = 7, 1
	mustFollow(t, "kungal", follower, author)
	processBatch(t)

	start := time.Now().Add(-3 * time.Hour)
	var items []ActivityInput
	for i := range 120 {
		items = append(items, notifyingPublish(fmt.Sprintf("r:%03d", i), author, start.Add(time.Duration(i)*time.Second), 1))
	}
	writeActivities(t, "kungal", items[:100]...)
	writeActivities(t, "kungal", items[100:]...)

	delivered, _, _ := processBatch(t)
	if delivered != notifyBatchSize {
		t.Fatalf("every event of the batch counts as delivered, got %d", delivered)
	}
	if left := pendingEvents(t); len(left) != 0 {
		t.Fatalf("one dispatch claims the whole burst, %d left", len(left))
	}
	rows := followeeRows(t, "kungal", follower)
	newest := getActivity(t, "kungal", "r:119")
	if len(rows) != 1 || rows[0].ItemCount != repository.FolloweeFoldCountCap || *rows[0].ActivityID != newest.ID ||
		!rows[0].SinceAt.Equal(start.Truncate(time.Microsecond)) {
		t.Fatalf("one row, count capped, pointing at the newest, since the earliest: %+v", rows)
	}
}

func TestFolloweeActivityRetraction(t *testing.T) {
	cleanTables(t)
	const author, follower int64 = 7, 1
	mustFollow(t, "kungal", follower, author)
	processBatch(t)
	svc := NewNotificationService(testDB)

	t1 := time.Now().Add(-2 * time.Hour)
	writeActivities(t, "kungal", notifyingPublish("r:1", author, t1, 1), notifyingPublish("r:2", author, t1.Add(time.Minute), 1))
	processBatch(t)
	before := followeeRows(t, "kungal", follower)[0]

	writeActivities(t, "kungal", tombstoneInput("r:2", author, 2))
	if pending := pendingEvents(t); len(pending) != 1 || pending[0].Kind != model.EventKindActivityChanged {
		t.Fatalf("removing a notified activity goes through the outbox: %+v", pending)
	}
	if row := followeeRows(t, "kungal", follower)[0]; row.Seq != before.Seq || row.ItemCount != 2 {
		t.Fatalf("the write itself must not touch the notification: %+v", row)
	}
	processBatch(t)
	r1 := getActivity(t, "kungal", "r:1")
	row := followeeRows(t, "kungal", follower)[0]
	if row.ItemCount != 1 || *row.ActivityID != r1.ID || row.Seq <= before.Seq || row.ReadAt != nil {
		t.Fatalf("the fold falls back to the live activity under a new seq: %+v", row)
	}
	feed, _, err := svc.Feed("kungal", before.Seq, 100)
	if err != nil || len(feed) != 1 || feed[0].ID != row.ID {
		t.Fatalf("a mirror past the old seq sees the change: %+v %v", feed, err)
	}

	writeActivities(t, "kungal", tombstoneInput("r:1", author, 2))
	processBatch(t)
	retracted := followeeRows(t, "kungal", follower)[0]
	if retracted.ItemCount != 0 || retracted.ActivityID != nil || retracted.ReadAt == nil || retracted.Seq <= row.Seq {
		t.Fatalf("a fold with nothing left is retracted: %+v", retracted)
	}
	feed, _, _ = svc.Feed("kungal", row.Seq, 100)
	if len(feed) != 1 || feed[0].ItemCount != 0 {
		t.Fatalf("the retraction reaches mirrors: %+v", feed)
	}
	inbox, unread, err := svc.List("kungal", follower, 0, false, 50)
	if err != nil {
		t.Fatalf("inbox: %v", err)
	}
	for _, n := range inbox {
		if n.ID == retracted.ID {
			t.Fatal("the inbox leaves a retracted row out")
		}
	}
	if unread != 0 {
		t.Fatalf("a retracted row is not unread, got %d", unread)
	}
}

func TestFolloweeActivityDispatchRechecks(t *testing.T) {
	cleanTables(t)
	const author, follower, leaver int64 = 7, 1, 2
	mustFollow(t, "kungal", follower, author)
	mustFollow(t, "kungal", leaver, author)
	processBatch(t)
	at := time.Now().Add(-time.Hour)

	writeActivities(t, "kungal", notifyingPublish("gone", author, at, 1))
	writeActivities(t, "kungal", tombstoneInput("gone", author, 2))
	processBatch(t)
	if rows := followeeRows(t, "kungal", follower); len(rows) != 0 {
		t.Fatalf("an activity removed before dispatch notifies no one: %+v", rows)
	}

	writeActivities(t, "kungal", notifyingPublish("kept", author, at, 1))
	setNotify(t, follower, author, model.FollowNotifyFeed)
	if _, err := NewFollowService(testDB).Unfollow(context.Background(), leaver, author); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	processBatch(t)
	if rows := followeeRows(t, "kungal", follower); len(rows) != 0 {
		t.Fatalf("a follower who went feed-only before dispatch is not told: %+v", rows)
	}
	if rows := followeeRows(t, "kungal", leaver); len(rows) != 0 {
		t.Fatalf("a follower who left before dispatch is not told: %+v", rows)
	}
}

func TestFolloweeThreadHonorsNotifyLevel(t *testing.T) {
	cleanTables(t)
	const author, loud, quiet int64 = 7, 1, 2
	mustFollow(t, "letmoe", loud, author)
	mustFollow(t, "letmoe", quiet, author)
	setNotify(t, quiet, author, model.FollowNotifyFeed)
	processBatch(t)

	openTopic(t, NewThreadService(testDB, NoopSink{}), "letmoe", author, "b1", "hello")
	processBatch(t)
	has9 := func(uid int64) bool {
		for _, n := range notifsOfSite(t, "letmoe", uid) {
			if n.Kind == model.NotificationKindFolloweeThreadCreated {
				return true
			}
		}
		return false
	}
	if !has9(loud) || has9(quiet) {
		t.Fatalf("kind 9 goes to notify-all followers only: loud=%v quiet=%v", has9(loud), has9(quiet))
	}
}

func TestFollowNotifyLevel(t *testing.T) {
	cleanTables(t)
	fs := NewFollowService(testDB)
	ctx := context.Background()
	if err := fs.SetNotifyLevel(ctx, 1, 2, model.FollowNotifyFeed); err != ErrNotFollowing {
		t.Fatalf("changing the level must never create the follow: %v", err)
	}
	var edges int64
	testDB.Model(&model.CommunityUserFollow{}).Count(&edges)
	if edges != 0 {
		t.Fatal("no edge may appear")
	}
	mustFollow(t, "kungal", 1, 2)
	if level, ok, _ := fs.NotifyLevel(ctx, 1, 2); !ok || level != model.FollowNotifyAll {
		t.Fatalf("a new follow notifies on everything: %d %v", level, ok)
	}
	events := len(pendingEvents(t))
	setNotify(t, 1, 2, model.FollowNotifyFeed)
	if level, _, _ := fs.NotifyLevel(ctx, 1, 2); level != model.FollowNotifyFeed {
		t.Fatalf("level: %d", level)
	}
	if len(pendingEvents(t)) != events {
		t.Fatal("changing the level must not enqueue a followed event")
	}
	if created, err := fs.Follow(ctx, "kungal", 1, 2); err != nil || created {
		t.Fatalf("re-follow: %v %v", created, err)
	}
	if level, _, _ := fs.NotifyLevel(ctx, 1, 2); level != model.FollowNotifyFeed {
		t.Fatal("following again keeps the chosen level")
	}
	if err := fs.SetNotifyLevel(ctx, 1, 2, 7); err == nil {
		t.Fatal("an unknown level is refused")
	}
}

func TestPurgeTakesActivities(t *testing.T) {
	cleanTables(t)
	ps := NewPostService(testDB, NoopSink{})
	ctx := context.Background()
	const gone, follower, other int64 = 7, 1, 8
	mustFollow(t, "kungal", follower, gone)
	mustFollow(t, "kungal", follower, other)
	processBatch(t)
	at := time.Now().Add(-time.Hour)
	writeActivities(t, "kungal", notifyingPublish("g:1", gone, at, 1), tombstoneInput("g:2", gone, 1),
		notifyingPublish("o:1", other, at, 1))
	writeActivities(t, "moyu", publishInput("g:m", gone, at, 1))
	processBatch(t)
	if _, err := NewActivityService(testDB).MarkSeen(gone, nil); err != nil {
		t.Fatalf("seen: %v", err)
	}
	var goneRow model.CommunityNotification
	testDB.Where("site = 'kungal' AND user_id = ? AND fold_key = ?", follower, "followee:7").Take(&goneRow)

	res, err := ps.PurgeAuthor(ctx, "kungal", gone)
	if err != nil {
		t.Fatalf("purge author: %v", err)
	}
	if res.ActivitiesDeleted != 2 {
		t.Fatalf("the site's activities by the author go, tombstones included: %d", res.ActivitiesDeleted)
	}
	count := func(q string, args ...any) int64 {
		t.Helper()
		var n int64
		if err := testDB.Raw(q, args...).Scan(&n).Error; err != nil {
			t.Fatalf("count: %v", err)
		}
		return n
	}
	if count(`SELECT count(*) FROM community_activity_group WHERE site = 'kungal' AND actor_id = ?`, gone) != 0 {
		t.Fatal("the author's groups go with the activities")
	}
	if count(`SELECT count(*) FROM community_activity WHERE site = 'kungal' AND actor_id = ?`, other) != 1 ||
		count(`SELECT count(*) FROM community_activity WHERE site = 'moyu' AND actor_id = ?`, gone) != 1 {
		t.Fatal("a site purge touches only that site's rows of that author")
	}
	var retracted model.CommunityNotification
	testDB.Where("id = ?", goneRow.ID).Take(&retracted)
	if goneRow.ID == 0 || retracted.ItemCount != 0 || retracted.ReadAt == nil || retracted.Seq <= goneRow.Seq ||
		retracted.FoldKey != nil {
		t.Fatalf("the notifications the author's activities raised are retracted and stop naming them: %+v", retracted)
	}

	var moyuRows int64
	testDB.Raw(`SELECT count(*) FROM community_event WHERE site = 'moyu'`).Scan(&moyuRows)
	if moyuRows != 0 {
		t.Fatal("setup: moyu must be reachable only through community_activity")
	}
	if err := ps.PurgeAccount(ctx, gone); err != nil {
		t.Fatalf("purge account: %v", err)
	}
	if count(`SELECT count(*) FROM community_activity WHERE actor_id = ?`, gone) != 0 {
		t.Fatal("an account purge reaches every site's activities")
	}
	if count(`SELECT count(*) FROM community_feed_seen WHERE user_id = ?`, gone) != 0 {
		t.Fatal("an account purge drops the feed's seen mark")
	}
}

func TestFolloweeActivityClaimsWhatArrivedMidBatch(t *testing.T) {
	cleanTables(t)
	const author, follower int64 = 7, 1
	mustFollow(t, "kungal", follower, author)
	processBatch(t)
	at := time.Now().Add(-time.Hour)
	writeActivities(t, "kungal", notifyingPublish("early", author, at, 1))
	writeActivities(t, "kungal", notifyingPublish("late", author, at.Add(time.Minute), 1))
	pending := pendingEvents(t)
	early := pending[0]
	// An earlier event of the same batch already claimed "early"; "late" was
	// committed after that claim.
	if err := testDB.Model(&model.CommunityEvent{}).Where("id = ?", early.ID).
		Update("processed_at", time.Now()).Error; err != nil {
		t.Fatalf("mark claimed: %v", err)
	}

	svc := NewNotificationService(testDB)
	if err := testDB.Transaction(func(tx *gorm.DB) error {
		_, err := svc.dispatchActivityPublished(tx, &early)
		return err
	}); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if left := pendingEvents(t); len(left) != 0 {
		t.Fatalf("the late event must be claimed: %+v", left)
	}
	late := getActivity(t, "kungal", "late")
	rows := followeeRows(t, "kungal", follower)
	if len(rows) != 1 || rows[0].ActivityID == nil || *rows[0].ActivityID != late.ID {
		t.Fatalf("what a claim takes it must deliver: %+v", rows)
	}
}

func TestPurgeDoesNotDeadlockWithAWriteOfTheSameAuthor(t *testing.T) {
	cleanTables(t)
	at := time.Now().Add(-time.Hour)
	writeActivities(t, "kungal", publishInput("k", 7, at, 1))

	w := testDB.Begin()
	defer w.Rollback()
	next := publishInput("k", 7, at, 2)
	write, reason := validateActivity(next, kungalHosts, time.Now())
	if reason != "" {
		t.Fatalf("valid write: %s", reason)
	}
	res, err := repository.UpsertActivityTx(w, "kungal", write, false)
	if err != nil {
		t.Fatalf("hold the activity row: %v", err)
	}

	purged := make(chan error, 1)
	go func() {
		_, err := NewPostService(testDB, NoopSink{}).PurgeAuthor(context.Background(), "kungal", 7)
		purged <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int64
		testDB.Raw(`SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event IN ('transactionid', 'tuple')`).Scan(&waiting)
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the purge never waited on the held row")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if err := repository.RefreshActivityGroupTx(w, res.NewGroup); err != nil {
		t.Fatalf("the write's group recount must not deadlock with the purge: %v", err)
	}
	if err := w.Commit().Error; err != nil {
		t.Fatalf("commit write: %v", err)
	}
	if err := <-purged; err != nil {
		t.Fatalf("the purge must finish after the write: %v", err)
	}
	var left int64
	testDB.Model(&model.CommunityActivity{}).Where("actor_id = 7").Count(&left)
	if left != 0 {
		t.Fatalf("the purge still removes the author's activities, left %d", left)
	}
}
