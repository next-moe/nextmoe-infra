package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func followEdge(t *testing.T, follower, followee int64, createdAt time.Time) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at)
		VALUES (?, ?, 'kungal', ?)`, follower, followee, createdAt).Error; err != nil {
		t.Fatalf("follow %d->%d: %v", follower, followee, err)
	}
}

func feed(t *testing.T, p ActivityFeedParams) []ActivityGroup {
	t.Helper()
	groups, _, err := NewActivityService(testDB).FollowingFeed(p)
	if err != nil {
		t.Fatalf("following feed: %v", err)
	}
	return groups
}

func TestFollowingFeed(t *testing.T) {
	cleanTables(t)
	const viewer, alice, bob, carol int64 = 1, 10, 20, 30
	long := time.Now().Add(-30 * 24 * time.Hour)
	followEdge(t, viewer, alice, long)
	followEdge(t, viewer, bob, long)

	day := func(d int) time.Time { return time.Date(2026, 9, d, 4, 0, 0, 0, time.UTC) }
	var items []ActivityInput
	for d := 1; d <= 5; d++ {
		items = append(items, publishInput(fmt.Sprintf("a:%d", d), alice, day(d), 1))
	}
	for i := range 5 {
		in := publishInput(fmt.Sprintf("b:%d", i), bob, day(3).Add(time.Duration(i)*time.Minute), 1)
		if i == 4 {
			in.ContentLimit = "nsfw"
		}
		items = append(items, in)
	}
	items = append(items, publishInput("c:1", carol, day(6), 1))
	writeActivities(t, "kungal", items...)
	moyuItem := publishInput("m:1", alice, day(2).Add(time.Hour), 1)
	moyuItem.URL = "https://www.kungal.com/x"
	writeActivities(t, "moyu", moyuItem)

	all := feed(t, ActivityFeedParams{UserID: viewer, Limit: 50})
	if len(all) != 7 {
		t.Fatalf("5 alice days + 1 bob day + 1 alice moyu day, got %d", len(all))
	}
	for _, g := range all {
		if g.ActorID == carol {
			t.Fatal("an author the viewer does not follow must not appear")
		}
	}
	for i := 1; i < len(all); i++ {
		prev, cur := all[i-1], all[i]
		if cur.LatestAt.After(prev.LatestAt) {
			t.Fatalf("groups must be newest first: %v before %v", prev.LatestAt, cur.LatestAt)
		}
	}
	var bobGroup ActivityGroup
	for _, g := range all {
		if g.ActorID == bob {
			bobGroup = g
		}
	}
	if bobGroup.ItemCount != 5 || len(bobGroup.Items) != 3 || bobGroup.Items[0].Key != "b:4" || bobGroup.Items[2].Key != "b:2" {
		t.Fatalf("a group previews its three newest items: %+v", bobGroup)
	}

	sfw := feed(t, ActivityFeedParams{UserID: viewer, SFW: true, Limit: 50})
	for _, g := range sfw {
		if g.ActorID == bob && (g.ItemCount != 4 || g.Items[0].Key != "b:3") {
			t.Fatalf("the sfw view counts and previews sfw items only: %+v", g)
		}
		for _, it := range g.Items {
			if it.ContentLimit != model.ContentLimitSFW {
				t.Fatalf("an nsfw item leaked into the sfw view: %+v", it)
			}
		}
	}

	if only := feed(t, ActivityFeedParams{UserID: viewer, Sites: []string{"moyu"}, Limit: 50}); len(only) != 1 || only[0].Site != "moyu" {
		t.Fatalf("sites filter: %+v", only)
	}
	if none := feed(t, ActivityFeedParams{UserID: viewer, Verbs: []int16{model.ActivityVerbReply}, Limit: 50}); len(none) != 0 {
		t.Fatalf("verbs filter: %+v", none)
	}

	var paged []ActivityGroup
	var cursor *repository.ActivityCursor
	for range 10 {
		page := feed(t, ActivityFeedParams{UserID: viewer, Before: cursor, Limit: 2})
		paged = append(paged, page...)
		if len(page) < 2 {
			break
		}
		last := page[len(page)-1]
		cursor = &repository.ActivityCursor{At: last.LatestAt, ID: last.ID}
	}
	if len(paged) != len(all) {
		t.Fatalf("paging must visit every group once: %d vs %d", len(paged), len(all))
	}
	for i := range all {
		if paged[i].ID != all[i].ID {
			t.Fatalf("page order differs at %d", i)
		}
	}

	writeActivities(t, "kungal", tombstoneInput("a:5", alice, 2))
	if after := feed(t, ActivityFeedParams{UserID: viewer, Limit: 50}); len(after) != 6 {
		t.Fatalf("a tombstoned item's group empties out of the feed, got %d", len(after))
	}

	if _, err := NewFollowService(testDB).Unfollow(context.Background(), viewer, bob); err != nil {
		t.Fatalf("unfollow: %v", err)
	}
	for _, g := range feed(t, ActivityFeedParams{UserID: viewer, Limit: 50}) {
		if g.ActorID == bob {
			t.Fatal("an unfollow takes effect at once")
		}
	}

	items2, _, err := NewActivityService(testDB).GroupItems(bobGroup.ID, false, nil, 2)
	if err != nil || len(items2) != 2 || items2[0].Key != "b:4" {
		t.Fatalf("group items page 1: %+v %v", items2, err)
	}
	rest, _, err := NewActivityService(testDB).GroupItems(bobGroup.ID, false,
		&repository.ActivityCursor{At: items2[1].OccurredAt, ID: items2[1].ID}, 50)
	if err != nil || len(rest) != 3 || rest[0].Key != "b:2" {
		t.Fatalf("group items page 2: %+v %v", rest, err)
	}
	if _, _, err := NewActivityService(testDB).GroupItems(99999, false, nil, 10); err != ErrActivityGroupNotFound {
		t.Fatalf("missing group: %v", err)
	}

	own, _, err := NewActivityService(testDB).ActorFeed(ActivityFeedParams{UserID: alice, Limit: 50})
	if err != nil || len(own) != 5 {
		t.Fatalf("an author's own groups: %d %v", len(own), err)
	}
}

func TestUnseenCountsFromTheLaterOfSeenAndFollow(t *testing.T) {
	cleanTables(t)
	const viewer, old, fresh, imported int64 = 1, 10, 20, 30
	svc := NewActivityService(testDB)
	now := time.Now()
	followEdge(t, viewer, old, now.Add(-10*24*time.Hour))
	followEdge(t, viewer, fresh, now.Add(-time.Hour))
	if err := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site, imported_at)
		VALUES (?, ?, 'moyu', ?)`, viewer, imported, now.Add(-2*time.Hour)).Error; err != nil {
		t.Fatalf("imported edge: %v", err)
	}

	writeActivities(t, "kungal",
		publishInput("old:history", old, now.Add(-20*24*time.Hour), 1),
		publishInput("old:recent", old, now.Add(-2*24*time.Hour), 1),
		publishInput("fresh:history", fresh, now.Add(-5*24*time.Hour), 1),
		publishInput("imported:history", imported, now.Add(-3*time.Hour), 1),
	)
	unseen := func() int {
		t.Helper()
		n, _, err := svc.Unseen(ActivityFeedParams{UserID: viewer})
		if err != nil {
			t.Fatalf("unseen: %v", err)
		}
		return n
	}
	if n := unseen(); n != 1 {
		t.Fatalf("only old:recent is after its follow began, got %d", n)
	}

	writeActivities(t, "kungal", publishInput("imported:now", imported, now.Add(-time.Minute), 1))
	if n := unseen(); n != 2 {
		t.Fatalf("an imported edge counts from its import, got %d", n)
	}

	seen, err := svc.MarkSeen(viewer, nil)
	if err != nil {
		t.Fatalf("mark seen: %v", err)
	}
	if n := unseen(); n != 0 {
		t.Fatalf("after marking, nothing is unseen, got %d", n)
	}
	back := now.Add(-48 * time.Hour)
	if got, err := svc.MarkSeen(viewer, &back); err != nil || !got.Equal(seen) {
		t.Fatalf("the mark never moves back: %v %v", got, err)
	}
	ahead := now.Add(24 * time.Hour)
	got, err := svc.MarkSeen(viewer, &ahead)
	if err != nil || got.After(time.Now().Add(time.Second)) {
		t.Fatalf("the mark never passes now: %v %v", got, err)
	}

	var many []ActivityInput
	for i := range 101 {
		in := publishInput(fmt.Sprintf("burst:%d", i), old, time.Now().Add(time.Duration(i-200)*24*time.Hour), 1)
		many = append(many, in)
	}
	for i := 0; i < len(many); i += 100 {
		writeActivities(t, "moyu", many[i:min(i+100, len(many))]...)
	}
	if err := testDB.Exec(`UPDATE community_feed_seen SET seen_at = ? WHERE user_id = ?`, now.Add(-400*24*time.Hour), viewer).Error; err != nil {
		t.Fatalf("rewind: %v", err)
	}
	if err := testDB.Exec(`UPDATE community_user_follow SET created_at = ? WHERE followee_id = ?`, now.Add(-400*24*time.Hour), old).Error; err != nil {
		t.Fatalf("rewind edge: %v", err)
	}
	if n := unseen(); n != activityUnseenCap {
		t.Fatalf("the count stops at %d, got %d", activityUnseenCap, n)
	}
}

func TestTombstonesLeaveGroupPreviewsAndItems(t *testing.T) {
	cleanTables(t)
	const viewer, author int64 = 1, 7
	followEdge(t, viewer, author, time.Now().Add(-30*24*time.Hour))
	at := time.Date(2026, 9, 20, 4, 0, 0, 0, time.UTC)
	writeActivities(t, "kungal",
		publishInput("r:1", author, at, 1),
		publishInput("r:2", author, at.Add(time.Minute), 1),
		publishInput("r:3", author, at.Add(2*time.Minute), 1))
	writeActivities(t, "kungal", tombstoneInput("r:3", author, 2))

	groups := feed(t, ActivityFeedParams{UserID: viewer, Limit: 50})
	if len(groups) != 1 || groups[0].ItemCount != 2 || len(groups[0].Items) != 2 || groups[0].Items[0].Key != "r:2" {
		t.Fatalf("a tombstone leaves the group's preview: %+v", groups)
	}
	items, _, err := NewActivityService(testDB).GroupItems(groups[0].ID, false, nil, 50)
	if err != nil || len(items) != 2 || items[0].Key != "r:2" {
		t.Fatalf("a tombstone leaves the group's items: %+v %v", items, err)
	}
}
