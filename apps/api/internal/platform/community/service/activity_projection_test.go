package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

var letmoeRules = []ProjectionRule{
	{AnchorKind: model.AnchorKindBoard, Role: RoleTopic, Verb: "publish", ObjectKind: "topic", ObjectLabel: "话题", Notify: true},
	{AnchorKind: model.AnchorKindBoard, Role: RoleReply, Verb: "reply", ObjectKind: "topic_reply", ObjectLabel: "回复", Fragment: "#post-{post_id}"},
}

var kungalRules = []ProjectionRule{
	{AnchorKind: model.AnchorKindSiteGame, Role: RoleComment, Verb: "comment", ObjectKind: "galgame_comment", ObjectLabel: "Galgame 评论", Fragment: "?comment={post_id}"},
	{AnchorKind: model.AnchorKindSiteResource, Role: RoleComment, Verb: "comment", ObjectKind: "galgame_resource_comment", ObjectLabel: "资源评论"},
	{AnchorKind: model.AnchorKindSiteResource, Prefix: "rating:", Role: RoleComment, Verb: "comment", ObjectKind: "galgame_rating_comment", ObjectLabel: "评分评论"},
	{AnchorKind: model.AnchorKindSiteResource, Prefix: "quiz:", Role: RoleComment, Verb: "comment", ObjectKind: "galgame_quiz_comment", ObjectLabel: "题目评论", NoExcerpt: true},
}

