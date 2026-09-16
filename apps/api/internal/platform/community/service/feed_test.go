package service

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func backdate(t *testing.T, table string, id int64, col string, at time.Time) {
	t.Helper()
	if err := testDB.Exec("UPDATE "+table+" SET "+col+" = ? WHERE id = ?", at, id).Error; err != nil {
		t.Fatalf("backdate %s.%s: %v", table, col, err)
	}
}

func listThreadIDs(t *testing.T, ts *ThreadService, q repository.ThreadListQuery) []int64 {
	t.Helper()
	q.Site, q.Limit = "letmoe", 50
	rows, err := ts.List(q)
	if err != nil {
		t.Fatalf("list threads (%s): %v", q.Sort, err)
	}
	ids := make([]int64, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	return ids
}

func TestThreadList_SortsDisagree(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})

	first := openTopic(t, ts, "letmoe", 100, "b1", "a")
	second := openTopic(t, ts, "letmoe", 100, "b2", "b")
	third := openTopic(t, ts, "letmoe", 100, "b3", "c")

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	for range 2 {
		if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: first.ID, AuthorID: 200, BodyRaw: "r"}); err != nil {
			t.Fatalf("reply: %v", err)
		}
	}

	activity := listThreadIDs(t, ts, repository.ThreadListQuery{Kind: model.ThreadKindTopic, Sort: repository.ThreadSortActivity})
	if activity[0] != first.ID {
		t.Fatalf("activity sort must float the bumped thread: got %v", activity)
	}
	created := listThreadIDs(t, ts, repository.ThreadListQuery{Kind: model.ThreadKindTopic, Sort: repository.ThreadSortCreated})
	if created[0] != third.ID || created[2] != first.ID {
		t.Fatalf("created sort must be newest-thread-first: got %v", created)
	}
	posts := listThreadIDs(t, ts, repository.ThreadListQuery{Kind: model.ThreadKindTopic, Sort: repository.ThreadSortPosts})
	if posts[0] != first.ID {
		t.Fatalf("posts sort must lead with the most-replied thread: got %v", posts)
	}
	if second.ID == 0 {
		t.Fatal("unused")
	}
}

func TestThreadList_SortKeysetCoversEveryRow(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})

	opened := make(map[int64]bool, 5)
	for i := range 5 {
		th := openTopic(t, ts, "letmoe", 100, string(rune('a'+i)), "x")
		opened[th.ID] = true
	}

	for _, sort := range []repository.ThreadSort{repository.ThreadSortCreated, repository.ThreadSortPosts} {
		seen := map[int64]bool{}
		cursor := repository.ThreadCursor{}
		for {
			page, err := ts.List(repository.ThreadListQuery{
				Site: "letmoe", Kind: model.ThreadKindTopic, Sort: sort, Cursor: cursor, Limit: 2,
			})
			if err != nil {
				t.Fatalf("%s page: %v", sort, err)
			}
			if len(page) == 0 {
				break
			}
			for _, th := range page {
				if seen[th.ID] {
					t.Fatalf("%s keyset returned %d twice", sort, th.ID)
				}
				seen[th.ID] = true
			}
			last := page[len(page)-1]
			cursor = repository.ThreadCursor{
				Sort: sort, Created: last.CreatedAt, PostsCount: last.PostsCount, ID: last.ID,
			}
			if len(page) < 2 {
				break
			}
		}
		if len(seen) != len(opened) {
			t.Fatalf("%s keyset covered %d of %d threads", sort, len(seen), len(opened))
		}
	}
}

func TestThreadList_HasPostsDropsTheViewMintedThreads(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})

	for _, anchor := range []string{"g1", "g2"} {
		if _, err := ts.GetOrCreateCommentsThread(context.Background(), CommentsThreadParams{
			Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: anchor, ActorID: 1,
		}); err != nil {
			t.Fatalf("resolve %s: %v", anchor, err)
		}
	}
	talked, err := ts.GetOrCreateCommentsThread(context.Background(), CommentsThreadParams{
		Site: "letmoe", AnchorKind: model.AnchorKindSiteGame, AnchorID: "g3", ActorID: 1,
	})
	if err != nil {
		t.Fatalf("resolve g3: %v", err)
	}
	seedTrust(t, 300, model.TrustLevelBasic, 0)
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: talked.ID, AuthorID: 300, BodyRaw: "hi"}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	all := listThreadIDs(t, ts, repository.ThreadListQuery{Kind: model.ThreadKindComments})
	if len(all) != 3 {
		t.Fatalf("the unfiltered list must stay as it was: got %d", len(all))
	}
	nonEmpty := listThreadIDs(t, ts, repository.ThreadListQuery{Kind: model.ThreadKindComments, HasPosts: true})
	if len(nonEmpty) != 1 || nonEmpty[0] != talked.ID {
		t.Fatalf("has_posts must keep only the thread holding a post: got %v", nonEmpty)
	}
}

