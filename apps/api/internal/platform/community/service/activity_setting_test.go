package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/community/model"
)

func setHidden(t *testing.T, userID int64, hidden bool) ActivitySetting {
	t.Helper()
	st, err := NewActivityService(testDB).SetHidden(context.Background(), userID, hidden)
	if err != nil {
		t.Fatalf("set hidden %d=%v: %v", userID, hidden, err)
	}
	return st
}

func TestHiddenActivitiesLeaveEveryoneElsesView(t *testing.T) {
	cleanTables(t)
	const viewer, alice, bob int64 = 1, 10, 20
	svc := NewActivityService(testDB)
	followEdge(t, viewer, alice, time.Now().Add(-30*24*time.Hour))
	followEdge(t, viewer, bob, time.Now().Add(-30*24*time.Hour))
	at := time.Now().Add(-time.Hour)
	writeActivities(t, "kungal", publishInput("a:1", alice, at, 1), publishInput("b:1", bob, at, 1))
	writeActivities(t, "moyu", publishInput("a:2", alice, at, 1))

	if st, err := svc.Setting(alice); err != nil || st.Hidden || st.UpdatedAt != nil {
		t.Fatalf("no row reads as shown and never set: %+v %v", st, err)
	}
	if st := setHidden(t, alice, true); !st.Hidden || st.UpdatedAt == nil {
		t.Fatalf("set echoes the setting: %+v", st)
	}
	if st, err := svc.Setting(alice); err != nil || !st.Hidden || st.UpdatedAt == nil {
		t.Fatalf("setting after hiding: %+v %v", st, err)
	}

	groups := feed(t, ActivityFeedParams{UserID: viewer, Limit: 50})
	if len(groups) != 1 || groups[0].ActorID != bob {
		t.Fatalf("a hidden author leaves the following feed on every site: %+v", groups)
	}
	if n, _, err := svc.Unseen(ActivityFeedParams{UserID: viewer}); err != nil || n != 1 {
		t.Fatalf("a hidden author leaves the unseen count: %d %v", n, err)
	}

	others, err := svc.ActorFeed(ActivityFeedParams{UserID: alice, ViewerID: viewer, Limit: 50})
	if err != nil || !others.Hidden || len(others.Groups) != 0 {
		t.Fatalf("someone else reads an empty, hidden page: %+v %v", others, err)
	}
	anonymous, err := svc.ActorFeed(ActivityFeedParams{UserID: alice, Limit: 50})
	if err != nil || !anonymous.Hidden || len(anonymous.Groups) != 0 {
		t.Fatalf("an anonymous reader too: %+v %v", anonymous, err)
	}
	own, err := svc.ActorFeed(ActivityFeedParams{UserID: alice, ViewerID: alice, Limit: 50})
	if err != nil || own.Hidden || len(own.Groups) != 2 {
		t.Fatalf("the author still reads their own: %+v %v", own, err)
	}
	if _, _, err := svc.GroupItems(own.Groups[0].ID, viewer, false, nil, 10); err != ErrActivityGroupNotFound {
		t.Fatalf("a hidden author's group is not found for someone else: %v", err)
	}
	if items, _, err := svc.GroupItems(own.Groups[0].ID, alice, false, nil, 10); err != nil || len(items) != 1 {
		t.Fatalf("the author expands their own group: %+v %v", items, err)
	}
	if bobs, err := svc.ActorFeed(ActivityFeedParams{UserID: bob, ViewerID: viewer, Limit: 50}); err != nil || bobs.Hidden || len(bobs.Groups) != 1 {
		t.Fatalf("hiding is per author: %+v %v", bobs, err)
	}

	setHidden(t, alice, false)
	if groups := feed(t, ActivityFeedParams{UserID: viewer, Limit: 50}); len(groups) != 3 {
		t.Fatalf("showing again restores the feed: %+v", groups)
	}
}

