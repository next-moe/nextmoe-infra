package devapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
)

type lastUsedSkipStore struct {
	*memStore
}

func (s lastUsedSkipStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if strings.HasPrefix(key, "devkey:lastused:") {
		return 2, nil
	}
	return s.memStore.Incr(ctx, key, ttl)
}

type recordedDelta struct {
	face  string
	path  string
	count int64
	s4xx  int64
	s5xx  int64
}

func newFwdHarness() (*memStore, *ForwardAuth, *UsageRecorder) {
	mem := newMemStore()
	st := lastUsedSkipStore{mem}
	usage := NewUsageRecorder(nil, st)
	return mem, NewForwardAuth(NewMiddleware(nil, st), usage), usage
}

func fwdApp(h fiber.Handler) *fiber.App {
	app := fiber.New()
	app.Get("/internal/devapi/forward-auth", h)
	return app
}

func seedCachedCred(store *memStore, raw string, cred *Credential) {
	b, _ := json.Marshal(cred)
	store.kv[credCacheKey(hashHex(raw))] = b
}

func mustV2Key(t *testing.T) string {
	t.Helper()
	k, err := GenerateV2Key(true)
	if err != nil {
		t.Fatalf("GenerateV2Key: %v", err)
	}
	return k
}

func recorded(u *UsageRecorder) []recordedDelta {
	u.mu.Lock()
	defer u.mu.Unlock()
	out := make([]recordedDelta, 0, len(u.deltas))
	for k, d := range u.deltas {
		out = append(out, recordedDelta{k.face, k.path, d.count, d.s4xx, d.s5xx})
	}
	return out
}

func TestForwardAuthUnknownFace(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	seedCachedCred(store, raw, &Credential{KeyID: 1, ClientID: "c1", Tier: TierFree, Scopes: []string{ScopeMoyuRead}})

	app := fwdApp(fwd.Handle)
	req := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=not-a-face", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Errorf("status = %d, want 500", resp.StatusCode)
	}
	if n := len(recorded(usage)); n != 0 {
		t.Errorf("usage deltas = %d, want 0", n)
	}
}

func TestForwardAuthMissingKey(t *testing.T) {
	_, fwd, usage := newFwdHarness()
	app := fwdApp(fwd.Handle)
	resp, err := app.Test(httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil))
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if n := len(recorded(usage)); n != 0 {
		t.Errorf("usage deltas = %d, want 0", n)
	}
}

func TestForwardAuthUnresolvableKey(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	store.kv[credCacheKey(hashHex(raw))] = []byte{credCacheNeg}

	app := fwdApp(fwd.Handle)
	req := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	if n := len(recorded(usage)); n != 0 {
		t.Errorf("usage deltas = %d, want 0", n)
	}
}

func TestForwardAuthMissingScope(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	cred := &Credential{KeyID: 9, ClientID: "c-scope", Tier: TierFree, Scopes: []string{ScopeCatalogRead}}
	seedCachedCred(store, raw, cred)

	app := fwdApp(fwd.Handle)
	req := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusForbidden {
		t.Errorf("status = %d, want 403", resp.StatusCode)
	}
	got := recorded(usage)
	if len(got) != 1 {
		t.Fatalf("usage deltas = %d, want 1", len(got))
	}
	if got[0].face != "moyu" || got[0].path != "/v2/moyu/*" || got[0].count != 1 || got[0].s4xx != 1 || got[0].s5xx != 0 {
		t.Errorf("delta = %+v, want face=moyu path=/v2/moyu/* count=1 s4xx=1 s5xx=0", got[0])
	}
}

