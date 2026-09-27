package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

var letmoeRules = []ProjectionRule{
	{AnchorKind: model.AnchorKindBoard, Role: RoleTopic, Verb: "publish", ObjectKind: "topic", ObjectLabel: "话题", Notify: true},
	{AnchorKind: model.AnchorKindBoard, Role: RoleReply, Verb: "reply", ObjectKind: "topic_reply", ObjectLabel: "回复"},
}

var kungalRules = []ProjectionRule{
	{AnchorKind: model.AnchorKindSiteGame, Role: RoleComment, Verb: "comment", ObjectKind: "galgame_comment", ObjectLabel: "Galgame 评论"},
	{AnchorKind: model.AnchorKindSiteResource, Role: RoleComment, Verb: "comment", ObjectKind: "galgame_resource_comment", ObjectLabel: "资源评论"},
	{AnchorKind: model.AnchorKindSiteResource, Prefix: "rating:", Role: RoleComment, Verb: "comment", ObjectKind: "galgame_rating_comment", ObjectLabel: "评分评论"},
}

func enableSite(t *testing.T, site, threadURL, fragment string, rules []ProjectionRule) {
	t.Helper()
	raw, _ := json.Marshal(rules)
	if err := testDB.Exec(`
		INSERT INTO community_activity_site (site, enabled, thread_url, post_fragment, rules, updated_at)
		VALUES (?, true, ?, ?, ?, now())
		ON CONFLICT (site) DO UPDATE SET enabled = true, thread_url = EXCLUDED.thread_url,
		    post_fragment = EXCLUDED.post_fragment, rules = EXCLUDED.rules, updated_at = now()`,
		site, threadURL, fragment, string(raw)).Error; err != nil {
		t.Fatalf("enable %s: %v", site, err)
	}
}

func project(t *testing.T) {
	t.Helper()
	svc := NewActivityService(testDB)
	for range 100 {
		n, err := svc.ProjectBatch(context.Background())
		if err != nil {
			t.Fatalf("project: %v", err)
		}
		if n == 0 {
			return
		}
	}
	t.Fatal("the projection queue never drained")
}

func ownActivity(t *testing.T, site string, postID int64) *model.CommunityActivity {
	t.Helper()
	var rows []model.CommunityActivity
	if err := testDB.Where("site = ? AND key = ?", site, ownActivityKey(postID)).Find(&rows).Error; err != nil {
		t.Fatalf("own activity: %v", err)
	}
	if len(rows) == 0 {
		return nil
	}
	return &rows[0]
}

func openingPost(t *testing.T, threadID int64) *model.CommunityPost {
	t.Helper()
	var p model.CommunityPost
	if err := testDB.Where("thread_id = ? AND post_number = 1", threadID).Take(&p).Error; err != nil {
		t.Fatalf("opening post of %d: %v", threadID, err)
	}
	return &p
}

func kungalComment(t *testing.T, anchorKind int16, anchorID string, author int64, body string) *model.CommunityPost {
	t.Helper()
	seedTrust(t, author, model.TrustLevelBasic, 0)
	_, p, err := NewPostService(testDB, NoopSink{}).Comment(WithCallerSite(context.Background(), "kungal"), CommentParams{
		Site: "kungal", AnchorKind: anchorKind, AnchorID: anchorID, AuthorID: author, BodyRaw: body,
	})
	if err != nil {
		t.Fatalf("comment: %v", err)
	}
	return p
}

func present(t *testing.T, items ...AnchorPresentationInput) []AnchorPresentationResult {
	t.Helper()
	res, err := NewActivityService(testDB).WritePresentations(context.Background(), "kungal", kungalHosts, items)
	if err != nil {
		t.Fatalf("write presentations: %v", err)
	}
	return res
}

