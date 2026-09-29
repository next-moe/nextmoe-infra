package hihyou

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"api/internal/platform/news/model"
)

type fakeBilibili struct {
	mu        sync.Mutex
	index     []IndexEntry
	articles  map[int64]*Article
	limited   map[int64]int // -509 answers left before the article is served
	requested []int64
	warms     int
}

func (f *fakeBilibili) serve(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/":
			f.warms++
			http.SetCookie(w, &http.Cookie{Name: "buvid3", Value: "test"})
		case indexPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0,
				"data": map[string]any{"articles": f.index, "count": len(f.index)}})
		case viewPath:
			id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			f.requested = append(f.requested, id)
			if f.limited[id] > 0 {
				f.limited[id]--
				_ = json.NewEncoder(w).Encode(map[string]any{"code": codeRateLimited, "message": "请求过于频繁"})
				return
			}
			a, ok := f.articles[id]
			if !ok {
				t.Errorf("fetched cv%d, which the poll had no reason to read", id)
				_ = json.NewEncoder(w).Encode(map[string]any{"code": -404, "message": "not found"})
				return
			}
			_ = json.NewEncoder(w).Encode(a)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient()
	if err != nil {
		t.Fatal(err)
	}
	c.apiBase, c.homeURL = srv.URL, srv.URL+"/"
	return c
}

func (f *fakeBilibili) takeRequested() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.requested
	f.requested = nil
	return out
}

func newPollWriter(t *testing.T) *writer {
	t.Helper()
	db, _ := openTestDB(t)
	w := &writer{db: db, opts: Opts{Apply: true}, uploaded: map[string]string{}, failed: map[string]bool{}}
	if err := w.seedSource(context.Background()); err != nil {
		t.Fatal(err)
	}
	return w
}

const day = 24 * time.Hour

var testPollOpts = PollOpts{Lookback: 28 * day, Recheck: 7 * day, Passes: 1}

func TestPollImportsRecentWeekliesAndReleasesThem(t *testing.T) {
	w := newPollWriter(t)
	ctx := context.Background()
	published := time.Unix(1786257779, 0)
	now := published.Add(3 * day)

	// A stored issue past the recheck window is not read again, even though it
	// is still inside the lookback.
	if err := w.db.Create(&model.NewsItem{SourceKey: model.SourceKeyHihyou, Lane: model.LaneNews,
		ExternalID: ExternalID(1990, 1), Title: "t", Preview: "p", SourceURL: SourceURL(1990),
		PublishedAt: published.Add(-10 * day), Status: model.StatusPublished}).Error; err != nil {
		t.Fatal(err)
	}
	broken := article("【Gal周报201期】x", text(17, false, "新作资讯"),
		text(17, true, "《只有一条》"), text(17, false, body))
	broken.Data.ID = 2011
	broken.Data.PublishTime = published.Add(day).Unix()
	fake := &fakeBilibili{
		index: []IndexEntry{
			{ID: 2011, Title: "【Gal周报201期】x", PublishTime: broken.Data.PublishTime},
			{ID: 2012, Title: "聊聊这两年的galgame", PublishTime: published.Unix()},
			{ID: 2010, Title: "【Gal周报200期】x", PublishTime: published.Unix()},
			{ID: 1990, Title: "【Gal周报199期】x", PublishTime: published.Add(-10 * day).Unix()},
			{ID: 1500, Title: "【Gal周报150期】x", PublishTime: published.Add(-60 * day).Unix()},
		},
		articles: map[int64]*Article{2010: healthy(2010), 2011: broken},
	}
	client := fake.serve(t)

	sum, err := w.poll(ctx, client, testPollOpts, now)
	if err == nil || !strings.Contains(err.Error(), "issue 201 quarantined") {
		t.Fatalf("err = %v, want the quarantined issue reported", err)
	}
	if sum["created"] != 3 || sum["released"] != 3 {
		t.Fatalf("created=%v released=%v, want 3/3", sum["created"], sum["released"])
	}
	if got := fake.takeRequested(); !slices.Equal(got, []int64{2010, 2011}) {
		t.Fatalf("fetched %v, want the two missing weeklies inside the lookback, oldest first", got)
	}

	var rows []model.NewsItem
	if err := w.db.Where("source_key = ? AND external_id LIKE 'cv2010#%'", model.SourceKeyHihyou).
		Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("stored %d rows for cv2010, want 3", len(rows))
	}
	for _, r := range rows {
		if r.Status != model.StatusPublished {
			t.Errorf("%s: status %d", r.ExternalID, r.Status)
		}
	}
	var decisions []model.NewsModerationDecision
	if err := w.db.Find(&decisions).Error; err != nil {
		t.Fatal(err)
	}
	for _, d := range decisions {
		if d.ActorUID != model.SystemActorUID || d.Reason != releaseReason {
			t.Errorf("decision = %+v", d)
		}
	}

	// The quarantine wrote nothing, so the next run looks for the issue again,
	// ahead of the stored one it re-reads.
	fixed := healthy(2011)
	fixed.Data.Title = "【Gal周报201期】x"
	fake.articles[2011] = fixed
	sum, err = w.poll(ctx, client, testPollOpts, now)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if sum["created"] != 3 || sum["unchanged"] != 3 || sum["released"] != 3 {
		t.Fatalf("second poll: created=%v unchanged=%v released=%v, want 3/3/3",
			sum["created"], sum["unchanged"], sum["released"])
	}
	if got := fake.takeRequested(); !slices.Equal(got, []int64{2011, 2010}) {
		t.Fatalf("second poll fetched %v, want the missing issue first", got)
	}
}

func TestPollRetriesARateLimitedArticleAfterACooldown(t *testing.T) {
	w := newPollWriter(t)
	ctx := context.Background()
	published := time.Unix(1786257779, 0)
	now := published.Add(day)
	fake := &fakeBilibili{
		index:    []IndexEntry{{ID: 2020, Title: "【Gal周报200期】x", PublishTime: published.Unix()}},
		articles: map[int64]*Article{2020: healthy(2020)},
		limited:  map[int64]int{2020: 1},
	}
	client := fake.serve(t)

	opts := testPollOpts
	opts.Passes = 2
	sum, err := w.poll(ctx, client, opts, now)
	if err != nil {
		t.Fatalf("poll: %v", err)
	}
	if sum["created"] != 3 || sum["rate_limited"] != 1 {
		t.Fatalf("created=%v rate_limited=%v, want 3/1", sum["created"], sum["rate_limited"])
	}
	if fake.warms != 2 {
		t.Errorf("warm-ups = %d, want a fresh cookie before the second pass", fake.warms)
	}

	// Out of passes, the article is reported rather than silently skipped.
	fake.limited[2020] = 1
	_, err = w.poll(ctx, client, testPollOpts, now)
	if err == nil || !strings.Contains(err.Error(), "cv2020") {
		t.Fatalf("err = %v, want the rate-limited article named", err)
	}
}
