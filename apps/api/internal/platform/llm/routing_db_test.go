package llm_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/llm"
)

type scriptedUpstream struct {
	mu    sync.Mutex
	calls map[string]int
	srv   *httptest.Server
}

func newScriptedUpstream(t *testing.T) *scriptedUpstream {
	u := &scriptedUpstream{calls: map[string]int{}}
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = decodeJSON(r, &body)
		u.mu.Lock()
		u.calls[body.Model]++
		u.mu.Unlock()
		switch {
		case body.Model == "busy":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"slow down"}`))
		case body.Model == "small":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"This model's maximum context length is 8192 tokens"}`))
		case body.Model == "strict":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"blocked by content policy"}`))
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"" + body.Model + "\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
		}
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *scriptedUpstream) count(model string) int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.calls[model]
}

func TestRoutingHonoursRetryAfterLimitsAndErrorClassFallbacks(t *testing.T) {
	db := gatewayDB(t)
	kek := testKEK(t)
	ctx := context.Background()
	g := llm.New(db, kek)
	a := llm.NewAdmin(db, kek, g)
	up := newScriptedUpstream(t)
	str := func(s string) *string { return &s }
	i64 := func(n int64) *int64 { return &n }
	intp := func(n int) *int { return &n }
	p, err := a.CreateProvider(ctx, llm.ProviderInput{Name: str("p"), BaseURL: str(up.srv.URL), APIKey: str("k")})
	if err != nil {
		t.Fatal(err)
	}
	model := func(key string, routes ...llm.RouteInput) int64 {
		t.Helper()
		m, err := a.CreateModel(ctx, llm.ModelInput{Key: str(key), Kind: str(llm.ModelChat)})
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range routes {
			r.ModelID, r.ProviderID = i64(m.ID), i64(p.ID)
			if _, err := a.CreateRoute(ctx, r); err != nil {
				t.Fatal(err)
			}
		}
		return m.ID
	}
	ask := func(scene string) (string, error) {
		res, err := g.Stream(ctx, llm.Call{Scene: scene, Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}, func(llm.Event) {})
		return res.Text, err
	}

	// Retry-After: one 429 cools the route for its stated time.
	mixed := model("mixed", llm.RouteInput{UpstreamID: str("busy"), Priority: intp(10)}, llm.RouteInput{UpstreamID: str("calm"), Priority: intp(0)})
	if _, err := a.PutScene(ctx, "t.retry", llm.SceneInput{ModelID: mixed}); err != nil {
		t.Fatal(err)
	}
	_ = g.Refresh(ctx)
	for range 2 {
		if text, err := ask("t.retry"); err != nil || text != "calm" {
			t.Fatalf("falls over to the calm route: %q %v", text, err)
		}
	}
	if up.count("busy") != 1 {
		t.Fatalf("after a 429 with Retry-After the busy route is skipped, called %d times", up.count("busy"))
	}

	// Context and content fallbacks.
	small, strict, roomy := model("small", llm.RouteInput{UpstreamID: str("small")}), model("strict", llm.RouteInput{UpstreamID: str("strict")}), model("roomy", llm.RouteInput{UpstreamID: str("roomy")})
	if _, err := a.PutScene(ctx, "t.context", llm.SceneInput{ModelID: small, ContextFallback: []int64{roomy}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutScene(ctx, "t.content", llm.SceneInput{ModelID: strict, ContentFallback: []int64{roomy}}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutScene(ctx, "t.nofallback", llm.SceneInput{ModelID: strict}); err != nil {
		t.Fatal(err)
	}
	_ = g.Refresh(ctx)
	if text, err := ask("t.context"); err != nil || text != "roomy" {
		t.Fatalf("too long goes to the context fallback: %q %v", text, err)
	}
	if text, err := ask("t.content"); err != nil || text != "roomy" {
		t.Fatalf("a content refusal goes to the content fallback: %q %v", text, err)
	}
	if _, err := ask("t.nofallback"); llm.KindOf(err) != llm.ErrKindContentFiltered {
		t.Fatalf("without a content fallback the refusal is returned, err=%v", err)
	}

	// Per-minute limits skip a route without counting it as failed.
	limited := model("limited", llm.RouteInput{UpstreamID: str("first"), Priority: intp(10), RPM: intp(1)}, llm.RouteInput{UpstreamID: str("second"), Priority: intp(0)})
	only := model("only", llm.RouteInput{UpstreamID: str("only"), RPM: intp(1)})
	if _, err := a.PutScene(ctx, "t.limit", llm.SceneInput{ModelID: limited}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.PutScene(ctx, "t.only", llm.SceneInput{ModelID: only}); err != nil {
		t.Fatal(err)
	}
	_ = g.Refresh(ctx)
	first, _ := ask("t.limit")
	second, _ := ask("t.limit")
	if first != "first" || second != "second" {
		t.Fatalf("rpm=1 sends the second call to the next route: %q then %q", first, second)
	}
	if _, err := ask("t.only"); err != nil {
		t.Fatal(err)
	}
	_, err = ask("t.only")
	if llm.KindOf(err) != llm.ErrKindRateLimit || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("every route at its limit answers rate_limit, err=%v", err)
	}

	health, err := a.Health(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range health {
		if h.UpstreamID == "busy" && (h.State != "cooling" || h.Health.CoolUntil == nil) {
			t.Fatalf("the health panel shows the cooling route: %+v", h)
		}
	}
	plan, err := a.PlanScene(ctx, "t.retry", "")
	if err != nil || len(plan.Steps) != 1 || plan.Steps[0].UpstreamID != "calm" || len(plan.Skipped) != 1 {
		t.Fatalf("the plan lists the calm route and the skipped busy one: %+v err=%v", plan, err)
	}
	for _, h := range health {
		if h.UpstreamID == "busy" {
			if err := a.ResetCooldown(ctx, h.RouteID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if plan, _ := a.PlanScene(ctx, "t.retry", ""); len(plan.Steps) != 2 || plan.Steps[0].UpstreamID != "busy" {
		t.Fatalf("after a reset the busy route is back first: %+v", plan)
	}
}