func TestProjectionRulePicksTheLongestPrefix(t *testing.T) {
	site := projectionSite{enabled: true, rules: kungalRules}
	if r, ok := site.rule(model.AnchorKindSiteResource, "rating:45", RoleComment); !ok || r.ObjectKind != "galgame_rating_comment" {
		t.Fatalf("rating anchor: %+v %v", r, ok)
	}
	if r, ok := site.rule(model.AnchorKindSiteResource, "resource:45", RoleComment); !ok || r.ObjectKind != "galgame_resource_comment" {
		t.Fatalf("other resource anchor: %+v %v", r, ok)
	}
	if _, ok := site.rule(model.AnchorKindBoard, "1", RoleTopic); ok {
		t.Fatal("no rule, no activity")
	}
	if got := plainExcerpt("<p>hello <strong>world</strong></p>\n<p>a &amp; b\x01</p>", 300); got != "hello world a & b" {
		t.Fatalf("excerpt: %q", got)
	}
	if got := plainExcerpt("<p>一二三四五</p>", 3); got != "一二三" {
		t.Fatalf("excerpt cuts by rune: %q", got)
	}
}

func TestBoardTopicsProjectAndNotifyAsKind10(t *testing.T) {
	cleanTables(t)
	const author, replier, follower int64 = 20, 21, 10
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", "#post-{post_id}", letmoeRules)
	mustFollow(t, "letmoe", follower, author)
	processBatch(t)

	th := openTopic(t, ts, "letmoe", author, "b1", "hello **world**")
	project(t)
	opening := openingPost(t, th.ID)
	a := ownActivity(t, "letmoe", opening.ID)
	if a == nil || a.Verb != model.ActivityVerbPublish || a.ObjectKind != "topic" || a.ObjectLabel != "话题" ||
		a.Title != "t" || a.Excerpt != "hello world" || a.ActorID != author || a.WorkID != nil ||
		a.URL != fmt.Sprintf("https://www.letmoe.com/community/%d", th.ID) ||
		a.ContentLimit != model.ContentLimitSFW || !a.Notify || a.NotifiedAt == nil {
		t.Fatalf("topic activity: %+v", a)
	}
	processBatch(t)
	rows := notifsOfSite(t, "letmoe", follower)
	if len(rows) != 1 || rows[0].Kind != model.NotificationKindFolloweeActivity {
		t.Fatalf("a topic notifies as kind 10 once the site projects its topics, not as kind 9: %+v", rows)
	}

	reply := visibleReply(t, ps, th.ID, replier)
	project(t)
	r := ownActivity(t, "letmoe", reply.ID)
	if r == nil || r.Verb != model.ActivityVerbReply || r.ObjectKind != "topic_reply" || r.Notify ||
		r.URL != fmt.Sprintf("https://www.letmoe.com/community/%d#post-%d", th.ID, reply.ID) {
		t.Fatalf("reply activity: %+v", r)
	}

	if err := testDB.Exec(`UPDATE community_post SET status = ? WHERE id = ?`, model.PostStatusHidden, reply.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if r := ownActivity(t, "letmoe", reply.ID); r == nil || r.RemovedAt == nil {
		t.Fatalf("a hidden reply is tombstoned: %+v", r)
	}
	if err := testDB.Exec(`UPDATE community_post SET status = ? WHERE id = ?`, model.PostStatusVisible, reply.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if r := ownActivity(t, "letmoe", reply.ID); r == nil || r.RemovedAt != nil || r.NotifiedAt != nil {
		t.Fatalf("shown again, it is restored and still never notifies: %+v", r)
	}

	if err := testDB.Exec(`UPDATE community_thread SET status = ? WHERE id = ?`, model.ThreadStatusHidden, th.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	for _, id := range []int64{opening.ID, reply.ID} {
		if a := ownActivity(t, "letmoe", id); a == nil || a.RemovedAt == nil {
			t.Fatalf("hiding the thread tombstones every post of it: %+v", a)
		}
	}
	listed, _, err := NewActivityService(testDB).ListOwn("letmoe", 0, 100)
	if err != nil || len(listed) != 0 {
		t.Fatalf("the site's reconciliation read never lists community's own rows: %+v %v", listed, err)
	}
}

func TestCommentWallsWaitForTheirPresentation(t *testing.T) {
	cleanTables(t)
	enableSite(t, "kungal", "", "#comment-{post_id}", kungalRules)
	const author int64 = 30
	work := int64(9001)
	now := time.Now()

	game := kungalComment(t, model.AnchorKindSiteGame, "123", author, "nice game")
	project(t)
	if a := ownActivity(t, "kungal", game.ID); a != nil {
		t.Fatalf("no presentation, no activity: %+v", a)
	}

	wantPresented := func(res []AnchorPresentationResult, want ...string) {
		t.Helper()
		for i := range want {
			if res[i].Outcome != want[i] {
				t.Fatalf("presentation %d: want %s, got %+v", i, want[i], res[i])
			}
		}
	}
	wantPresented(present(t,
		AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "123", Title: "千恋＊万花",
			URL: "https://www.kungal.com/galgame/123", WorkID: &work, ContentLimit: "nsfw", Revision: now.UnixMicro()},
		AnchorPresentationInput{AnchorKind: model.AnchorKindSiteResource, AnchorID: "rating:45", Title: "千恋＊万花 的评分",
			URL: "https://www.kungal.com/galgame-rating/45", ContentLimit: "sfw", Revision: now.UnixMicro()},
		AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "124", Title: "x",
			URL: "https://evil.example/galgame/124", Revision: now.UnixMicro()},
		AnchorPresentationInput{AnchorKind: model.AnchorKindBoard, AnchorID: "1", Title: "x",
			URL: "https://www.kungal.com/x", Revision: now.UnixMicro()},
	), ActivityCreated, ActivityCreated, ActivityInvalid, ActivityInvalid)
	project(t)
	a := ownActivity(t, "kungal", game.ID)
	if a == nil || a.Verb != model.ActivityVerbComment || a.ObjectKind != "galgame_comment" || a.Title != "千恋＊万花" ||
		a.URL != fmt.Sprintf("https://www.kungal.com/galgame/123#comment-%d", game.ID) || a.WorkID == nil || *a.WorkID != work ||
		a.ContentLimit != model.ContentLimitNSFW || a.Excerpt != "nice game" || a.Notify {
		t.Fatalf("the presentation arriving projects the wall's comments: %+v", a)
	}

	rating := kungalComment(t, model.AnchorKindSiteResource, "rating:45", author, "agree")
	project(t)
	if r := ownActivity(t, "kungal", rating.ID); r == nil || r.ObjectKind != "galgame_rating_comment" || r.ContentLimit != model.ContentLimitSFW {
		t.Fatalf("the prefixed rule wins and an sfw page stays sfw: %+v", r)
	}

	wantPresented(present(t, AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "123", Title: "千恋＊万花 Renewal",
		URL: "https://www.kungal.com/galgame/123", WorkID: &work, ContentLimit: "nsfw", Revision: now.UnixMicro() - 1}), ActivityStale)
	wantPresented(present(t, AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "123", Title: "千恋＊万花 Renewal",
		URL: "https://www.kungal.com/galgame/123", WorkID: &work, ContentLimit: "nsfw", Revision: now.UnixMicro()}), ActivityStale)
	wantPresented(present(t, AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "123", Title: "千恋＊万花 Renewal",
		URL: "https://www.kungal.com/galgame/123", WorkID: &work, ContentLimit: "nsfw", Revision: now.UnixMicro() + 1}), ActivityUpdated)
	project(t)
	if a := ownActivity(t, "kungal", game.ID); a == nil || a.Title != "千恋＊万花 Renewal" {
		t.Fatalf("a rename reaches the wall's activities: %+v", a)
	}

	wantPresented(present(t, AnchorPresentationInput{AnchorKind: model.AnchorKindSiteGame, AnchorID: "123",
		Revision: now.UnixMicro() + 2, Removed: true}), ActivityRemoved)
	project(t)
	if a := ownActivity(t, "kungal", game.ID); a == nil || a.RemovedAt == nil {
		t.Fatalf("a removed page takes its comments out of the feed: %+v", a)
	}

	stored, limit, err := NewActivityService(testDB).ListPresentations("kungal", nil, 1)
	if err != nil || limit != 1 || len(stored) != 1 || stored[0].AnchorID != "123" || stored[0].RemovedAt == nil || stored[0].Title != "" {
		t.Fatalf("reconciliation read, first page: %+v %v", stored, err)
	}
	rest, _, err := NewActivityService(testDB).ListPresentations("kungal",
		&repository.AnchorPresentationCursor{AnchorKind: stored[0].AnchorKind, AnchorID: stored[0].AnchorID}, 10)
	if err != nil || len(rest) != 1 || rest[0].AnchorID != "rating:45" {
		t.Fatalf("reconciliation read, second page: %+v %v", rest, err)
	}
}