func TestForwardAuthSuccess(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	cred := &Credential{KeyID: 11, ClientID: "c-ok", Tier: TierFree, Scopes: []string{ScopeMoyuRead}}
	seedCachedCred(store, raw, cred)

	app := fiber.New()
	app.Get("/internal/devapi/forward-auth", fwd.Handle)
	app.Get("/v2/moyu/resources/:id", fwd.Handle)

	req := httptest.NewRequest("GET", "/v2/moyu/resources/123?face=moyu", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("app.Test: %v", err)
	}
	if resp.StatusCode != fiber.StatusNoContent {
		t.Errorf("status = %d, want 204", resp.StatusCode)
	}
	for _, h := range []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset", "X-Quota-Limit", "X-Quota-Remaining"} {
		if resp.Header.Get(h) == "" {
			t.Errorf("missing header %s", h)
		}
	}
	if got := resp.Header.Get("X-NextMoe-Client-Id"); got != cred.ClientID {
		t.Errorf("X-NextMoe-Client-Id = %q, want %q", got, cred.ClientID)
	}
	if got := resp.Header.Get("X-NextMoe-Key-Id"); got != strconv.FormatUint(uint64(cred.KeyID), 10) {
		t.Errorf("X-NextMoe-Key-Id = %q, want %d", got, cred.KeyID)
	}
	if got := resp.Header.Get("X-NextMoe-Tier"); got != cred.Tier {
		t.Errorf("X-NextMoe-Tier = %q, want %q", got, cred.Tier)
	}

	got := recorded(usage)
	if len(got) != 1 {
		t.Fatalf("usage deltas = %d, want 1", len(got))
	}
	if got[0].face != "moyu" || got[0].path != "/v2/moyu/*" {
		t.Errorf("recorded face/path = %q %q, want moyu /v2/moyu/* (request URI was /v2/moyu/resources/123)", got[0].face, got[0].path)
	}
	if got[0].count != 1 || got[0].s4xx != 0 || got[0].s5xx != 0 {
		t.Errorf("delta = %+v, want count=1 zero error buckets", got[0])
	}
}

func TestForwardAuthRateLimited(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	cred := &Credential{KeyID: 13, ClientID: "c-rate", Tier: TierFree, Scopes: []string{ScopeMoyuRead}, RateOverride: 1}
	seedCachedCred(store, raw, cred)

	app := fwdApp(fwd.Handle)
	first := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	first.Header.Set("Authorization", "Bearer "+raw)
	resp1, err := app.Test(first)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if resp1.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first status = %d, want 204", resp1.StatusCode)
	}

	second := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	second.Header.Set("Authorization", "Bearer "+raw)
	resp2, err := app.Test(second)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if resp2.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("second status = %d, want 429", resp2.StatusCode)
	}
	if resp2.Header.Get("Retry-After") == "" {
		t.Errorf("429 missing Retry-After header")
	}
	got := recorded(usage)
	if len(got) != 1 {
		t.Fatalf("usage deltas = %d, want 1", len(got))
	}
	if got[0].count != 2 || got[0].s4xx != 1 {
		t.Errorf("delta = %+v, want count=2 s4xx=1 (429 recorded)", got[0])
	}
}

func TestForwardAuthQuotaExceeded(t *testing.T) {
	store, fwd, usage := newFwdHarness()
	raw := mustV2Key(t)
	cred := &Credential{KeyID: 14, ClientID: "c-quota", Tier: TierFree, Scopes: []string{ScopeMoyuRead}, QuotaOverride: 1}
	seedCachedCred(store, raw, cred)

	app := fwdApp(fwd.Handle)
	first := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	first.Header.Set("Authorization", "Bearer "+raw)
	resp1, err := app.Test(first)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if resp1.StatusCode != fiber.StatusNoContent {
		t.Fatalf("first status = %d, want 204", resp1.StatusCode)
	}

	second := httptest.NewRequest("GET", "/internal/devapi/forward-auth?face=moyu", nil)
	second.Header.Set("Authorization", "Bearer "+raw)
	resp2, err := app.Test(second)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if resp2.StatusCode != fiber.StatusTooManyRequests {
		t.Errorf("second status = %d, want 429", resp2.StatusCode)
	}
	got := recorded(usage)
	if len(got) != 1 {
		t.Fatalf("usage deltas = %d, want 1", len(got))
	}
	if got[0].count != 2 || got[0].s4xx != 1 {
		t.Errorf("delta = %+v, want count=2 s4xx=1 (429 recorded)", got[0])
	}
}
