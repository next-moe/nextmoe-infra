package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"api/internal/platform/agent/model"
	"api/internal/platform/agent/tools"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"gorm.io/datatypes"
)

type topicCards struct {
	Cards []map[string]any `json:"cards"`
}

func fakeSiteMCP(t *testing.T) (*httptest.Server, *[]string) {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "kungal", Version: "1"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "search_topics", Description: "Search forum topics"},
		func(ctx context.Context, req *mcp.CallToolRequest, in struct {
			Query string `json:"query"`
		}) (*mcp.CallToolResult, topicCards, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "2 topics about " + in.Query}}}, topicCards{Cards: []map[string]any{
				{"kind": "kungal:topic", "id": "88", "title": "猫耳作品讨论", "summary": "大家推荐的猫耳作品", "url": "/topic/88", "cover": "http://insecure.example/a.png"},
				{"kind": "work", "id": "15", "title": "冒充共享作品", "url": "/galgame/15"},
				{"kind": "kungal:topic", "id": "89", "title": "外链", "url": "https://evil.example/x"},
			}}, nil
		})
	mcp.AddTool(server, &mcp.Tool{Name: "delete_topic", Description: "Not allowed"},
		func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "deleted"}}}, nil, nil
		})
	var mu sync.Mutex
	var seen []string
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

func TestSiteToolsListCallForwardTheUserAndKeepOnlyOwnCards(t *testing.T) {
	testDB.Exec("TRUNCATE agent_site_tool RESTART IDENTITY")
	ctx := context.Background()
	srv, seen := fakeSiteMCP(t)
	st := NewSiteTools(testDB, http.DefaultClient)
	row := &model.SiteTool{SiteID: 2, Name: "kungal", URL: srv.URL, Auth: model.SiteToolAuthUser, CardPrefix: "kungal",
		Allowed: datatypes.JSON(`["search_topics"]`), TimeoutMs: 5000, Enabled: true}
	if err := st.Save(ctx, row); err != nil {
		t.Fatal(err)
	}
	if got := st.For(ctx, 2, ""); len(got) != 0 {
		t.Fatalf("a user_token server is skipped for a signed-out caller, got %d tools", len(got))
	}
	got := st.For(ctx, 2, "user-token-1")
	if len(got) != 1 || got[0].Spec().Name != "kungal__search_topics" {
		t.Fatalf("only allowed tools, prefixed with the server name: %+v", got)
	}
	if !strings.Contains(string(got[0].Spec().Parameters), "query") {
		t.Fatalf("the server's input schema reaches the model: %s", got[0].Spec().Parameters)
	}
	run := tools.NewRegistry(got...).NewRun(4)
	out := run.Call(ctx, "kungal__search_topics", `{"query":"猫耳"}`)
	if out.Text != "2 topics about 猫耳" || len(out.Cards) != 1 {
		t.Fatalf("text and only the site's own linkable cards: %q cards=%d", out.Text, len(out.Cards))
	}
	var card map[string]any
	_ = json.Unmarshal(out.Cards[0].Data, &card)
	if out.Cards[0].ID != "kungal:topic-88" || card["url"] != "/topic/88" || card["cover"] != nil {
		t.Fatalf("card %s %v: an http cover is dropped", out.Cards[0].ID, card)
	}
	forwarded := false
	for _, h := range *seen {
		forwarded = forwarded || h == "Bearer user-token-1"
	}
	if !forwarded {
		t.Fatalf("the asking user's token is forwarded: %v", *seen)
	}
	if other := st.For(ctx, 3, "user-token-1"); len(other) != 0 {
		t.Fatal("a site sees only its own tool servers")
	}
}