func TestEnablingASiteBackfillsWithoutNotifying(t *testing.T) {
	cleanTables(t)
	const author, follower int64 = 20, 10
	ts := NewThreadService(testDB, NoopSink{})
	mustFollow(t, "letmoe", follower, author)
	processBatch(t)
	th := openTopic(t, ts, "letmoe", author, "b1", "before")
	processBatch(t)
	project(t)
	opening := openingPost(t, th.ID)
	if a := ownActivity(t, "letmoe", opening.ID); a != nil {
		t.Fatalf("a site without a row projects nothing: %+v", a)
	}

	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", "#post-{post_id}", letmoeRules)
	project(t)
	a := ownActivity(t, "letmoe", opening.ID)
	if a == nil || a.RemovedAt != nil || a.NotifiedAt != nil {
		t.Fatalf("enabling backfills the site's posts, without notifying, even a fresh topic: %+v", a)
	}
	processBatch(t)
	for _, n := range notifsOfSite(t, "letmoe", follower) {
		if n.Kind == model.NotificationKindFolloweeActivity {
			t.Fatalf("a backfill raised kind 10: %+v", n)
		}
	}

	if err := testDB.Exec(`UPDATE community_activity_site SET enabled = false WHERE site = 'letmoe'`).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.RemovedAt == nil {
		t.Fatalf("switching a site off tombstones what it projected: %+v", a)
	}
}

