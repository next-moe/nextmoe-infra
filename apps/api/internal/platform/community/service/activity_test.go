package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"api/internal/platform/community/model"
)

var kungalHosts = []string{"kungal.com", "www.kungal.com"}

func publishInput(key string, actor int64, occurred time.Time, rev int64) ActivityInput {
	return ActivityInput{
		Key: key, ActorID: actor, Verb: "publish", ObjectKind: "galgame_resource", ObjectLabel: "Galgame 资源",
		Title: "title " + key, Excerpt: "excerpt", URL: "https://www.kungal.com/resource/" + key,
		ContentLimit: "sfw", OccurredAt: &occurred, Revision: rev,
	}
}

func tombstoneInput(key string, actor int64, rev int64) ActivityInput {
	return ActivityInput{Key: key, ActorID: actor, Revision: rev, Removed: true}
}

func writeActivities(t *testing.T, site string, items ...ActivityInput) []ActivityResult {
	t.Helper()
	res, err := NewActivityService(testDB).Write(context.Background(), site, kungalHosts, items)
	if err != nil {
		t.Fatalf("write activities: %v", err)
	}
	return res
}

func wantOutcomes(t *testing.T, res []ActivityResult, want ...string) {
	t.Helper()
	if len(res) != len(want) {
		t.Fatalf("want %d results, got %+v", len(want), res)
	}
	for i := range want {
		if res[i].Outcome != want[i] {
			t.Fatalf("result[%d] %q: want %s, got %s (%s)", i, res[i].Key, want[i], res[i].Outcome, res[i].Reason)
		}
	}
}

func getActivity(t *testing.T, site, key string) model.CommunityActivity {
	t.Helper()
	var a model.CommunityActivity
	if err := testDB.Where("site = ? AND key = ?", site, key).Take(&a).Error; err != nil {
		t.Fatalf("activity %s/%s: %v", site, key, err)
	}
	return a
}

func activityGroups(t *testing.T) []model.CommunityActivityGroup {
	t.Helper()
	var rows []model.CommunityActivityGroup
	if err := testDB.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("list groups: %v", err)
	}
	return rows
}

func TestActivityRevisionRule(t *testing.T) {
	cleanTables(t)
	at := time.Now().Add(-48 * time.Hour)

	wantOutcomes(t, writeActivities(t, "kungal", publishInput("topic:1", 7, at, 10)), ActivityCreated)
	wantOutcomes(t, writeActivities(t, "kungal", publishInput("topic:1", 7, at, 10)), ActivityStale)

	older := publishInput("topic:1", 7, at, 9)
	older.Title = "older"
	wantOutcomes(t, writeActivities(t, "kungal", older), ActivityStale)
	if a := getActivity(t, "kungal", "topic:1"); a.Title != "title topic:1" || a.Revision != 10 {
		t.Fatalf("a stale write must change nothing: %+v", a)
	}

	newer := publishInput("topic:1", 7, at, 11)
	newer.Title = "renamed"
	wantOutcomes(t, writeActivities(t, "kungal", newer), ActivityUpdated)

	wantOutcomes(t, writeActivities(t, "kungal", tombstoneInput("topic:1", 7, 11)), ActivityStale)
	wantOutcomes(t, writeActivities(t, "kungal", tombstoneInput("topic:1", 7, 12)), ActivityRemoved)
	a := getActivity(t, "kungal", "topic:1")
	if a.RemovedAt == nil || a.Title != "" || a.Excerpt != "" || a.URL != "" || a.CoverImageHash != nil || a.WorkID != nil {
		t.Fatalf("a tombstone keeps no content: %+v", a)
	}
	if a.Verb != model.ActivityVerbPublish || a.ObjectKind != "galgame_resource" || a.ActorID != 7 {
		t.Fatalf("a tombstone keeps its identity: %+v", a)
	}

	late := publishInput("topic:1", 7, at, 11)
	wantOutcomes(t, writeActivities(t, "kungal", late), ActivityStale)
	if a := getActivity(t, "kungal", "topic:1"); a.RemovedAt == nil {
		t.Fatal("a late upsert must not bring a tombstone back")
	}

	wantOutcomes(t, writeActivities(t, "kungal", publishInput("topic:1", 7, at, 13)), ActivityRestored)
	if a := getActivity(t, "kungal", "topic:1"); a.RemovedAt != nil || a.Title != "title topic:1" {
		t.Fatalf("a newer upsert restores the tombstone: %+v", a)
	}

	wantOutcomes(t, writeActivities(t, "kungal", tombstoneInput("never:seen", 7, 5)), ActivityRemoved)
	wantOutcomes(t, writeActivities(t, "kungal", publishInput("never:seen", 7, at, 4)), ActivityStale)
	if a := getActivity(t, "kungal", "never:seen"); a.RemovedAt == nil {
		t.Fatal("a tombstone for an unseen key must still block its older upsert")
	}

	wantOutcomes(t, writeActivities(t, "moyu", publishInput("topic:1", 8, at, 1)), ActivityCreated)
	if a := getActivity(t, "kungal", "topic:1"); a.ActorID != 7 {
		t.Fatalf("keys are per site: %+v", a)
	}
}

