package proxy_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	aimigrate "api/internal/platform/ai/migrate"
	"api/internal/platform/llm"
	"api/internal/platform/llm/proxy"
	"api/internal/testsupport/dbtest"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func upstream(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			if _, ok := body["response_format"]; !ok {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"response_format was not passed through"}`))
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"hmm\"}}]}\n\n" +
				"data: {\"model\":\"served-x\",\"choices\":[{\"delta\":{\"content\":\"hello\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"))
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			if strings.Contains(string(body["input"]), "huge") {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"errors":[{"message":"AiError: input too big","code":3030}]}`))
				return
			}
			_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,2]}],"usage":{"prompt_tokens":1}}`))
		case strings.HasSuffix(r.URL.Path, "/moderations"):
			_, _ = w.Write([]byte(`{"results":[{"flagged":true}]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func setup(t *testing.T) (*fiber.App, string) {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
		return nil, ""
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := aimigrate.Run(db); err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"llm_attempt", "llm_event", "llm_scene", "llm_price_revision", "llm_route", "llm_model", "llm_provider", "llm_access_key"} {
		db.Exec("TRUNCATE " + tbl + " RESTART IDENTITY CASCADE")
	}
	kek, _ := llm.ParseKEK(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	gw := llm.New(db, kek)
	admin := llm.NewAdmin(db, kek, gw)
	ctx := context.Background()
	str := func(s string) *string { return &s }
	i64 := func(n int64) *int64 { return &n }
	up := upstream(t)
	p, err := admin.CreateProvider(ctx, llm.ProviderInput{Name: str("up"), BaseURL: str(up.URL), APIKey: str("sk-upstream-1")})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []struct{ key, kind, upstream string }{
		{"chat-a", llm.ModelChat, "@cf/chat-a"}, {"emb-a", llm.ModelEmbedding, "@cf/qwen/qwen3-embedding-0.6b"}, {"mod-a", llm.ModelModeration, "omni-x"},
	} {
		mv, err := admin.CreateModel(ctx, llm.ModelInput{Key: str(m.key), Kind: str(m.kind)})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := admin.CreateRoute(ctx, llm.RouteInput{ModelID: i64(mv.ID), ProviderID: i64(p.ID), UpstreamID: str(m.upstream)}); err != nil {
			t.Fatal(err)
		}
	}
	_, secret, err := admin.CreateAccessKey(ctx, llm.AccessKeyInput{Name: str("catalog"), AllowedModels: &[]string{"chat-a", "emb-a", "mod-a"}})
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New()
	proxy.Mount(app, &proxy.Server{GW: gw})
	return app, secret
}

func do(t *testing.T, app *fiber.App, secret, path, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if path == "/v1/models" {
		req = httptest.NewRequest(http.MethodGet, path, nil)
	}
	if secret != "" {
		req.Header.Set("Authorization", "Bearer "+secret)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 0})
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

func TestOpenAICompatibleEndpoints(t *testing.T) {
	app, secret := setup(t)
	if app == nil {
		return
	}
	if status, _ := do(t, app, "", "/v1/models", ""); status != http.StatusUnauthorized {
		t.Fatalf("no key = %d", status)
	}
	if status, _ := do(t, app, "nmk_wrong", "/v1/chat/completions", `{"model":"chat-a"}`); status != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d", status)
	}
	if status, body := do(t, app, secret, "/v1/models", ""); status != 200 || !strings.Contains(body, `"id":"chat-a"`) || !strings.Contains(body, `"id":"emb-a"`) {
		t.Fatalf("models = %d %s", status, body)
	}

	status, body := do(t, app, secret, "/v1/chat/completions",
		`{"model":"@cf/chat-a","messages":[{"role":"user","content":"hi"}],"response_format":{"type":"json_object"},"max_tokens":32}`)
	if status != 200 {
		t.Fatalf("chat = %d %s", status, body)
	}
	var cc struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning_content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body), &cc); err != nil || cc.Choices[0].Message.Content != "hello" || cc.Choices[0].Message.Reasoning != "hmm" ||
		cc.Choices[0].FinishReason != "stop" || cc.Usage.PromptTokens != 5 || cc.Model != "served-x" {
		t.Fatalf("chat completion = %s", body)
	}

	status, body = do(t, app, secret, "/v1/chat/completions",
		`{"model":"chat-a","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}],"response_format":{"type":"json_object"}}`)
	if status != 200 || !strings.Contains(body, `"content":"hello"`) || !strings.Contains(body, `"reasoning_content":"hmm"`) ||
		!strings.Contains(body, `"finish_reason":"stop"`) || !strings.Contains(body, `"prompt_tokens":5`) || !strings.HasSuffix(strings.TrimSpace(body), "data: [DONE]") {
		t.Fatalf("stream = %d %s", status, body)
	}

	if status, body = do(t, app, secret, "/v1/embeddings", `{"model":"@cf/qwen/qwen3-embedding-0.6b","input":["a"]}`); status != 200 || !strings.Contains(body, `"embedding":[1,2]`) {
		t.Fatalf("embeddings = %d %s", status, body)
	}
	if status, body = do(t, app, secret, "/v1/embeddings", `{"model":"emb-a","input":["huge"]}`); status != http.StatusBadRequest || !strings.Contains(body, "input too big") {
		t.Fatalf("an upstream 400 must pass through with its text, so the embedder can halve its batch: %d %s", status, body)
	}
	if status, body = do(t, app, secret, "/v1/moderations", `{"model":"mod-a","input":"x"}`); status != 200 || !strings.Contains(body, `"flagged":true`) {
		t.Fatalf("moderations = %d %s", status, body)
	}
	if status, _ = do(t, app, secret, "/v1/embeddings", `{"model":"chat-a","input":["a"]}`); status != http.StatusBadRequest {
		t.Fatalf("embeddings through a chat model = %d", status)
	}
	if status, _ = do(t, app, secret, "/v1/chat/completions", `{"model":"nope","messages":[{"role":"user","content":"x"}]}`); status != http.StatusForbidden {
		t.Fatalf("a model outside the key's scope = %d", status)
	}
}
