package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"api/internal/platform/community/model"
	"api/internal/platform/community/repository"
)

func openTitled(t *testing.T, ts *ThreadService, site string, author int64, anchor, title, body string) *model.CommunityThread {
	t.Helper()
	seedTrust(t, author, model.TrustLevelBasic, 0)
	th, _, err := ts.OpenTopic(context.Background(), OpenThreadParams{
		Site: site, AuthorID: author, AnchorKind: model.AnchorKindBoard, AnchorID: anchor,
		Title: title, BodyRaw: body,
	})
	if err != nil {
		t.Fatalf("open topic %q: %v", title, err)
	}
	return th
}

func searchBodies(t *testing.T, ss *SearchService, site, q string) []string {
	t.Helper()
	rows, err := ss.Posts(repository.SearchQuery{Site: site, Q: q, Kind: -1, Limit: 50})
	if err != nil {
		t.Fatalf("search %q: %v", q, err)
	}
	out := make([]string, len(rows))
	for i := range rows {
		out[i] = rows[i].ContentRaw
	}
	return out
}

func TestSearchPosts_ReadsTheSourceNotTheCookedHTML(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ss := NewSearchService(testDB)
	openTitled(t, ts, "letmoe", 100, "b1", "t", "看看这个[链接](https://example.com)")

	if got := searchBodies(t, ss, "letmoe", "链接"); len(got) != 1 {
		t.Fatalf("a CJK two-character query must match the source: %v", got)
	}
	if got := searchBodies(t, ss, "letmoe", "nofollow"); len(got) != 0 {
		t.Fatalf("searching must not see the sanitizer's own markup: %v", got)
	}
}

func TestSearchPosts_WildcardsAreLiteral(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ss := NewSearchService(testDB)
	openTitled(t, ts, "letmoe", 100, "b1", "t", "折扣 100% 了")
	openTitled(t, ts, "letmoe", 101, "b2", "t", "100abc")

	got := searchBodies(t, ss, "letmoe", "100%")
	if len(got) != 1 || !strings.Contains(got[0], "折扣") {
		t.Fatalf("%% in a query is a character the user typed, not a wildcard: %v", got)
	}
	if got := searchBodies(t, ss, "letmoe", "10_abc"); len(got) != 0 {
		t.Fatalf("_ must not match any single character: %v", got)
	}
}

func TestSearchPosts_SkipsWhatTheReaderCannotSee(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ps := NewPostService(testDB, NoopSink{})
	ss := NewSearchService(testDB)
	th := openTitled(t, ts, "letmoe", 100, "b1", "t", "visible needle")

	seedTrust(t, 200, model.TrustLevelBasic, 0)
	hidden, err := ps.Reply(letmoeCtx(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "hidden needle"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	tombstoned, err := ps.Reply(letmoeCtx(), ReplyParams{ThreadID: th.ID, AuthorID: 200, BodyRaw: "deleted needle"})
	if err != nil {
		t.Fatalf("reply: %v", err)
	}
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusHidden, hidden.ID).Error; err != nil {
		t.Fatalf("hide: %v", err)
	}
	if err := testDB.Exec("UPDATE community_post SET status = ? WHERE id = ?", model.PostStatusDeleted, tombstoned.ID).Error; err != nil {
		t.Fatalf("tombstone: %v", err)
	}

	got := searchBodies(t, ss, "letmoe", "needle")
	if len(got) != 1 || got[0] != "visible needle" {
		t.Fatalf("only visible posts may surface in search: %v", got)
	}

	if err := testDB.Exec("UPDATE community_thread SET status = ? WHERE id = ?", model.ThreadStatusDeleted, th.ID).Error; err != nil {
		t.Fatalf("delete thread: %v", err)
	}
	if got := searchBodies(t, ss, "letmoe", "needle"); len(got) != 0 {
		t.Fatalf("a deleted thread takes its posts out of search: %v", got)
	}
}

func TestSearchPosts_TenancyMirrorsTheIDGuard(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ss := NewSearchService(testDB)

	openTitled(t, ts, "letmoe", 100, "b1", "t", "needle mine")
	openTitled(t, ts, "kungal", 101, "b1", "t", "needle theirs")
	if _, _, err := ts.OpenFeedback(context.Background(), OpenThreadParams{
		Site: "kungal", AuthorID: 102, AnchorKind: model.AnchorKindCatalogWork, AnchorID: "w42",
		Title: "t", BodyRaw: "needle shared",
	}); err != nil {
		t.Fatalf("open feedback: %v", err)
	}

	got := searchBodies(t, ss, "letmoe", "needle")
	found := map[string]bool{}
	for _, b := range got {
		found[b] = true
	}
	if !found["needle mine"] || !found["needle shared"] || found["needle theirs"] {
		t.Fatalf("search scope must match the id-addressed guard: %v", got)
	}
}

func TestSearch_QueryBounds(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ss := NewSearchService(testDB)
	openTitled(t, ts, "letmoe", 100, "b1", "t", "ab needle")

	for _, q := range []string{"", " ", "a", strings.Repeat("字", 101)} {
		if _, err := ss.Posts(repository.SearchQuery{Site: "letmoe", Q: q, Kind: -1}); !errors.Is(err, ErrInvalidSearchQuery) {
			t.Fatalf("query %q should be refused, got %v", q, err)
		}
		if _, err := ss.Threads(repository.SearchQuery{Site: "letmoe", Q: q, Kind: -1}); !errors.Is(err, ErrInvalidSearchQuery) {
			t.Fatalf("thread query %q should be refused, got %v", q, err)
		}
	}
	if got := searchBodies(t, ss, "letmoe", "  ab  "); len(got) != 1 {
		t.Fatalf("a padded query is trimmed before it is matched: %v", got)
	}
}

func TestSearchThreads_TitlesAndKeyset(t *testing.T) {
	cleanTables(t)
	ts := NewThreadService(testDB, NoopSink{})
	ss := NewSearchService(testDB)

	openTitled(t, ts, "letmoe", 100, "b1", "夏日回忆 攻略", "x")
	openTitled(t, ts, "letmoe", 100, "b2", "夏日回忆 感想", "x")
	openTitled(t, ts, "letmoe", 100, "b3", "别的话题", "x")
	gone := openTitled(t, ts, "letmoe", 100, "b4", "夏日回忆 删除", "x")
	if err := testDB.Exec("UPDATE community_thread SET status = ? WHERE id = ?", model.ThreadStatusDeleted, gone.ID).Error; err != nil {
		t.Fatalf("delete thread: %v", err)
	}

	hits, err := ss.Threads(repository.SearchQuery{Site: "letmoe", Q: "夏日", Kind: -1, Limit: 50})
	if err != nil {
		t.Fatalf("search threads: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected the two live 夏日回忆 threads, got %d", len(hits))
	}

	seen := map[int64]bool{}
	cursor := repository.TimeCursor{}
	for {
		page, err := ss.Threads(repository.SearchQuery{Site: "letmoe", Q: "夏日", Kind: -1, Cursor: cursor, Limit: 1})
		if err != nil {
			t.Fatalf("page: %v", err)
		}
		if len(page) == 0 {
			break
		}
		for _, th := range page {
			if seen[th.ID] {
				t.Fatalf("search keyset returned %d twice", th.ID)
			}
			seen[th.ID] = true
		}
		last := page[len(page)-1]
		cursor = repository.TimeCursor{CreatedAt: last.CreatedAt, ID: last.ID}
	}
	if len(seen) != 2 {
		t.Fatalf("search keyset covered %d of 2 threads", len(seen))
	}
}