func TestActivityValidationIsPerItem(t *testing.T) {
	cleanTables(t)
	at := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	mut := func(f func(*ActivityInput)) ActivityInput {
		in := publishInput("k:ok", 7, at, 1)
		f(&in)
		return in
	}
	cases := []struct {
		in     ActivityInput
		reason string
	}{
		{mut(func(i *ActivityInput) { i.Key = "bad key" }), "key must be"},
		{mut(func(i *ActivityInput) { i.Key = strings.Repeat("k", 129) }), "key must be"},
		{mut(func(i *ActivityInput) { i.ActorID = 0 }), "actor_id must be positive"},
		{mut(func(i *ActivityInput) { i.Revision = 0 }), "revision must be positive"},
		{mut(func(i *ActivityInput) { i.Revision = time.Now().Add(time.Hour).UnixMicro() }), "revision must be positive and not in the future"},
		{mut(func(i *ActivityInput) { i.OccurredAt = &future }), "occurred_at is in the future"},
		{mut(func(i *ActivityInput) { i.OccurredAt = nil }), "occurred_at is required"},
		{mut(func(i *ActivityInput) { i.Verb = "shout" }), "unknown verb"},
		{mut(func(i *ActivityInput) { i.Verb = "" }), "verb is required"},
		{mut(func(i *ActivityInput) { i.ObjectKind = "Galgame" }), "object_kind must be"},
		{mut(func(i *ActivityInput) { i.ObjectLabel = "" }), "object_label length"},
		{mut(func(i *ActivityInput) { i.ObjectLabel = strings.Repeat("资", 17) }), "object_label length"},
		{mut(func(i *ActivityInput) { i.Title = "   " }), "title length"},
		{mut(func(i *ActivityInput) { i.Title = strings.Repeat("题", 201) }), "title length"},
		{mut(func(i *ActivityInput) { i.Title = "a\x00b" }), "title contains a control character"},
		{mut(func(i *ActivityInput) { i.Excerpt = strings.Repeat("字", 301) }), "excerpt length"},
		{mut(func(i *ActivityInput) { i.URL = "http://www.kungal.com/x" }), "url must be an absolute https URL"},
		{mut(func(i *ActivityInput) { i.URL = "https://moyu.moe/patch/1" }), "url host is not one of this site's registered hosts"},
		{mut(func(i *ActivityInput) { i.URL = "https://evil@www.kungal.com/x" }), "url must be an absolute https URL"},
		{mut(func(i *ActivityInput) { i.CoverImageHash = "abc" }), "cover_image_hash must be"},
		{mut(func(i *ActivityInput) { i.WorkID = new(int64(0)) }), "work_id must be positive"},
		{mut(func(i *ActivityInput) { i.ContentLimit = "r18" }), "content_limit must be"},
		{mut(func(i *ActivityInput) { i.Verb = "reply"; i.Notify = true }), "notify is only for verb publish"},
	}
	for i, c := range cases {
		if !strings.HasPrefix(c.reason, "key must be") {
			c.in.Key = fmt.Sprintf("bad:%d", i)
		}
		res := writeActivities(t, "kungal", c.in, publishInput(fmt.Sprintf("good:%d", i), 7, at, 1))
		if res[0].Outcome != ActivityInvalid || !strings.Contains(res[0].Reason, c.reason) {
			t.Errorf("case %d: want invalid %q, got %+v", i, c.reason, res[0])
		}
		if res[1].Outcome != ActivityCreated {
			t.Errorf("case %d: an invalid item must not block the valid one, got %+v", i, res[1])
		}
	}

	res := writeActivities(t, "kungal", publishInput("dup", 7, at, 1), publishInput("dup", 7, at, 2))
	if res[0].Outcome != ActivityCreated || res[1].Outcome != ActivityInvalid || res[1].Reason != "duplicate key in batch" {
		t.Fatalf("a duplicate key in one batch: %+v", res)
	}

	valid := publishInput("full", 7, at, 1)
	valid.CoverImageHash = strings.Repeat("ab", 32)
	valid.WorkID = new(int64(42))
	valid.Excerpt = "line one\nline two"
	valid.ContentLimit = ""
	wantOutcomes(t, writeActivities(t, "kungal", valid), ActivityCreated)
	if a := getActivity(t, "kungal", "full"); a.ContentLimit != model.ContentLimitNSFW || a.WorkID == nil || *a.WorkID != 42 {
		t.Fatalf("a missing content_limit is nsfw: %+v", a)
	}

	svc := NewActivityService(testDB)
	_, err := svc.Write(context.Background(), "kungal", kungalHosts, nil)
	wantInvalid(t, err, "items must hold 1-100 activities")
	_, err = svc.Write(context.Background(), "kungal", kungalHosts, make([]ActivityInput, 101))
	wantInvalid(t, err, "items must hold 1-100 activities")
}

