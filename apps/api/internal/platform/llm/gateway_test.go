package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func sse(frames ...string) string {
	var b strings.Builder
	for _, f := range frames {
		b.WriteString("data: " + f + "\n\n")
	}
	return b.String()
}

func streamServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != http.StatusOK {
			http.Error(w, body, status)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const okStream = `{"model":"m","choices":[{"delta":{"reasoning":"think","reasoning_content":"think"}}]}`

func okFrames(text string) []string {
	return []string{
		okStream,
		fmt.Sprintf(`{"choices":[{"delta":{"content":%q}}]}`, text),
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"search","arguments":"{\"q\":"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":40}}}`,
		"[DONE]",
	}
}

func testGateway(t *testing.T, urls ...string) *Gateway {
	t.Helper()
	g := New(nil, nil)
	s := &snapshot{
		providers: map[int64]*Provider{}, models: map[int64]*Model{1: {ID: 1, Key: "m1", Enabled: true}},
		routes: map[int64][]*Route{}, scenes: map[string]*Scene{
			"s": {Key: "s", ModelID: 1, TTFTMs: 2000, IdleMs: 2000, TotalMs: 5000},
		},
		prices: map[int64]*PriceRevision{}, keys: map[int64]string{}, headers: map[int64]map[string]string{},
	}
	for i, u := range urls {
		id := int64(i + 1)
		s.providers[id] = &Provider{ID: id, Name: fmt.Sprint("p", id), BaseURL: u, Enabled: true}
		s.routes[1] = append(s.routes[1], &Route{ID: id, ModelID: 1, ProviderID: id, UpstreamID: "up", Priority: len(urls) - i, Weight: 100, Status: RouteActive})
	}
	g.snap.Store(s)
	return g
}

func TestStreamParsesTextReasoningToolsAndUsage(t *testing.T) {
	srv := streamServer(t, http.StatusOK, sse(okFrames("hello")...))
	g := testGateway(t, srv.URL)
	var events []Event
	res, err := g.Stream(context.Background(), Call{Scene: "s"}, func(ev Event) { events = append(events, ev) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hello" || res.Reasoning != "think" || res.FinishReason != "tool_calls" || res.Model != "m" {
		t.Fatalf("result = %+v", res)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].Name != "search" || res.ToolCalls[0].Arguments != `{"q":"x"}` {
		t.Fatalf("tool calls = %+v", res.ToolCalls)
	}
	if res.Usage.InputTokens != 100 || res.Usage.CachedTokens != 40 || res.Usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v", res.Usage)
	}
	if len(events) != 2 || events[0].Kind != EventReasoning || events[1].Text != "hello" {
		t.Fatalf("events = %+v", events)
	}
}

func TestStreamAcceptsNumericContentTokens(t *testing.T) {
	srv := streamServer(t, http.StatusOK, sse(`{"choices":[{"delta":{"content":"第"}}]}`, `{"choices":[{"delta":{"content":1}}]}`,
		`{"choices":[{"delta":{"content":"部"},"finish_reason":"stop"}]}`, "[DONE]"))
	g := testGateway(t, srv.URL)
	res, err := g.Stream(context.Background(), Call{Scene: "s"}, func(Event) {})
	if err != nil || res.Text != "第1部" {
		t.Fatalf("res=%q err=%v", res.Text, err)
	}
}

func TestStreamFailsOverBeforeTheFirstContent(t *testing.T) {
	bad := streamServer(t, http.StatusServiceUnavailable, `{"errors":[{"message":"AiError: Service temporarily at capacity","code":4006}]}`)
	good := streamServer(t, http.StatusOK, sse(okFrames("from good")...))
	g := testGateway(t, bad.URL, good.URL)
	var text strings.Builder
	res, err := g.Stream(context.Background(), Call{Scene: "s"}, func(ev Event) {
		if ev.Kind == EventText {
			text.WriteString(ev.Text)
		}
	})
	if err != nil || res.Text != "from good" || text.String() != "from good" {
		t.Fatalf("res=%+v err=%v streamed=%q", res, err, text.String())
	}
}

func TestStreamDoesNotFailOverAfterCommitting(t *testing.T) {
	broken := streamServer(t, http.StatusOK, sse(`{"choices":[{"delta":{"content":"partial"}}]}`))
	good := streamServer(t, http.StatusOK, sse(okFrames("other")...))
	g := testGateway(t, broken.URL, good.URL)
	var text strings.Builder
	_, err := g.Stream(context.Background(), Call{Scene: "s"}, func(ev Event) { text.WriteString(ev.Text) })
	if KindOf(err) != ErrKindNetwork || text.String() != "partial" {
		t.Fatalf("err=%v streamed=%q; want the committed route's failure and no splice", err, text.String())
	}
}

func TestBufferedCallStillFailsOverAfterContent(t *testing.T) {
	broken := streamServer(t, http.StatusOK, sse(`{"choices":[{"delta":{"content":"{\"a\":"}}]}`))
	good := streamServer(t, http.StatusOK, sse(`{"choices":[{"delta":{"content":"{}"},"finish_reason":"stop"}]}`, "[DONE]"))
	g := testGateway(t, broken.URL, good.URL)
	res, err := g.Stream(context.Background(), Call{Scene: "s", Buffered: true}, nil)
	if err != nil || res.Text != "{}" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestContentFilterIsNotRetried(t *testing.T) {
	filtered := streamServer(t, http.StatusBadRequest, `Request blocked under the model provider's usage guidelines`)
	good := streamServer(t, http.StatusOK, sse(okFrames("x")...))
	g := testGateway(t, filtered.URL, good.URL)
	if _, err := g.Stream(context.Background(), Call{Scene: "s"}, nil); KindOf(err) != ErrKindContentFiltered {
		t.Fatalf("err = %v", err)
	}
}

func TestClassifyHTTP(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		want   ErrorKind
	}{
		{429, `{"code":3021}`, ErrKindRateLimit},
		{401, `bad key`, ErrKindAuth},
		{400, `This model's maximum context length is 32768 tokens`, ErrKindContextLength},
		{503, `Unknown internal error (code 3048)`, ErrKindUpstream},
		{400, `余额不足`, ErrKindQuota},
		{400, `unknown parameter: foo`, ErrKindBadRequest},
	} {
		if got := classifyHTTP(tc.status, tc.body).Kind; got != tc.want {
			t.Errorf("classify(%d, %q) = %s, want %s", tc.status, tc.body, got, tc.want)
		}
	}
}

func TestRendezvousIsStablePerKey(t *testing.T) {
	routes := []*Route{{ID: 1, Weight: 100}, {ID: 2, Weight: 100}, {ID: 3, Weight: 100}}
	first := rendezvous(routes, "conv-1")[0].ID
	for range 20 {
		if rendezvous(routes, "conv-1")[0].ID != first {
			t.Fatal("the same affinity key must pick the same route")
		}
	}
	hits := map[int64]int{}
	for i := range 3000 {
		hits[rendezvous(routes, fmt.Sprint("k", i))[0].ID]++
	}
	for id, n := range hits {
		if n < 800 || n > 1200 {
			t.Fatalf("route %d won %d of 3000 keys; weights are equal", id, n)
		}
	}
	gone := rendezvous(routes[:2], "conv-1")[0].ID
	if first != 3 && gone != first {
		t.Fatal("dropping another route must not move this key")
	}
}

func TestBreakerOpensAndSuspends(t *testing.T) {
	now := time.Unix(1000, 0)
	h := newHealth(func() time.Time { return now })
	for range 2 {
		h.failure(1, ErrKindUpstream)
	}
	if _, blocked := h.blockedUntil(1); blocked {
		t.Fatal("two transient failures must not open the breaker")
	}
	h.failure(1, ErrKindUpstream)
	if _, blocked := h.blockedUntil(1); !blocked {
		t.Fatal("the third transient failure opens the breaker")
	}
	h.success(1)
	if _, blocked := h.blockedUntil(1); blocked {
		t.Fatal("success closes the breaker")
	}
	if h.failure(2, ErrKindAuth) || h.failure(2, ErrKindAuth) || !h.failure(2, ErrKindAuth) {
		t.Fatal("the third auth failure in a row suspends the route")
	}
}

func TestSealedKeyRoundTrip(t *testing.T) {
	kek, err := ParseKEK("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealKey(kek, "sk-secret")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := openKey(kek, sealed); err != nil || got != "sk-secret" {
		t.Fatalf("got %q err %v", got, err)
	}
}

func TestCostNano(t *testing.T) {
	p := &PriceRevision{InputMicro: 440_000, CachedMicro: 14_000, OutputMicro: 1_320_000}
	got := costNano(Usage{InputTokens: 1_000_000, CachedTokens: 500_000, OutputTokens: 1_000_000}, p, 0)
	want := int64(500_000*440_000+500_000*14_000+1_000_000*1_320_000) / 1000
	if got != want {
		t.Fatalf("cost = %d, want %d", got, want)
	}
}