func TestHidingRetractsAndStopsFolloweeNotifications(t *testing.T) {
	cleanTables(t)
	const author, unread, read int64 = 7, 1, 2
	ns := NewNotificationService(testDB)
	mustFollow(t, "kungal", unread, author)
	mustFollow(t, "kungal", read, author)
	processBatch(t)

	t1 := time.Now().Add(-3 * time.Hour)
	writeActivities(t, "kungal", notifyingPublish("r:1", author, t1, 1))
	writeActivities(t, "moyu", notifyingPublish("m:1", author, t1, 1))
	processBatch(t)
	before := append(followeeRows(t, "kungal", unread), followeeRows(t, "moyu", unread)...)
	readRows := followeeRows(t, "kungal", read)
	if len(before) != 2 || len(readRows) != 1 {
		t.Fatalf("setup: one fold per site per follower: %+v %+v", before, readRows)
	}
	if _, _, err := ns.MarkRead("kungal", read, []int64{readRows[0].ID}, false); err != nil {
		t.Fatalf("mark read: %v", err)
	}
	readRows = followeeRows(t, "kungal", read)

	setHidden(t, author, true)
	after := append(followeeRows(t, "kungal", unread), followeeRows(t, "moyu", unread)...)
	after = append(after, followeeRows(t, "kungal", read)...)
	byID := map[int64]model.CommunityNotification{}
	for _, r := range append(before, readRows...) {
		byID[r.ID] = r
	}
	if len(after) != 3 {
		t.Fatalf("hiding retracts rows, it does not delete them: %+v", after)
	}
	for _, r := range after {
		if r.ItemCount != 0 || r.ActivityID != nil || r.ReadAt == nil || r.Seq <= byID[r.ID].Seq {
			t.Fatalf("every fold naming the author is retracted on every site, read ones too: %+v", r)
		}
		if was := byID[r.ID].ReadAt; was != nil && !r.ReadAt.After(*was) {
			t.Fatalf("a retracted read row is read again now, so the 90-day prune cannot take it before mirrors see it: %+v", r)
		}
	}

	t2 := time.Now().Add(-2 * time.Hour)
	writeActivities(t, "kungal", notifyingPublish("r:2", author, t2, 1))
	processBatch(t)
	if left := pendingEvents(t); len(left) != 0 {
		t.Fatalf("the event of a hidden author is consumed: %+v", left)
	}
	if rows := followeeRows(t, "kungal", unread); len(rows) != 1 || rows[0].ItemCount != 0 {
		t.Fatalf("a hidden author's new work notifies nobody: %+v", rows)
	}

	setHidden(t, author, false)
	t3 := time.Now().Add(-time.Hour)
	writeActivities(t, "kungal", notifyingPublish("r:3", author, t3, 1))
	processBatch(t)
	rows := followeeRows(t, "kungal", unread)
	r3 := getActivity(t, "kungal", "r:3")
	if len(rows) != 2 || rows[1].ItemCount != 1 || rows[1].ActivityID == nil || *rows[1].ActivityID != r3.ID {
		t.Fatalf("shown again, the next work starts a new fold without the hidden stretch: %+v", rows)
	}
}

func TestAccountPurgeDropsTheActivitySetting(t *testing.T) {
	cleanTables(t)
	const gone int64 = 9
	setHidden(t, gone, true)
	if err := NewPostService(testDB, NoopSink{}).PurgeAccount(context.Background(), gone); err != nil {
		t.Fatalf("purge account: %v", err)
	}
	var n int64
	testDB.Raw(`SELECT count(*) FROM community_activity_setting WHERE user_id = ?`, gone).Scan(&n)
	if n != 0 {
		t.Fatal("an account purge drops the activity setting")
	}
}

func TestNothingPublishedWhileHiddenNotifies(t *testing.T) {
	cleanTables(t)
	const author, follower int64 = 7, 1
	mustFollow(t, "kungal", follower, author)
	processBatch(t)
	now := time.Now()

	setHidden(t, author, true)
	writeActivities(t, "kungal", notifyingPublish("hidden", author, now.Add(-10*time.Minute), 1))
	setHidden(t, author, false)
	processBatch(t)
	if rows := followeeRows(t, "kungal", follower); len(rows) != 0 {
		t.Fatalf("written while hidden, shown again before the dispatch: %+v", rows)
	}
	if a := getActivity(t, "kungal", "hidden"); a.NotifiedAt != nil {
		t.Fatalf("an activity written while hidden is never notified: %+v", a)
	}

	writeActivities(t, "kungal", notifyingPublish("raced", author, now.Add(-5*time.Minute), 1))
	setHidden(t, author, true)
	processBatch(t)
	if a := getActivity(t, "kungal", "raced"); a.NotifiedAt != nil {
		t.Fatalf("an event dropped for a hidden author forgets its notification: %+v", a)
	}
	setHidden(t, author, false)

	writeActivities(t, "kungal", notifyingPublish("late", author, now.Add(-20*time.Minute), 1))
	processBatch(t)
	rows := followeeRows(t, "kungal", follower)
	late := getActivity(t, "kungal", "late")
	if len(rows) != 1 || rows[0].ItemCount != 1 || rows[0].ActivityID == nil || *rows[0].ActivityID != late.ID {
		t.Fatalf("a fold reaching back past the hidden stretch counts none of it: %+v", rows)
	}
}

func TestHiddenAuthorsTopicNotifiesNoFollower(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ctx := letmoeCtx()
	const follower, author int64 = 10, 20
	boardID := testBoard(t, "letmoe", "b1")
	mustFollow(t, "letmoe", follower, author)
	processBatch(t)
	seedTrust(t, author, model.TrustLevelBasic, 0)
	open := func(title string) {
		t.Helper()
		if _, _, err := ts.OpenTopic(ctx, OpenTopicParams{
			Site: "letmoe", AuthorID: author, BoardID: boardID,
			Title: title, ContentRating: model.ContentRatingAll, BodyRaw: "opening",
		}); err != nil {
			t.Fatalf("open topic: %v", err)
		}
		processBatch(t)
	}

	setHidden(t, author, true)
	open("while hidden")
	if rows := notifsOf(t, follower); len(rows) != 0 {
		t.Fatalf("a hidden author's topic raises no kind 9: %+v", rows)
	}
	setHidden(t, author, false)
	open("shown")
	if rows := notifsOf(t, follower); len(rows) != 1 || rows[0].Kind != model.NotificationKindFolloweeThreadCreated {
		t.Fatalf("shown again, the next topic notifies: %+v", rows)
	}
}
