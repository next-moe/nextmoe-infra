package mcpface

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// grownSpec is fixtureSpec plus one operation; retiredSpec drops one of the two
// fixtureSpec carries. Together they separate "the new op is offered" from "the
// retired op is gone", which one spec cannot.
var grownSpec = []byte(`{
  "paths": {
    "/v2/catalog/works": {
      "get": {
        "operationId": "listCatalogWorks",
        "summary": "List works",
        "parameters": [
          {"name": "q", "in": "query", "schema": {"type": "string"}},
          {"name": "view", "in": "query", "schema": {"type": "string"}},
          {"name": "fields", "in": "query", "schema": {"type": "string"}},
          {"name": "released_after", "in": "query", "schema": {"type": "string"}}
        ]
      }
    },
    "/v2/problems": {
      "get": {"operationId": "listProblemTypes", "summary": "Problem types"}
    },
    "/v2/catalog/calendar": {
      "get": {"operationId": "listCatalogCalendar", "summary": "Release calendar"}
    }
  }
}`)

var retiredSpec = []byte(`{
  "paths": {
    "/v2/problems": {
      "get": {"operationId": "listProblemTypes", "summary": "Problem types"}
    }
  }
}`)

func liveToolNames(t *testing.T, server *mcp.Server) []string {
	t.Helper()
	ctx := context.Background()
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
	names := make([]string, 0, len(res.Tools))
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func toolParams(t *testing.T, server *mcp.Server, name string) []string {
	t.Helper()
	ctx := context.Background()
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
	for _, tool := range res.Tools {
		if tool.Name != name {
			continue
		}
		raw, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("tool %q schema: %v", name, err)
		}
		var schema struct {
			Properties map[string]any `json:"properties"`
		}
		if err := json.Unmarshal(raw, &schema); err != nil {
			t.Fatalf("tool %q schema: %v", name, err)
		}
		out := make([]string, 0, len(schema.Properties))
		for k := range schema.Properties {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	t.Fatalf("tool %q is not offered", name)
	return nil
}

func TestSpecSyncOffersOperationsAddedAfterStartup(t *testing.T) {
	server, sync, err := NewServer(NewUpstream("http://127.0.0.1:0"), fixtureSpec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if got := liveToolNames(t, server); len(got) != 2 {
		t.Fatalf("startup tools = %v, want 2", got)
	}

	changed, err := sync.Apply(grownSpec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if !changed {
		t.Fatal("Apply reported no change for a document that grew an operation")
	}

	got := liveToolNames(t, server)
	want := []string{"listCatalogCalendar", "listCatalogWorks", "listProblemTypes"}
	if len(got) != len(want) {
		t.Fatalf("tools = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("tools = %v, want %v", got, want)
		}
	}

	// A parameter added to an operation that already existed is the other half:
	// the tool name is unchanged, so a name-only assertion would pass on a stale
	// schema.
	params := toolParams(t, server, "listCatalogWorks")
	if len(params) != 4 {
		t.Fatalf("listCatalogWorks params = %v, want 4 including released_after", params)
	}
}

func TestSpecSyncRemovesRetiredOperations(t *testing.T) {
	server, sync, err := NewServer(NewUpstream("http://127.0.0.1:0"), fixtureSpec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	if _, err := sync.Apply(retiredSpec); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	got := liveToolNames(t, server)
	if len(got) != 1 || got[0] != "listProblemTypes" {
		t.Fatalf("tools = %v, want only listProblemTypes", got)
	}
}

func TestSpecSyncKeepsTheToolSetWhenTheDocumentDoesNotParse(t *testing.T) {
	server, sync, err := NewServer(NewUpstream("http://127.0.0.1:0"), fixtureSpec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	before := liveToolNames(t, server)

	if _, err := sync.Apply([]byte("{not json")); err == nil {
		t.Fatal("Apply accepted a document that is not JSON")
	}
	after := liveToolNames(t, server)
	if len(after) != len(before) {
		t.Fatalf("tools = %v after a bad document, want the previous %v", after, before)
	}
}

func TestSpecSyncIsQuietWhenTheDocumentHasNotMoved(t *testing.T) {
	_, sync, err := NewServer(NewUpstream("http://127.0.0.1:0"), fixtureSpec)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	changed, err := sync.Apply(fixtureSpec)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if changed {
		t.Fatal("Apply reported a change for the same document; every poll would notify listChanged")
	}
}
