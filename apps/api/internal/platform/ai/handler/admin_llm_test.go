package handler

import (
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/middleware"
	aiPerm "api/internal/platform/ai/perm"
	"api/internal/platform/llm"
	"api/pkg/oidctoken"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func buildLLMAdminApp(t *testing.T) *fiber.App {
	t.Helper()
	for _, tbl := range []string{"llm_attempt", "llm_event", "llm_scene", "llm_price_revision", "llm_route", "llm_model", "llm_provider", "llm_access_key"} {
		require.NoError(t, testDB.Exec("TRUNCATE "+tbl+" RESTART IDENTITY CASCADE").Error)
	}
	kek, err := llm.ParseKEK(base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	require.NoError(t, err)
	gw := llm.New(testDB, kek)
	app := fiber.New()
	verifier := oidctoken.NewVerifier(adminTestSecret, nil)
	app.Use("/api/v1/admin/ai", middleware.JWTAuth(verifier), middleware.RequirePermission(aiPerm.Resolver, aiPerm.UsageView))
	app.Use("/api/v1/admin/ai/llm", middleware.RequirePermission(aiPerm.Resolver, aiPerm.LLMManage))
	SetupAdmin(app, nil, nil, llm.NewAdmin(testDB, kek, gw))
	return app
}

func TestLLMAdminIsRenOnlyForReadingAndWriting(t *testing.T) {
	app := buildLLMAdminApp(t)
	call := func(method, path, body string, roles ...string) (int, []byte) {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+adminToken(t, roles...))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		return send(t, app, publishedAdminSpec(t), req)
	}

	for _, roles := range [][]string{{"admin"}, {"moderator"}} {
		status, _ := call("GET", "/api/v1/admin/ai/llm/providers", "", roles...)
		require.Equal(t, fiber.StatusForbidden, status, "%v must not read the LLM config", roles)
		status, _ = call("POST", "/api/v1/admin/ai/llm/providers", `{"name":"x","base_url":"https://example.com/v1"}`, roles...)
		require.Equal(t, fiber.StatusForbidden, status, "%v must not write the LLM config", roles)
	}

	status, raw := call("POST", "/api/v1/admin/ai/llm/providers",
		`{"name":"cloudflare","base_url":"https://api.example.com/v1","api_key":"sk-live-secret-9876"}`, "admin", "ren")
	require.Equal(t, fiber.StatusOK, status, string(raw))
	require.NotContains(t, string(raw), "sk-live-secret", "a provider response must never echo its key")
	var created struct {
		Data llm.ProviderView `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.True(t, created.Data.KeyConfigured)
	require.Equal(t, "…9876", created.Data.KeyHint)

	status, raw = call("GET", "/api/v1/admin/ai/llm/providers", "", "ren")
	require.Equal(t, fiber.StatusOK, status)
	require.NotContains(t, string(raw), "sk-live-secret")

	status, raw = call("POST", "/api/v1/admin/ai/llm/models", `{"key":"chat-a","kind":"chat"}`, "ren")
	require.Equal(t, fiber.StatusOK, status, string(raw))
	status, raw = call("POST", "/api/v1/admin/ai/llm/routes", `{"model_id":1,"provider_id":1,"upstream_id":"@cf/x","price":{"input_micro":1,"cached_micro":0,"output_micro":2}}`, "ren")
	require.Equal(t, fiber.StatusOK, status, string(raw))
	status, raw = call("PUT", "/api/v1/admin/ai/llm/scenes/test.chat", `{"model_id":1,"fallback_model_ids":[],"class":"interactive","temperature":null,"max_output":0,"ttft_ms":0,"idle_ms":0,"total_ms":0}`, "ren")
	require.Equal(t, fiber.StatusOK, status, string(raw))
	status, raw = call("DELETE", "/api/v1/admin/ai/llm/providers/1", "", "ren")
	require.Equal(t, fiber.StatusConflict, status, "a provider with routes is in use: %s", raw)

	status, raw = call("POST", "/api/v1/admin/ai/llm/access-keys", `{"name":"catalog","allowed_models":["catalog.embed"]}`, "ren")
	require.Equal(t, fiber.StatusOK, status, string(raw))
	var key struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &key))
	require.True(t, strings.HasPrefix(key.Data.Secret, "nmk_"))
	status, raw = call("GET", "/api/v1/admin/ai/llm/access-keys", "", "ren")
	require.Equal(t, fiber.StatusOK, status)
	require.NotContains(t, string(raw), key.Data.Secret, "a secret is shown once, at creation")

	for _, path := range []string{"/status", "/models", "/routes", "/scenes", "/events"} {
		status, raw = call("GET", "/api/v1/admin/ai/llm"+path, "", "ren")
		require.Equal(t, fiber.StatusOK, status, "%s: %s", path, raw)
	}
}
