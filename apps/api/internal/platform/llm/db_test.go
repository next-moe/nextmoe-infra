package llm_test

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aimigrate "api/internal/platform/ai/migrate"
	"api/internal/platform/llm"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func gatewayDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
		return nil
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := aimigrate.Run(db); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"llm_attempt", "llm_event", "llm_scene", "llm_price_revision", "llm_route", "llm_model", "llm_provider", "llm_access_key"} {
		if err := db.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func testKEK(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	kek, err := llm.ParseKEK(base64.StdEncoding.EncodeToString(b))
	if err != nil {
		t.Fatal(err)
	}
	return kek
}

type fakeUpstream struct {
	srv       *httptest.Server
	auth      []string
	models    []string
	failChat  bool
	chatCalls int
}

func newFakeUpstream(t *testing.T) *fakeUpstream {
	f := &fakeUpstream{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = decodeJSON(r, &body)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.models = append(f.models, body.Model)
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			f.chatCalls++
			if f.failChat {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"error":"overloaded"}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1000,\"completion_tokens\":10}}\n\ndata: [DONE]\n\n"))
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			_, _ = w.Write([]byte(`{"model":"` + body.Model + `","data":[{"index":0,"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":3,"total_tokens":3}}`))
		case strings.HasSuffix(r.URL.Path, "/moderations"):
			_, _ = w.Write([]byte(`{"model":"` + body.Model + `","results":[{"flagged":false,"categories":{},"category_scores":{}}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func TestSeededGatewayStreamsEmbedsModeratesAndRecords(t *testing.T) {
	db := gatewayDB(t)
	up := newFakeUpstream(t)
	t.Setenv("LLM_TEST_KEY", "sk-test-upstream")
	t.Setenv("LLM_TEST_OMNI", "sk-test-omni")
	kek := testKEK(t)
	ctx := context.Background()
	boot := llm.Bootstrap{UpstreamBaseURL: up.srv.URL, UpstreamKeyEnv: "LLM_TEST_KEY", OmniBaseURL: up.srv.URL, OmniKeyEnv: "LLM_TEST_OMNI"}
	for range 2 {
		if err := llm.SeedDefaults(ctx, db, kek, boot); err != nil {
			t.Fatal(err)
		}
	}
	var providers []llm.Provider
	db.Order("id").Find(&providers)
	if len(providers) != 2 {
		t.Fatalf("seed must run once and create two providers, got %d", len(providers))
	}
	for _, p := range providers {
		if p.KeyEnv != "" || len(p.KeyCipher) == 0 || strings.Contains(string(p.KeyCipher), "sk-test") {
			t.Fatalf("a seeded key must be sealed in the database, not referenced by env: %+v", p)
		}
	}
	if providers[1].BaseURL != up.srv.URL+"/v1" {
		t.Fatalf("omni base = %q", providers[1].BaseURL)
	}
	t.Setenv("LLM_TEST_KEY", "")

	g := llm.New(db, kek)
	if err := g.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := g.Stream(ctx, llm.Call{Scene: llm.SceneAssistant, Site: "letmoe", UserID: 7,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hello"}}}, func(llm.Event) {})
	if err != nil || res.Text != "hi" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if up.auth[0] != "Bearer sk-test-upstream" || up.models[0] != "@cf/deepseek-ai/deepseek-v4-flash" {
		t.Fatalf("upstream saw auth=%q model=%q", up.auth[0], up.models[0])
	}
	var a llm.Attempt
	if err := db.Where("call_id = ?", res.CallID).Take(&a).Error; err != nil {
		t.Fatal(err)
	}
	if !a.OK || a.Site != "letmoe" || a.UserID != 7 || a.InputTokens != 1000 || a.CostNano != (1000*440_000+10*1_320_000)/1000 {
		t.Fatalf("attempt = %+v", a)
	}

	emb, err := g.Embed(ctx, llm.Request{Model: "@cf/qwen/qwen3-embedding-0.6b", Body: map[string]json.RawMessage{"input": json.RawMessage(`["x"]`)}})
	if err != nil || !strings.Contains(string(emb.Body), `"embedding":[0.1,0.2]`) || emb.Usage.InputTokens != 3 {
		t.Fatalf("embed by legacy upstream alias: %+v err=%v", emb, err)
	}
	mod, err := g.Moderate(ctx, llm.Request{Scene: llm.SceneModerationOmni, Body: map[string]json.RawMessage{"input": json.RawMessage(`"x"`)}})
	if err != nil || !strings.Contains(string(mod.Body), `"flagged":false`) || up.auth[len(up.auth)-1] != "Bearer sk-test-omni" {
		t.Fatalf("moderate: %+v err=%v auth=%q", mod, err, up.auth[len(up.auth)-1])
	}
	if _, err := g.Embed(ctx, llm.Request{Model: "deepseek-v4-flash"}); llm.KindOf(err) != llm.ErrKindBadRequest {
		t.Fatalf("embedding through a chat model must be refused, err=%v", err)
	}
	if _, err := g.Embed(ctx, llm.Request{Model: "no-such-model"}); !errors.Is(err, llm.ErrUnknownModel) {
		t.Fatalf("unknown model err=%v", err)
	}
}

func TestAdminManagedRoutesFailOverAndAccessKeysScope(t *testing.T) {
	db := gatewayDB(t)
	kek := testKEK(t)
	ctx := context.Background()
	g := llm.New(db, kek)
	admin := llm.NewAdmin(db, kek, g)

	broken, healthy := newFakeUpstream(t), newFakeUpstream(t)
	broken.failChat = true
	str := func(s string) *string { return &s }
	i64 := func(n int64) *int64 { return &n }
	intp := func(n int) *int { return &n }

	pBroken, err := admin.CreateProvider(ctx, llm.ProviderInput{Name: str("broken"), BaseURL: str(broken.srv.URL), APIKey: str("sk-broken-0001")})
	if err != nil {
		t.Fatal(err)
	}
	if !pBroken.KeyConfigured || pBroken.KeyHint != "…0001" {
		t.Fatalf("provider view must hint, never reveal, the key: %+v", pBroken)
	}
	pHealthy, err := admin.CreateProvider(ctx, llm.ProviderInput{Name: str("healthy"), BaseURL: str(healthy.srv.URL), APIKey: str("sk-healthy-0002")})
	if err != nil {
		t.Fatal(err)
	}
	m, err := admin.CreateModel(ctx, llm.ModelInput{Key: str("chat-a"), Kind: str(llm.ModelChat)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.CreateRoute(ctx, llm.RouteInput{ModelID: i64(m.ID), ProviderID: i64(pBroken.ID), UpstreamID: str("up-a"), Priority: intp(10)}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.CreateRoute(ctx, llm.RouteInput{ModelID: i64(m.ID), ProviderID: i64(pHealthy.ID), UpstreamID: str("up-a"), Priority: intp(0),
		Price: &llm.Price{InputMicro: 1_000_000, OutputMicro: 2_000_000}}); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.PutScene(ctx, "test.chat", llm.SceneInput{ModelID: m.ID}); err != nil {
		t.Fatal(err)
	}
	emb, err := admin.CreateModel(ctx, llm.ModelInput{Key: str("emb-a"), Kind: str(llm.ModelEmbedding)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.PutScene(ctx, "test.mixed", llm.SceneInput{ModelID: m.ID, FallbackModelIDs: []int64{emb.ID}}); !errors.Is(err, llm.ErrInvalid) {
		t.Fatalf("a chat scene must refuse an embedding fallback, err=%v", err)
	}

	res, err := g.Stream(ctx, llm.Call{Scene: "test.chat", Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}, func(llm.Event) {})
	if err != nil || res.Text != "hi" || broken.chatCalls != 1 || healthy.chatCalls != 1 {
		t.Fatalf("the higher tier must fail over to the next one: res=%+v err=%v broken=%d healthy=%d", res, err, broken.chatCalls, healthy.chatCalls)
	}
	if healthy.auth[0] != "Bearer sk-healthy-0002" {
		t.Fatalf("auth = %q", healthy.auth[0])
	}

	routes, err := admin.Routes(ctx)
	if err != nil || len(routes) != 2 {
		t.Fatalf("routes = %+v err=%v", routes, err)
	}
	if routes[0].Stats24h.Errors != 1 || routes[1].Stats24h.Calls != 1 || routes[1].Price == nil {
		t.Fatalf("route stats/prices = %+v", routes)
	}
	if err := admin.DeleteProvider(ctx, pBroken.ID); !errors.Is(err, llm.ErrInUse) {
		t.Fatalf("a provider with routes must not be deletable, err=%v", err)
	}

	disabled := llm.RouteDisabled
	if _, err := admin.UpdateRoute(ctx, routes[0].ID, llm.RouteInput{Status: &disabled}); err != nil {
		t.Fatal(err)
	}
	probe, err := admin.Probe(ctx, routes[0].ID)
	if err != nil || probe.OK || probe.ErrorKind != string(llm.ErrKindUpstream) {
		t.Fatalf("probe of the broken route = %+v err=%v", probe, err)
	}
	var r llm.Route
	db.First(&r, routes[0].ID)
	if r.Status != llm.RouteDisabled {
		t.Fatalf("a probe must not change a route's status, got %s", r.Status)
	}

	key, secret, err := admin.CreateAccessKey(ctx, llm.AccessKeyInput{Name: str("catalog"), AllowedModels: &[]string{"test.chat"}})
	if err != nil || !strings.HasPrefix(secret, "nmk_") || !strings.HasPrefix(secret, key.Prefix) {
		t.Fatalf("access key = %+v secret=%q err=%v", key, secret, err)
	}
	var stored llm.AccessKey
	db.First(&stored, key.ID)
	if strings.Contains(stored.Hash, secret) || stored.Hash != llm.HashAccessKey(secret) {
		t.Fatal("only the hash of an access key may be stored")
	}
	k, ok := g.Authorize(secret)
	if !ok || !g.Allows(k, "test.chat") || g.Allows(k, "chat-a") || g.Allows(k, "emb-a") {
		t.Fatalf("a scene grant covers that scene only: ok=%v", ok)
	}
	byModel, modelSecret, err := admin.CreateAccessKey(ctx, llm.AccessKeyInput{Name: str("cron"), AllowedModels: &[]string{"chat-a"}})
	if err != nil {
		t.Fatal(err)
	}
	mk, _ := g.Authorize(modelSecret)
	if mk == nil || mk.ID != byModel.ID || !g.Allows(mk, "chat-a") || !g.Allows(mk, "test.chat") || !g.Allows(mk, "up-a") || g.Allows(mk, "emb-a") {
		t.Fatal("a model grant covers the model, its scenes and its upstream aliases")
	}
	if _, ok := g.Authorize(secret + "x"); ok {
		t.Fatal("a wrong secret must not authorize")
	}
	off := false
	if _, err := admin.UpdateAccessKey(ctx, key.ID, llm.AccessKeyInput{Enabled: &off}); err != nil {
		t.Fatal(err)
	}
	if _, ok := g.Authorize(secret); ok {
		t.Fatal("a disabled access key must stop working as soon as it is saved")
	}
}

func TestProviderKeysNeedAKEK(t *testing.T) {
	db := gatewayDB(t)
	admin := llm.NewAdmin(db, nil, llm.New(db, nil))
	name, base, key := "x", "https://example.com/v1", "sk-1234567890"
	if _, err := admin.CreateProvider(context.Background(), llm.ProviderInput{Name: &name, BaseURL: &base, APIKey: &key}); !errors.Is(err, llm.ErrNoKEK) {
		t.Fatalf("err = %v", err)
	}
}
