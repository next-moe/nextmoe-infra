package mcpface

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var fixtureSpec = []byte(`{
  "paths": {
    "/v2/catalog/works": {
      "get": {
        "operationId": "listCatalogWorks",
        "summary": "List works",
        "parameters": [
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "view", "in": "query", "schema": {"type": "string"}},
          {"name": "fields", "in": "query", "schema": {"type": "string"}}
        ]
      }
    },
    "/v2/problems": {
      "get": {"operationId": "listProblemTypes", "summary": "Problem types"}
    }
  }
}`)

func TestToolRegistry(t *testing.T) {
	ctx := context.Background()
	server, _, err := NewServer(NewUpstream("http://127.0.0.1:0"), fixtureSpec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}

	got := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
		if tool.InputSchema == nil {
			t.Errorf("tool %q has a nil input schema", tool.Name)
		}
		if tool.Description == "" {
			t.Errorf("tool %q has an empty description", tool.Name)
		}
	}
	sort.Strings(got)
	want := []string{"listCatalogWorks", "listProblemTypes"}
	if len(got) != len(want) {
		t.Fatalf("tool count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("tool[%d] = %q, want %q", i, got[i], name)
		}
	}
	descs, err := ToolsFromSpec(fixtureSpec)
	if err != nil {
		t.Fatal(err)
	}
	var works ToolDesc
	for _, d := range descs {
		if d.Name == "listCatalogWorks" {
			works = d
		}
	}
	have := map[string]bool{}
	for _, p := range works.Params {
		have[p] = true
	}
	for _, p := range []string{"q", "view", "fields"} {
		if !have[p] {
			t.Errorf("listCatalogWorks missing param %s", p)
		}
	}
}

func TestBearerTokenFormCheck(t *testing.T) {
	tests := []struct {
		name      string
		header    http.Header
		wantOK    bool
		wantToken string
	}{
		{"missing", http.Header{}, false, ""},
		{"nil header", nil, false, ""},
		{"non-bearer scheme", http.Header{"Authorization": {"Basic nmk_live_x"}}, false, ""},
		{"bearer non-nmk token (e.g. a JWT)", http.Header{"Authorization": {"Bearer eyJhbGci.foo.bar"}}, false, ""},
		{"bearer v1 key rejected", http.Header{"Authorization": {"Bearer nm_live_abc123"}}, false, ""},
		{"bearer live key", http.Header{"Authorization": {"Bearer nmk_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAA"}}, true, "nmk_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			token, ok := bearerToken(tc.header)
			if ok != tc.wantOK || token != tc.wantToken {
				t.Errorf("bearerToken() = (%q, %v), want (%q, %v)", token, ok, tc.wantToken, tc.wantOK)
			}
		})
	}
}

func TestUpstreamGetPassthrough(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	up := NewUpstream(srv.URL)
	q := url.Values{}
	q.Set("q", "スモーク")
	status, body, err := up.Get(context.Background(), "/v1/catalog/search", q, "Bearer nm_live_k")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if status != http.StatusOK {
		t.Errorf("status = %d, want 200", status)
	}
	if string(body) != `{"data":{"ok":true}}` {
		t.Errorf("body = %s", body)
	}
	if gotPath != "/v1/catalog/search" {
		t.Errorf("upstream path = %q", gotPath)
	}
	if gotQuery != "q=%E3%82%B9%E3%83%A2%E3%83%BC%E3%82%AF" {
		t.Errorf("upstream query = %q", gotQuery)
	}
	if gotAuth != "Bearer nm_live_k" {
		t.Errorf("forwarded auth = %q", gotAuth)
	}
}

func TestMapUpstream(t *testing.T) {
	if r := mapUpstream(200, []byte(`{"x":1}`)); r.IsError {
		t.Error("200 must not be an error result")
	} else if textOf(t, r) != `{"x":1}` {
		t.Errorf("200 body = %q", textOf(t, r))
	}

	for _, status := range []int{401, 403, 404, 429, 500} {
		r := mapUpstream(status, []byte(`{"error":"nope"}`))
		if !r.IsError {
			t.Errorf("status %d must be an error result", status)
		}
		if txt := textOf(t, r); !contains(txt, "nope") {
			t.Errorf("status %d result should carry the upstream body, got %q", status, txt)
		}
	}
	if txt := textOf(t, mapUpstream(429, nil)); !contains(txt, devPortalURL) {
		t.Errorf("429 hint missing portal pointer: %q", txt)
	}
}

func TestUpstreamGetTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	up := NewUpstream(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := up.Get(ctx, "/v1/catalog/search", url.Values{}, "Bearer nm_live_k"); err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
}

func TestHandlerEndToEnd(t *testing.T) {
	var gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"object":"list","items":[]}`))
	}))
	defer srv.Close()

	key := "nmk_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	td := ToolDesc{Name: "listCatalogWorks", Path: "/v2/catalog/works", Params: []string{"q", "view"}, NeedsKey: true}
	h := (&toolsRunner{up: NewUpstream(srv.URL)}).handler(td)
	ctx := context.Background()
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{
		Header: http.Header{"Authorization": {"Bearer " + key}},
	}}
	res, _, err := h(ctx, req, map[string]any{"q": "fate", "view": "basic"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if res.IsError {
		t.Fatalf("handler returned an error result: %q", textOf(t, res))
	}
	if gotPath != "/v2/catalog/works" {
		t.Errorf("path = %q", gotPath)
	}
	if !contains(gotQuery, "q=fate") || !contains(gotQuery, "view=basic") {
		t.Errorf("query = %q", gotQuery)
	}
	if gotAuth != "Bearer "+key {
		t.Errorf("auth = %q", gotAuth)
	}

	noKey := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: http.Header{}}}
	res2, _, err := h(ctx, noKey, map[string]any{"q": "fate"})
	if err != nil {
		t.Fatalf("handler err: %v", err)
	}
	if !res2.IsError {
		t.Fatal("missing key must yield an error result")
	}
	if !contains(textOf(t, res2), devPortalURL) {
		t.Errorf("auth error should point at the portal: %q", textOf(t, res2))
	}
}

func TestSpecToolsSubstitutePathAndSkipKeyOnNews(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"object":"news_item"}`))
	}))
	defer srv.Close()

	td := ToolDesc{Name: "getNewsItem", Path: "/v2/news/{id}", Params: []string{"id"}, Required: []string{"id"}, NeedsKey: false}
	h := (&toolsRunner{up: NewUpstream(srv.URL)}).handler(td)
	res, _, err := h(context.Background(), &mcp.CallToolRequest{}, map[string]any{"id": "42"})
	if err != nil {
		t.Fatalf("news: %v", err)
	}
	if res.IsError {
		t.Fatalf("news error: %q", textOf(t, res))
	}
	if gotPath != "/v2/news/42" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("news must not require a key, got auth %q", gotAuth)
	}

	covers := ToolDesc{Name: "getCatalogWorkCovers", Path: "/v2/catalog/works/{id}/covers", Params: []string{"id", "limit"}, NeedsKey: true}
	h = (&toolsRunner{up: NewUpstream(srv.URL)}).handler(covers)
	key := "nmk_live_AAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: http.Header{"Authorization": {"Bearer " + key}}}}
	if _, _, err := h(context.Background(), req, map[string]any{"id": "7", "limit": "5"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/v2/catalog/works/7/covers" {
		t.Errorf("covers path = %q", gotPath)
	}
}

func textOf(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) == 0 {
		t.Fatal("result has no content")
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("content[0] is %T, want *mcp.TextContent", r.Content[0])
	}
	return tc.Text
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
