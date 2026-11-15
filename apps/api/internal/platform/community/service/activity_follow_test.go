package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"api/internal/platform/community/model"
)

func TestActivityObjectRefLetsURLAndTitleGo(t *testing.T) {
	cleanTables(t)
	at := time.Now().Add(-time.Hour)
	ref := func(key, objectRef string) ActivityInput {
		in := publishInput(key, 7, at, 1)
		in.Verb, in.ObjectKind, in.ObjectLabel = "mark", "work_done", "作品"
		in.URL, in.Title, in.ObjectRef = "", "", objectRef
		return in
	}
	for _, bad := range []string{"work", "work:", "work:0", "work:-1", "game:1", "Work:1", "work:1x", "work:12345678901234567890"} {
		res := writeActivities(t, "catalog", ref("bad:"+bad, bad))
		if res[0].Outcome != ActivityInvalid || !strings.Contains(res[0].Reason, "object_ref must be") {
			t.Errorf("object_ref %q: want invalid, got %+v", bad, res[0])
		}
	}
	noRef := ref("noref", "")
	noRef.Title = "a title"
	res := writeActivities(t, "catalog", noRef)
	if res[0].Outcome != ActivityInvalid || res[0].Reason != "url is required unless object_ref is set" {
		t.Fatalf("no url and no object_ref: %+v", res[0])
	}
	noRef.URL, noRef.Title = "https://www.kungal.com/x", ""
	res = writeActivities(t, "catalog", noRef)
	if res[0].Outcome != ActivityInvalid || !strings.Contains(res[0].Reason, "title length") {
		t.Fatalf("no object_ref keeps title required: %+v", res[0])
	}

	wantOutcomes(t, writeActivities(t, "catalog", ref("ws:7:42:2", "work:42")), ActivityCreated)
	a := getActivity(t, "catalog", "ws:7:42:2")
	if a.ObjectRef == nil || *a.ObjectRef != "work:42" || a.URL != "" || a.Title != "" || a.Verb != model.ActivityVerbMark {
		t.Fatalf("an object_ref item stores without url or title: %+v", a)
	}

	withURL := ref("topic_favorite:7:9", "topic:9")
	withURL.Verb, withURL.ObjectKind, withURL.Title = "favorite", "topic", "a topic"
	withURL.URL = "https://moyu.moe/topic/9"
	res = writeActivities(t, "kungal", withURL)
	if res[0].Outcome != ActivityInvalid || !strings.Contains(res[0].Reason, "url host") {
		t.Fatalf("a url sent beside object_ref is still host-checked: %+v", res[0])
	}
	withURL.URL = "https://www.kungal.com/topic/9"
	wantOutcomes(t, writeActivities(t, "kungal", withURL), ActivityCreated)

	wantOutcomes(t, writeActivities(t, "catalog", tombstoneInput("ws:7:42:2", 7, 2)), ActivityRemoved)
	if a := getActivity(t, "catalog", "ws:7:42:2"); a.ObjectRef != nil {
		t.Fatalf("a tombstone clears object_ref: %+v", a)
	}
}

func followItems(t *testing.T) map[string]model.CommunityActivity {
	t.Helper()
	var rows []model.CommunityActivity
	if err := testDB.Where("key LIKE 'community:follow:%'").Find(&rows).Error; err != nil {
		t.Fatalf("list follow items: %v", err)
	}
	out := map[string]model.CommunityActivity{}
	for _, r := range rows {
		out[r.Site+"/"+r.Key] = r
	}
	return out
}

func TestFollowProjectsIntoTheFollowersFeed(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()

	if _, err := s.Follow(ctx, "kungal", 1, 2); err != nil {
		t.Fatal(err)
	}
	items := followItems(t)
	a, ok := items["kungal/community:follow:1:2"]
	if !ok || len(items) != 1 {
		t.Fatalf("a new follow is one item under the site it was made on: %+v", items)
	}
	if a.ActorID != 1 || a.Verb != model.ActivityVerbFollow || a.ObjectKind != "user" || a.ObjectRef == nil ||
		*a.ObjectRef != "user:2" || a.URL != "" || a.Notify || a.ContentLimit != model.ContentLimitSFW || a.RemovedAt != nil {
		t.Fatalf("follow item shape: %+v", a)
	}
	if groups := activityGroups(t); len(groups) != 1 || groups[0].CountAll != 1 || groups[0].Verb != model.ActivityVerbFollow {
		t.Fatalf("the item lands in the follower's follow group: %+v", groups)
	}
	var events int64
	if err := testDB.Model(&model.CommunityEvent{}).Where("kind = ?", model.EventKindActivityPublished).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("a follow item never notifies, got %d activity_published events", events)
	}

	if _, err := s.Follow(ctx, "moyu", 1, 2); err != nil {
		t.Fatal(err)
	}
	if items := followItems(t); len(items) != 1 || items["kungal/community:follow:1:2"].Revision != a.Revision {
		t.Fatalf("following again changes nothing: %+v", items)
	}

	if _, err := s.Unfollow(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}
	if a := followItems(t)["kungal/community:follow:1:2"]; a.RemovedAt == nil || a.ObjectRef != nil {
		t.Fatalf("an unfollow tombstones the item: %+v", a)
	}
	if groups := activityGroups(t); len(groups) != 0 {
		t.Fatalf("the emptied group goes: %+v", groups)
	}
	if _, err := s.Unfollow(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Follow(ctx, "moyu", 1, 2); err != nil {
		t.Fatal(err)
	}
	items = followItems(t)
	if items["kungal/community:follow:1:2"].RemovedAt == nil || items["moyu/community:follow:1:2"].RemovedAt != nil {
		t.Fatalf("a follow made again on another site lives there: %+v", items)
	}

	if _, err := s.Follow(ctx, "kungal", 2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Block(ctx, "kungal", 2, 1); err != nil {
		t.Fatal(err)
	}
	for k, a := range followItems(t) {
		if a.RemovedAt == nil {
			t.Fatalf("a block retracts both directions' items, %s is live", k)
		}
	}
}

func TestPurgeAccountRetractsFollowsOfIt(t *testing.T) {
	cleanTables(t)
	s := NewFollowService(testDB)
	ctx := context.Background()
	const gone = 5
	for _, follower := range []int64{1, 2} {
		if _, err := s.Follow(ctx, "kungal", follower, gone); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Follow(ctx, "kungal", 1, 3); err != nil {
		t.Fatal(err)
	}
	if err := NewPostService(testDB, NoopSink{}).PurgeAccount(ctx, gone); err != nil {
		t.Fatal(err)
	}
	items := followItems(t)
	for _, k := range []string{"kungal/community:follow:1:5", "kungal/community:follow:2:5"} {
		if items[k].RemovedAt == nil {
			t.Fatalf("%s must be retracted with the account: %+v", k, items[k])
		}
	}
	if items["kungal/community:follow:1:3"].RemovedAt != nil {
		t.Fatal("a follow of someone else stays")
	}
}