func TestAMovedThreadTakesItsActivitiesAlong(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", "#post-{post_id}", letmoeRules)
	enableSite(t, "moyu", "https://www.moyu.moe/community/{thread_id}", "#post-{post_id}", letmoeRules)
	th := openTopic(t, ts, "letmoe", 20, "b1", "moving")
	project(t)
	opening := openingPost(t, th.ID)
	if err := testDB.Exec(`UPDATE community_thread SET site = 'moyu' WHERE id = ?`, th.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.RemovedAt == nil {
		t.Fatalf("the old site's copy is tombstoned: %+v", a)
	}
	if a := ownActivity(t, "moyu", opening.ID); a == nil || a.RemovedAt != nil ||
		a.URL != fmt.Sprintf("https://www.moyu.moe/community/%d", th.ID) {
		t.Fatalf("the new site holds it: %+v", a)
	}
}

func TestPurgedAuthorsPostsStayOutOfTheFeed(t *testing.T) {
	cleanTables(t)
	const author int64 = 20
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", "#post-{post_id}", letmoeRules)
	th := openTopic(t, ts, "letmoe", 99, "b1", "host")
	reply := visibleReply(t, ps, th.ID, author)
	project(t)
	if a := ownActivity(t, "letmoe", reply.ID); a == nil || a.RemovedAt != nil {
		t.Fatalf("setup: %+v", a)
	}
	if _, err := ps.PurgeAuthor(context.Background(), "letmoe", author); err != nil {
		t.Fatalf("purge: %v", err)
	}
	project(t)
	var n int64
	testDB.Raw(`SELECT count(*) FROM community_activity WHERE actor_id = ? AND removed_at IS NULL`, author).Scan(&n)
	if n != 0 {
		t.Fatal("the projection must not bring a purged author's activities back")
	}
}

func TestSitesCannotWriteCommunityKeys(t *testing.T) {
	cleanTables(t)
	res := writeActivities(t, "kungal", publishInput("community:post:1", 7, time.Now().Add(-time.Hour), 1))
	if res[0].Outcome != ActivityInvalid {
		t.Fatalf("a site write under the reserved prefix is refused: %+v", res)
	}
}
