package hihyou

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	requested []int64
}

func (f *fakeBilibili) serve(t *testing.T) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch r.URL.Path {
		case "/":
			http.SetCookie(w, &http.Cookie{Name: "buvid3", Value: "test"})
		case indexPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0,
				"data": map[string]any{"articles": f.index, "count": len(f.index)}})
		case viewPath:
			id, _ := strconv.ParseInt(r.URL.Query().Get("id"), 10, 64)
			f.requested = append(f.requested, id)
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

func TestPollImportsRecentWeekliesAndReleasesThem(t *testing.T) {
	db, _ := openTestDB(t)
	ctx := context.Background()
	published := time.Unix(1786257779, 0)
	now := published.Add(3 * 24 * time.Hour)

	broken := article("【Gal周报201期】x", text(17, false, "新作资讯"),
		text(17, true, "《只有一条》"), text(17, false, body))
	broken.Data.ID = 2011
	broken.Data.PublishTime = published.Add(24 * time.Hour).Unix()
	fake := &fakeBilibili{
		index: []IndexEntry{
			{ID: 2011, Title: "【Gal周报201期】x", PublishTime: broken.Data.PublishTime},
			{ID: 2012, Title: "聊聊这两年的galgame", PublishTime: published.Unix()},
			{ID: 2010, Title: "【Gal周报200期】x", PublishTime: published.Unix()},
			{ID: 1500, Title: "【Gal周报150期】x", PublishTime: published.Add(-60 * 24 * time.Hour).Unix()},
		},
		articles: map[int64]*Article{2010: healthy(2010), 2011: broken},
	}
	client := fake.serve(t)
	w := &writer{db: db, opts: Opts{Apply: true}, uploaded: map[string]string{}, failed: map[string]bool{}}
	if err := w.seedSource(ctx); err != nil {
		t.Fatal(err)
	}
	opts := PollOpts{Lookback: 28 * 24 * time.Hour}

	sum, err := w.poll(ctx, client, opts, now)
	if err == nil || !strings.Contains(err.Error(), "issue 201 quarantined") {
		t.Fatalf("err = %v, want the quarantined issue reported", err)
	}
	if sum["created"] != 3 || sum["released"] != 3 {
		t.Fatalf("created=%v released=%v, want 3/3", sum["created"], sum["released"])
	}
	if got := fake.requested; len(got) != 2 || got[0] != 2010 || got[1] != 2011 {
		t.Fatalf("fetched %v, want the two weeklies inside the lookback, oldest first", got)
	}

	var rows []model.NewsItem
	if err := db.Where("source_key = ?", model.SourceKeyHihyou).Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Status != model.StatusPublished || !strings.HasPrefix(r.ExternalID, "cv2010#") {
			t.Errorf("%s: status %d", r.ExternalID, r.Status)
		}
	}
	var decisions []model.NewsModerationDecision
	if err := db.Find(&decisions).Error; err != nil {
		t.Fatal(err)
	}
	for _, d := range decisions {
		if d.ActorUID != model.SystemActorUID || d.Reason != releaseReason {
			t.Errorf("decision = %+v", d)
		}
	}

	// The quarantine wrote nothing, so the next run reads the issue again: once
	// upstream (or the gate) is fixed it arrives without anyone re-running it.
	fixed := healthy(2011)
	fixed.Data.Title = "【Gal周报201期】x"
	fake.articles[2011] = fixed
	fake.requested = nil
	sum, err = w.poll(ctx, client, opts, now)
	if err != nil {
		t.Fatalf("second poll: %v", err)
	}
	if sum["created"] != 3 || sum["unchanged"] != 3 || sum["released"] != 3 {
		t.Fatalf("second poll: created=%v unchanged=%v released=%v, want 3/3/3",
			sum["created"], sum["unchanged"], sum["released"])
	}
}