func enableSite(t *testing.T, site, threadURL string, rules []ProjectionRule) {
	t.Helper()
	raw, _ := json.Marshal(rules)
	if err := testDB.Exec(`
		INSERT INTO community_activity_site (site, enabled, thread_url, rules, updated_at)
		VALUES (?, true, ?, ?, now())
		ON CONFLICT (site) DO UPDATE SET enabled = true, thread_url = EXCLUDED.thread_url,
		    rules = EXCLUDED.rules, updated_at = now()`,
		site, threadURL, string(raw)).Error; err != nil {
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
	for _, c := range []struct{ in, want string }{
		{"hello **world**\n\na & b\x01 ![cover](https://x/y.webp) [link](https://x)", "hello world a & b link"},
		{"凶手是 ||犯人A|| 对吧", "凶手是 ███ 对吧"},
		{"||跨\n行|| 还有", "███ 还有"},
		{"前\n:::spoiler\n第一段\n\n第二段\n:::\n后", "前 ███ 后"},
		{":::spoiler\n没有结尾", "███"},
		{"- 列表\n- :::spoiler\n  列表里的\n  :::\n- 后", "列表 ███ 后"},
		{"> :::spoiler\n> 引用里的\n> :::\n\n后", "███ 后"},
		{"::::spoiler\n外层秘密\n:::spoiler\n内层\n:::\n外层还有\n::::\n后", "███ 后"},
		{":::spoiler\n一\n:::spoiler\n二\n:::\n三\n:::\n后", "███ 三 ::: 后"},
		{"::::spoiler\n秘密\n:::\n还是秘密\n::::\n后", "███ 后"},
		{"||use `a || b` here secret||", "███"},
		{"||看 [链接](https://x/a||b) 秘密||", "███"},
		{"> 回复 [#3楼](kungal-reply:12)\n[@kun](kungal-user:1) 同意", "同意"},
		{"一二三四五", "一二三"},
	} {
		limit := 300
		if c.want == "一二三" {
			limit = 3
		}
		if got := plainExcerpt(c.in, limit); got != c.want {
			t.Errorf("excerpt of %q: got %q, want %q", c.in, got, c.want)
		}
	}
	for _, in := range []string{
		":::spoiler\nA\n- :::\nsecret\n:::",
		":::spoiler\nA\n1. :::\nsecret\n:::",
		"- :::spoiler\n  > :::\n  secret\n  :::",
		":::spoiler\nA\n    :::\nsecret\n:::",
		":::spoiler\nA\n\t:::\nsecret\n:::",
		"> :::spoiler\n> A\n:::\nsecret\n> :::",
		"||secret :::spoiler more||",
		"||secret\nmore :::spoiler x||",
		"前 <span class=\"kun-spoiler text-transparent\">secret</span> 后",
		"<details><summary>点开</summary>secret</details>",
		":::spoiler\r\nsecret\r\n:::\r\n",
		"||use `a || b` here secret||",
		"||secret $x||y$ more||",
		"||a $$x||y$$ secret||",
		`<p>a<div class="kun-spoiler">s</p> secret</div>`,
		"<noscript>secret</noscript>",
		`<span class="kun-spoiler"><table><tr><td></div>secret</td></tr></table></span>`,
	} {
		if got := plainExcerpt(in, 300); strings.Contains(got, "secret") {
			t.Errorf("spoiler leaked from %q: %q", in, got)
		}
	}
}

func TestBoardTopicsProjectAndNotifyAsKind10(t *testing.T) {
	cleanTables(t)
	const author, replier, follower int64 = 20, 21, 10
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
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
	enableSite(t, "kungal", "", kungalRules)
	const author int64 = 30
	work := int64(9001)
	cover := strings.Repeat("ab", 32)
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
			URL: "https://www.kungal.com/galgame/123", WorkID: &work, CoverImageHash: cover, ContentLimit: "nsfw", Revision: now.UnixMicro()},
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
		a.URL != fmt.Sprintf("https://www.kungal.com/galgame/123?comment=%d", game.ID) || a.CoverImageHash == nil || *a.CoverImageHash != cover || a.WorkID == nil || *a.WorkID != work ||
		a.ContentLimit != model.ContentLimitNSFW || a.Excerpt != "nice game" || a.Notify {
		t.Fatalf("the presentation arriving projects the wall's comments: %+v", a)
	}

	rating := kungalComment(t, model.AnchorKindSiteResource, "rating:45", author, "agree")
	project(t)
	if r := ownActivity(t, "kungal", rating.ID); r == nil || r.ObjectKind != "galgame_rating_comment" ||
		r.ContentLimit != model.ContentLimitSFW || r.URL != "https://www.kungal.com/galgame-rating/45" || r.CoverImageHash != nil {
		t.Fatalf("the prefixed rule wins, an sfw page stays sfw, and a rule without a fragment adds none: %+v", r)
	}

	wantPresented(present(t, AnchorPresentationInput{AnchorKind: model.AnchorKindSiteResource, AnchorID: "quiz:7", Title: "题目",
		URL: "https://www.kungal.com/galgame-quiz/7", ContentLimit: "sfw", Revision: now.UnixMicro()}), ActivityCreated)
	quiz := kungalComment(t, model.AnchorKindSiteResource, "quiz:7", author, "the answer is B")
	project(t)
	if q := ownActivity(t, "kungal", quiz.ID); q == nil || q.Excerpt != "" || q.ObjectKind != "galgame_quiz_comment" {
		t.Fatalf("a quiz wall's comments carry no excerpt, it would give the answer away: %+v", q)
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
	if err != nil || len(rest) != 2 || rest[0].AnchorID != "quiz:7" || rest[1].AnchorID != "rating:45" {
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

	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
	// Touched before the backfill drained: still written before the switch,
	// and its followers already had kind 9 for it.
	if err := testDB.Exec(`UPDATE community_post SET content_raw = 'edited' WHERE id = ?`, opening.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	a := ownActivity(t, "letmoe", opening.ID)
	if a == nil || a.RemovedAt != nil || a.NotifiedAt != nil || a.Excerpt != "edited" {
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
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
	enableSite(t, "moyu", "https://www.moyu.moe/community/{thread_id}", letmoeRules)
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
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
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

func kinds(rows []model.CommunityNotification) []int16 {
	out := make([]int16, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out
}

func TestKind9StaysWhenNoActivityWillNotify(t *testing.T) {
	const author, follower int64 = 20, 10
	ts := NewThreadService(testDB, NoopSink{})

	t.Run("the projection cannot shape the topic", func(t *testing.T) {
		cleanTables(t)
		enableSite(t, "letmoe", "", letmoeRules)
		mustFollow(t, "letmoe", follower, author)
		processBatch(t)
		th := openTopic(t, ts, "letmoe", author, "b1", "no url")
		processBatch(t)
		project(t)
		if a := ownActivity(t, "letmoe", openingPost(t, th.ID).ID); a != nil {
			t.Fatalf("setup: no thread URL, no activity: %+v", a)
		}
		if got := kinds(notifsOfSite(t, "letmoe", follower)); len(got) != 1 || got[0] != model.NotificationKindFolloweeThreadCreated {
			t.Fatalf("kind 9 must stay when no kind 10 will come: %v", got)
		}

		if err := testDB.Exec(`UPDATE community_activity_site SET thread_url = 'https://www.letmoe.com/community/{thread_id}'`).Error; err != nil {
			t.Fatal(err)
		}
		project(t)
		processBatch(t)
		if a := ownActivity(t, "letmoe", openingPost(t, th.ID).ID); a == nil || a.NotifiedAt != nil {
			t.Fatalf("fixing the URL projects the topic, without notifying: %+v", a)
		}
		if got := kinds(notifsOfSite(t, "letmoe", follower)); len(got) != 1 {
			t.Fatalf("a topic announced by kind 9 is not announced again by kind 10: %v", got)
		}
	})

	t.Run("the topic is over a day old when it is dispatched", func(t *testing.T) {
		cleanTables(t)
		enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
		mustFollow(t, "letmoe", follower, author)
		processBatch(t)
		if err := testDB.Exec(`UPDATE community_activity_site SET notify_after = now() - interval '40 hours'`).Error; err != nil {
			t.Fatal(err)
		}
		th := openTopic(t, ts, "letmoe", author, "b1", "approved late")
		opening := openingPost(t, th.ID)
		if err := testDB.Exec(`UPDATE community_post SET created_at = now() - interval '30 hours' WHERE id = ?`, opening.ID).Error; err != nil {
			t.Fatal(err)
		}
		processBatch(t)
		project(t)
		processBatch(t)
		if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.NotifiedAt != nil {
			t.Fatalf("setup: a day-old topic is projected without notifying: %+v", a)
		}
		if got := kinds(notifsOfSite(t, "letmoe", follower)); len(got) != 1 || got[0] != model.NotificationKindFolloweeThreadCreated {
			t.Fatalf("a topic approved a day late still raises kind 9, and only that: %v", got)
		}
	})

	t.Run("the site's rules do not parse", func(t *testing.T) {
		cleanTables(t)
		enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
		mustFollow(t, "letmoe", follower, author)
		processBatch(t)
		first := openTopic(t, ts, "letmoe", author, "b1", "before the typo")
		processBatch(t)
		project(t)
		if err := testDB.Exec(`UPDATE community_activity_site SET rules = '[{"anchor_kind":"board"}]' WHERE site = 'letmoe'`).Error; err != nil {
			t.Fatal(err)
		}
		project(t)
		if a := ownActivity(t, "letmoe", openingPost(t, first.ID).ID); a == nil || a.RemovedAt != nil {
			t.Fatalf("a typo in the rules must not tombstone the site's items: %+v", a)
		}
		openTopic(t, ts, "letmoe", author, "b1", "after the typo")
		processBatch(t)
		got := kinds(notifsOfSite(t, "letmoe", follower))
		if len(got) != 2 || got[1] != model.NotificationKindFolloweeThreadCreated {
			t.Fatalf("with rules that do not parse, kind 9 carries topics: %v", got)
		}
	})
}

func TestProjectionEdges(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
	th := openTopic(t, ts, "letmoe", 20, "b1", "edges")
	opening := openingPost(t, th.ID)
	project(t)

	if err := testDB.Exec(`UPDATE community_thread SET title = E'line1\nline2\tend' WHERE id = ?`, th.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.Title != "line1 line2 end" {
		t.Fatalf("a title is one line, as the site API requires: %+v", a)
	}

	if err := testDB.Exec(`UPDATE community_activity SET revision = revision + 1000000000000 WHERE key = ?`, ownActivityKey(opening.ID)).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`UPDATE community_post SET content_raw = 'after a clock step' WHERE id = ?`, opening.ID).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.Excerpt != "after a clock step" {
		t.Fatalf("a stored revision ahead of the clock does not make a real change stale: %+v", a)
	}

	if err := testDB.Exec(`DELETE FROM community_activity_site WHERE site = 'letmoe'`).Error; err != nil {
		t.Fatal(err)
	}
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil || a.RemovedAt == nil {
		t.Fatalf("deleting a site's row takes its items out, like switching it off: %+v", a)
	}
}

func TestAClaimSkipsARowAWriterHolds(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	enableSite(t, "letmoe", "https://www.letmoe.com/community/{thread_id}", letmoeRules)
	th := openTopic(t, ts, "letmoe", 20, "b1", "held")
	opening := openingPost(t, th.ID)

	writer := testDB.Begin()
	defer writer.Rollback()
	if err := writer.Exec(`SELECT 1 FROM community_activity_projection WHERE post_id = ? FOR UPDATE`, opening.ID).Error; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := NewActivityService(testDB).ProjectBatch(ctx); err != nil {
		t.Fatalf("a claim must not wait on a row a writer holds: %v", err)
	}
	if a := ownActivity(t, "letmoe", opening.ID); a != nil {
		t.Fatalf("the held row is left for the next batch: %+v", a)
	}
	writer.Rollback()
	project(t)
	if a := ownActivity(t, "letmoe", opening.ID); a == nil {
		t.Fatal("the next batch takes it")
	}
}
