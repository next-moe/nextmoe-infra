package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"

	"api/internal/platform/llm"
)

type countTool struct{ n int }

func (c *countTool) Spec() llm.Tool { return llm.Tool{Name: "count"} }
func (c *countTool) Run(context.Context, json.RawMessage) (Output, error) {
	c.n++
	return Output{Text: strings.Repeat("字", modelOutputCap)}, nil
}

func TestRunReusesIdenticalCallsAndEnforcesTheBudget(t *testing.T) {
	ct := &countTool{}
	run := NewRegistry(ct).NewRun(2)
	a := run.Call(context.Background(), "count", `{"x":1}`)
	b := run.Call(context.Background(), "count", `{ "x" : 1 }`)
	if ct.n != 1 || a.Text != b.Text {
		t.Fatalf("identical arguments must reuse the first result, runs=%d", ct.n)
	}
	if len(a.Text) > modelOutputCap+len("…") {
		t.Fatalf("output must be clipped, got %d bytes", len(a.Text))
	}
	run.Call(context.Background(), "count", `{"x":2}`)
	if !run.Exhausted() {
		t.Fatal("two distinct calls spend a budget of two")
	}
	if out := run.Call(context.Background(), "count", `{"x":3}`); !strings.Contains(out.Text, "budget") || ct.n != 2 {
		t.Fatalf("over budget must not run the tool: %q runs=%d", out.Text, ct.n)
	}
	if out := run.Call(context.Background(), "count", `not json`); !strings.Contains(out.Text, "JSON object") {
		t.Fatalf("bad arguments: %q", out.Text)
	}
}

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "127.0.0.1": false, "10.1.2.3": false, "192.168.1.1": false,
		"169.254.169.254": false, "100.64.0.1": false, "::1": false, "fe80::1": false, "::ffff:127.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("publicAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestReadURLRefusesPrivateAddresses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("secret"))
	}))
	defer srv.Close()
	out, err := (&ReadURL{HTTP: NewPublicHTTPClient()}).Run(context.Background(), json.RawMessage(`{"url":"`+srv.URL+`"}`))
	if err != nil || !strings.Contains(out.Text, "not reachable") {
		t.Fatalf("out=%q err=%v", out.Text, err)
	}
}

func TestWebSearchRunsAtMostTwicePerAnswerAndCapsSources(t *testing.T) {
	calls := 0
	ws := &WebSearch{Search: func(ctx context.Context, query, instructions string) (llm.SearchResult, error) {
		calls++
		var cites []llm.Citation
		for i := range 20 {
			cites = append(cites, llm.Citation{URL: "https://example.com/" + strings.Repeat("a", i+1), Title: "t"})
		}
		return llm.SearchResult{Text: "summary for " + query, Citations: cites}, nil
	}}
	run := NewRegistry(ws).NewRun(10)
	first := run.Call(context.Background(), "web_search", `{"query":"a"}`)
	if len(first.Sources) != maxWebSources || !strings.Contains(first.Text, "summary for a") {
		t.Fatalf("sources must be capped at %d: got %d, text %q", maxWebSources, len(first.Sources), first.Text)
	}
	run.Call(context.Background(), "web_search", `{"query":"b"}`)
	third := run.Call(context.Background(), "web_search", `{"query":"c"}`)
	if calls != 2 || !strings.Contains(third.Text, "only 2 times") {
		t.Fatalf("a third distinct search must be refused without calling out: calls=%d text=%q", calls, third.Text)
	}
}

func TestStripTags(t *testing.T) {
	got := stripTags(`<html><head><title>x</title></head><body><script>evil()</script><p>Hello&nbsp;<b>world</b></p></body></html>`)
	if strings.Contains(got, "evil") || !strings.Contains(got, "Hello") || !strings.Contains(got, "world") {
		t.Fatalf("stripTags = %q", got)
	}
}