func TestSiteFeed_OrdersByCreatedTimeNotID(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	seedTrust(t, 400, model.TrustLevelBasic, 0)
	recent, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 400, BodyRaw: "recent"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	imported, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 400, BodyRaw: "imported"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	// The kungal import shape: the highest id carries the oldest timestamp.
	backdate(t, "community_post", imported.ID, "created_at", time.Now().Add(-72*time.Hour))

	rows, err := ps.SiteFeed(repository.PostFeedQuery{Site: "letmoe", Kind: -1, AnchorKind: -1, Limit: 50})
	if err != nil {
		t.Fatalf("site feed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("feed should hold the opening post and both replies: got %d", len(rows))
	}
	if rows[0].ID != recent.ID {
		t.Fatalf("feed must lead with the newest post by time, got post %d", rows[0].ID)
	}
	if rows[len(rows)-1].ID != imported.ID {
		t.Fatalf("the backdated post must sort last, got post %d", rows[len(rows)-1].ID)
	}
}

func TestSiteFeed_RepliesOnlyAndThreadContext(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	th := openTopic(t, ts, "letmoe", 100, "b1", "opening")

	seedTrust(t, 500, model.TrustLevelBasic, 0)
	if _, err := ps.Reply(context.Background(), ReplyParams{ThreadID: th.ID, AuthorID: 500, BodyRaw: "r"}); err != nil {
		t.Fatalf("reply: %v", err)
	}

	rows, err := ps.SiteFeed(repository.PostFeedQuery{Site: "letmoe", Kind: -1, AnchorKind: -1, RepliesOnly: true, Limit: 50})
	if err != nil {
		t.Fatalf("site feed: %v", err)
	}
	if len(rows) != 1 || rows[0].PostNumber != 2 {
		t.Fatalf("replies_only must drop the opening post: got %v", rows)
	}
	if rows[0].ThreadTitle == nil || rows[0].ThreadAnchorID != model.BoardAnchorID(testBoard(t, "letmoe", "b1")) {
		t.Fatalf("feed rows must carry thread context: %+v", rows[0])
	}

	hidden, err := ps.SiteFeed(repository.PostFeedQuery{
		Site: "letmoe", Kind: model.ThreadKindFeedback, AnchorKind: -1, Limit: 50,
	})
	if err != nil {
		t.Fatalf("kind filter: %v", err)
	}
	if len(hidden) != 0 {
		t.Fatalf("a kind filter with no threads must return nothing: got %d", len(hidden))
	}
}

func TestSiteFeed_TenancyMirrorsTheIDGuard(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})

	openTopic(t, ts, "letmoe", 100, "b1", "mine")
	openTopic(t, ts, "kungal", 101, "b1", "theirs")
	openFeedback(t, ts, "kungal", 102, model.AnchorKindCatalogWork, "w42", "shared")

	ps := NewPostService(testDB, NoopSink{})
	rows, err := ps.SiteFeed(repository.PostFeedQuery{Site: "letmoe", Kind: -1, AnchorKind: -1, Limit: 50})
	if err != nil {
		t.Fatalf("site feed: %v", err)
	}
	bodies := map[string]bool{}
	for _, r := range rows {
		bodies[r.ContentRaw] = true
	}
	if !bodies["mine"] {
		t.Fatal("the caller's own post must be in its feed")
	}
	if bodies["theirs"] {
		t.Fatal("another site's site-local post leaked into the feed")
	}
	if !bodies["shared"] {
		t.Fatal("a catalog-anchored post is one network-wide conversation and must be visible")
	}
}
