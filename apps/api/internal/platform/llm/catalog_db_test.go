package llm_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/llm"
)

type catalogUpstream struct {
	mu     sync.Mutex
	models string
	srv    *httptest.Server
}

func newCatalogUpstream(t *testing.T) *catalogUpstream {
	c := &catalogUpstream{}
	c.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models"):
			c.mu.Lock()
			defer c.mu.Unlock()
			_, _ = w.Write([]byte(c.models))
		case strings.HasSuffix(r.URL.Path, "/images/generations"):
			_, _ = w.Write([]byte(`{"data":[{"url":"https://img.example/a.jpeg"},{"url":"https://img.example/b.jpeg"}],"usage":{"cost_in_usd_ticks":400000000}}`))
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"pong\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(c.srv.Close)
	return c
}

func (c *catalogUpstream) list(models string) {
	c.mu.Lock()
	c.models = models
	c.mu.Unlock()
}

func TestCatalogSyncGuessesKindsTracksDelistingAndAdopts(t *testing.T) {
	db := gatewayDB(t)
	ctx := context.Background()
	kek := testKEK(t)
	gw := llm.New(db, kek)
	a := llm.NewAdmin(db, kek, gw)
	up := newCatalogUpstream(t)
	name, base, key := "relay", up.srv.URL+"/v1", "sk-relay"
	p, err := a.CreateProvider(ctx, llm.ProviderInput{Name: &name, BaseURL: &base, APIKey: &key})
	if err != nil {
		t.Fatal(err)
	}

	up.list(`{"data":[
		{"id":"grok-4.7","owned_by":"xai"},
		{"id":"grok-imagine-image","owned_by":"xai"},
		{"id":"grok-imagine-video","owned_by":"xai"},
		{"id":"qwen3-embedding-0.6b"},
		{"id":"vision-pro","architecture":{"input_modalities":["text","image"],"output_modalities":["text"]},"context_length":131072}
	]}`)
	res, err := a.SyncProviderModels(ctx, p.ID)
	if err != nil || res.Listed != 5 || res.Added != 5 {
		t.Fatalf("first sync: %+v err=%v", res, err)
	}
	entries, _ := a.ProviderModels(ctx, p.ID)
	kinds := map[string]llm.ProviderModelView{}
	for _, e := range entries {
		kinds[e.UpstreamID] = e
	}
	for id, want := range map[string]string{"grok-4.7": "chat", "grok-imagine-image": "image", "grok-imagine-video": "video", "qwen3-embedding-0.6b": "embedding", "vision-pro": "chat"} {
		if kinds[id].Kind != want {
			t.Errorf("%s kind = %s (%s), want %s", id, kinds[id].Kind, kinds[id].KindSource, want)
		}
	}
	if v := kinds["vision-pro"]; v.KindSource != "metadata" || v.ContextTokens != 131072 || len(v.InputModalities) != 2 {
		t.Fatalf("metadata is kept: %+v", v)
	}
	if kinds["grok-imagine-video"].Supported || !kinds["grok-imagine-image"].Supported {
		t.Fatal("video is catalog-only; images can be routed")
	}

	if _, err := a.SetProviderModelKind(ctx, kinds["vision-pro"].ID, "search"); err != nil {
		t.Fatal(err)
	}
	adopted, err := a.AdoptProviderModel(ctx, kinds["grok-imagine-image"].ID, llm.AdoptInput{Price: &llm.Price{PerCallMicro: 20_000}})
	if err != nil || !adopted.ModelCreated || adopted.Model.Key != "grok-imagine-image" || adopted.Model.Kind != "image" || adopted.Route.Protocol != "images" {
		t.Fatalf("adopt: %+v err=%v", adopted, err)
	}
	chat, err := a.AdoptProviderModel(ctx, kinds["grok-4.7"].ID, llm.AdoptInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.AdoptProviderModel(ctx, kinds["qwen3-embedding-0.6b"].ID, llm.AdoptInput{ModelKey: &chat.Model.Key}); err == nil {
		t.Fatal("an embedding upstream cannot join a chat model")
	}

	probed, err := a.ProbeProviderModel(ctx, kinds["grok-4.7"].ID, "")
	if err != nil || probed.Probe == nil || !probed.Probe.OK || probed.KindSource != "probe" {
		t.Fatalf("probe: %+v err=%v", probed.Probe, err)
	}

	up.list(`{"data":[{"id":"grok-imagine-image"},{"id":"vision-pro"},{"id":"qwen3-embedding-0.6b"},{"id":"grok-imagine-video"}]}`)
	res, err = a.SyncProviderModels(ctx, p.ID)
	if err != nil || res.Delisted != 1 || len(res.Orphaned) != 1 || res.Orphaned[0] != chat.Route.ID {
		t.Fatalf("second sync: %+v err=%v", res, err)
	}
	entries, _ = a.ProviderModels(ctx, p.ID)
	for _, e := range entries {
		switch e.UpstreamID {
		case "grok-4.7":
			if e.Listed || e.DelistedAt == nil || len(e.Routes) != 1 {
				t.Fatalf("a delisted entry keeps its route link: %+v", e)
			}
		case "vision-pro":
			if e.Kind != "search" || e.KindSource != "admin" {
				t.Fatalf("an admin's kind survives a sync: %+v", e)
			}
		}
	}
	var events []llm.EventView
	events, _ = a.Events(ctx, 50)
	found := false
	for _, ev := range events {
		found = found || ev.Kind == "upstream_delisted"
	}
	if !found {
		t.Fatal("a delisted upstream behind an active route raises an event")
	}

	if err := gw.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	body := map[string]json.RawMessage{"prompt": json.RawMessage(`"cat"`), "n": json.RawMessage(`2`)}
	if _, err := gw.GenerateImages(ctx, llm.Request{Model: "grok-imagine-image", Body: body, Site: "test"}); err != nil {
		t.Fatal(err)
	}
	var cost int64
	db.Raw("SELECT cost_nano FROM llm_attempt WHERE model_key = 'grok-imagine-image' ORDER BY id DESC LIMIT 1").Scan(&cost)
	if cost != 40_000_000 {
		t.Fatalf("the upstream's reported cost (4e8 ticks = 0.04 USD) wins over the per-picture price: cost_nano=%d", cost)
	}
}

func TestCatalogSyncReportsAProviderWithoutAModelList(t *testing.T) {
	db := gatewayDB(t)
	ctx := context.Background()
	kek := testKEK(t)
	a := llm.NewAdmin(db, kek, llm.New(db, kek))
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	name, base, key := "nolist", srv.URL, "k"
	p, _ := a.CreateProvider(ctx, llm.ProviderInput{Name: &name, BaseURL: &base, APIKey: &key})
	if _, err := a.SyncProviderModels(ctx, p.ID); err != llm.ErrCatalogUnsupported {
		t.Fatalf("err = %v", err)
	}
}
