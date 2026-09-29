package ymgalnews

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"api/internal/platform/news/model"
	"api/internal/testsupport/dbtest"
	"api/pkg/config"
)

func fakeYmgal(t *testing.T, title *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case tokenPath:
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "t", "expires_in": 3600})
		case pathNews:
			var data []Topic
			if r.URL.Query().Get("page") == "1" {
				data = []Topic{topic("700", title.Load().(string), "intro")}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "code": 0, "data": data})
		case pathColumn:
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "code": 0, "data": []Topic{}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRunPublishesWhatItIngests(t *testing.T) {
	newTestWriter(t, true)
	dsn, _ := dbtest.DSN()
	var title atomic.Value
	title.Store("original")
	srv := fakeYmgal(t, &title)

	cfg := &config.Config{}
	cfg.Ymgal = config.YmgalConfig{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}
	opts := Opts{Lanes: []string{LaneNews, LaneColumn}, Pages: 1, Apply: true, NoImages: true, DSN: dsn}
	ctx := context.Background()

	sum, err := Run(ctx, cfg, opts)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if sum["created"] != 1 || sum["released"] != 1 {
		t.Fatalf("created=%v released=%v, want 1/1", sum["created"], sum["released"])
	}
	if got := row(t, "700").Status; got != model.StatusPublished {
		t.Fatalf("status = %d, want published", got)
	}

	title.Store("edited")
	if _, err := Run(ctx, cfg, opts); err != nil {
		t.Fatalf("second run: %v", err)
	}
	r := row(t, "700")
	if r.Status != model.StatusPublished || r.Title != "edited" {
		t.Fatalf("after edit: status=%d title=%q, want published/edited", r.Status, r.Title)
	}

	var decisions []model.NewsModerationDecision
	if err := testDB.Where("item_id = ?", r.ID).Order("id").Find(&decisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(decisions) != 2 {
		t.Fatalf("decision rows = %d, want one for the release and one for the edit", len(decisions))
	}
	for _, d := range decisions {
		if d.ActorUID != model.SystemActorUID || d.FromStatus != model.StatusPending ||
			d.ToStatus != model.StatusPublished || d.Reason != releaseReason {
			t.Errorf("decision = %+v", d)
		}
	}
}

func TestDryRunReleasesNothing(t *testing.T) {
	newTestWriter(t, false)
	if err := testDB.Create(&model.NewsItem{SourceKey: model.SourceKeyYmgal, Lane: LaneNews,
		ExternalID: "701", Title: "t", Preview: "p", SourceURL: "https://www.ymgal.games/co/article/701",
		Status: model.StatusPending}).Error; err != nil {
		t.Fatal(err)
	}
	dsn, _ := dbtest.DSN()
	var title atomic.Value
	title.Store("t")
	srv := fakeYmgal(t, &title)

	cfg := &config.Config{}
	cfg.Ymgal = config.YmgalConfig{BaseURL: srv.URL, ClientID: "id", ClientSecret: "secret"}
	if _, err := Run(context.Background(), cfg, Opts{Pages: 1, NoImages: true, DSN: dsn}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if got := row(t, "701").Status; got != model.StatusPending {
		t.Errorf("a dry run published a row: status = %d", got)
	}
}