func TestActivityNotifiesOnlyFreshNewPublications(t *testing.T) {
	cleanTables(t)
	fresh := time.Now().Add(-time.Hour)
	stale := time.Now().Add(-25 * time.Hour)
	notify := func(key string, at time.Time, rev int64) ActivityInput {
		in := publishInput(key, 7, at, rev)
		in.Notify = true
		return in
	}
	publishedEvents := func() int {
		var n int64
		if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindActivityPublished).Count(&n).Error; err != nil {
			t.Fatalf("count events: %v", err)
		}
		return int(n)
	}

	writeActivities(t, "kungal", notify("a", fresh, 1))
	if publishedEvents() != 1 {
		t.Fatal("a fresh new publication notifies")
	}
	ev := pendingEvents(t)[0]
	if ev.Site != "kungal" || ev.ActorID != 7 || ev.ThreadID != 0 || ev.ActivityID == nil {
		t.Fatalf("event 6: %+v", ev)
	}

	writeActivities(t, "kungal", notify("a", fresh, 2))
	writeActivities(t, "kungal", notify("old", stale, 1))
	writeActivities(t, "kungal", publishInput("quiet", 7, fresh, 1))
	writeActivities(t, "kungal", tombstoneInput("gone", 7, 1))
	writeActivities(t, "kungal", notify("gone", fresh, 2))
	if n := publishedEvents(); n != 1 {
		t.Fatalf("updates, backfill, notify=false and restores must not notify, events=%d", n)
	}
	if a := getActivity(t, "kungal", "old"); a.NotifiedAt != nil {
		t.Fatal("a backfilled item is never notified")
	}
}

func TestActivityGroupsFollowTheirMembers(t *testing.T) {
	cleanTables(t)
	evening := time.Date(2026, 9, 25, 15, 30, 0, 0, time.UTC)
	afterMidnightCST := time.Date(2026, 9, 25, 16, 30, 0, 0, time.UTC)

	a := publishInput("r:1", 7, evening, 1)
	b := publishInput("r:2", 7, afterMidnightCST, 1)
	c := publishInput("r:3", 7, afterMidnightCST.Add(time.Minute), 1)
	c.ContentLimit = "nsfw"
	writeActivities(t, "kungal", a, b, c)

	groups := activityGroups(t)
	if len(groups) != 2 {
		t.Fatalf("two Beijing days make two groups, got %+v", groups)
	}
	day25, day26 := groups[0], groups[1]
	if day25.BucketDate.Format(time.DateOnly) != "2026-09-25" || day26.BucketDate.Format(time.DateOnly) != "2026-09-26" {
		t.Fatalf("buckets are Beijing days: %s, %s", day25.BucketDate, day26.BucketDate)
	}
	r3 := getActivity(t, "kungal", "r:3")
	r2 := getActivity(t, "kungal", "r:2")
	if day26.CountAll != 2 || day26.CountSFW != 1 || *day26.LatestAllID != r3.ID || *day26.LatestSFWID != r2.ID ||
		day26.ObjectLabel != "Galgame 资源" {
		t.Fatalf("group counts per view: %+v", day26)
	}

	writeActivities(t, "kungal", tombstoneInput("r:3", 7, 2))
	day26 = activityGroups(t)[1]
	if day26.CountAll != 1 || *day26.LatestAllID != r2.ID {
		t.Fatalf("a tombstone leaves its group: %+v", day26)
	}

	moved := publishInput("r:1", 7, afterMidnightCST.Add(-time.Minute), 2)
	writeActivities(t, "kungal", moved)
	groups = activityGroups(t)
	if len(groups) != 1 || groups[0].BucketDate.Format(time.DateOnly) != "2026-09-26" || groups[0].CountAll != 2 {
		t.Fatalf("a member moved to another day leaves an empty group behind to be deleted: %+v", groups)
	}

	reply := publishInput("r:2", 7, afterMidnightCST, 2)
	reply.Verb, reply.ObjectKind = "comment", "resource_comment"
	writeActivities(t, "kungal", reply)
	groups = activityGroups(t)
	if len(groups) != 2 {
		t.Fatalf("a changed verb moves the item to its own group: %+v", groups)
	}
}

func TestActivityTombstonesArePruned(t *testing.T) {
	cleanTables(t)
	writeActivities(t, "kungal", tombstoneInput("old", 7, 1), tombstoneInput("recent", 7, 1),
		publishInput("live", 7, time.Now().Add(-90*24*time.Hour), 1))
	if err := testDB.Exec(`UPDATE community_activity SET removed_at = now() - interval '31 days' WHERE key = 'old'`).Error; err != nil {
		t.Fatalf("age tombstone: %v", err)
	}
	n, err := NewActivityService(testDB).Prune(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	var keys []string
	testDB.Model(&model.CommunityActivity{}).Order("key").Pluck("key", &keys)
	if strings.Join(keys, ",") != "live,recent" {
		t.Fatalf("left: %v", keys)
	}
}
