package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/llm"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type MCPServer struct {
	Name       string
	URL        string
	CardPrefix string
	Token      string
	Timeout    time.Duration
	HTTP       *http.Client
}

type bearer struct {
	base  http.RoundTripper
	token string
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	if b.token != "" {
		r = r.Clone(r.Context())
		r.Header.Set("Authorization", "Bearer "+b.token)
	}
	return b.base.RoundTrip(r)
}

func (s MCPServer) connect(ctx context.Context) (*mcp.ClientSession, error) {
	base := http.DefaultTransport
	if s.HTTP != nil && s.HTTP.Transport != nil {
		base = s.HTTP.Transport
	}
	hc := &http.Client{Transport: bearer{base: base, token: s.Token}, Timeout: s.Timeout}
	client := mcp.NewClient(&mcp.Implementation{Name: "nextmoe-agent", Version: "1"}, nil)
	return client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: s.URL, HTTPClient: hc, MaxRetries: -1, DisableStandaloneSSE: true}, nil)
}

func (s MCPServer) ListTools(ctx context.Context) ([]*mcp.Tool, error) {
	session, err := s.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer session.Close()
	var out []*mcp.Tool
	params := &mcp.ListToolsParams{}
	for {
		res, err := session.ListTools(ctx, params)
		if err != nil {
			return nil, err
		}
		out = append(out, res.Tools...)
		if res.NextCursor == "" {
			return out, nil
		}
		params.Cursor = res.NextCursor
	}
}

var toolNameUnsafe = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func SiteToolName(server, tool string) string {
	name := toolNameUnsafe.ReplaceAllString(server, "_") + "__" + toolNameUnsafe.ReplaceAllString(tool, "_")
	if len(name) > 64 {
		name = name[:64]
	}
	return name
}

type mcpTool struct {
	server MCPServer
	tool   *mcp.Tool
}

func NewMCPTool(server MCPServer, tool *mcp.Tool) Tool { return &mcpTool{server: server, tool: tool} }

func (t *mcpTool) Spec() llm.Tool {
	params, _ := json.Marshal(t.tool.InputSchema)
	if len(params) == 0 || string(params) == "null" {
		params = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return llm.Tool{Name: SiteToolName(t.server.Name, t.tool.Name), Description: t.tool.Description, Parameters: params}
}

func (t *mcpTool) Timeout() time.Duration { return t.server.Timeout }

func (t *mcpTool) Run(ctx context.Context, args json.RawMessage) (Output, error) {
	var arguments map[string]any
	if err := json.Unmarshal(args, &arguments); err != nil {
		return Output{Text: `{"error":"arguments must be a JSON object"}`}, nil
	}
	session, err := t.server.connect(ctx)
	if err != nil {
		return Output{}, fmt.Errorf("%s is unreachable: %w", t.server.Name, err)
	}
	defer session.Close()
	res, err := session.CallTool(ctx, &mcp.CallToolParams{Name: t.tool.Name, Arguments: arguments})
	if err != nil {
		return Output{}, err
	}
	var text strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if text.Len() > 0 {
				text.WriteString("\n")
			}
			text.WriteString(tc.Text)
		}
	}
	if res.IsError {
		raw, _ := json.Marshal(map[string]string{"error": text.String()})
		return Output{Text: string(raw)}, nil
	}
	out := Output{Text: text.String(), Cards: SiteCards(t.server.CardPrefix, res.StructuredContent)}
	if out.Text == "" && res.StructuredContent != nil {
		raw, _ := json.Marshal(res.StructuredContent)
		out.Text = string(raw)
	}
	return out, nil
}

type siteCard struct {
	Kind         string `json:"kind"`
	ID           string `json:"id"`
	Title        string `json:"title"`
	Subtitle     string `json:"subtitle,omitempty"`
	Cover        string `json:"cover,omitempty"`
	Summary      string `json:"summary,omitempty"`
	URL          string `json:"url"`
	ContentLimit string `json:"content_limit,omitempty"`
}

var siteKindSuffix = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}$`)

// SiteCards keeps only well-formed cards of the site's own kinds, so a site
// tool can neither pass itself off as a shared NextMoe entity nor link out of
// the site it serves.
func SiteCards(prefix string, structured any) []Card {
	raw, err := json.Marshal(structured)
	if err != nil {
		return nil
	}
	var body struct {
		Cards []siteCard `json:"cards"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil
	}
	var out []Card
	for _, c := range body.Cards {
		suffix, ok := strings.CutPrefix(c.Kind, prefix+":")
		if !ok || prefix == "" || !siteKindSuffix.MatchString(suffix) {
			continue
		}
		c.ID, c.Title = strings.TrimSpace(c.ID), strings.TrimSpace(c.Title)
		if c.ID == "" || len(c.ID) > 64 || c.Title == "" || !strings.HasPrefix(c.URL, "/") || strings.HasPrefix(c.URL, "//") {
			continue
		}
		if c.Cover != "" && !strings.HasPrefix(c.Cover, "https://") {
			c.Cover = ""
		}
		c.Title, c.Subtitle, c.Summary = clipRunes(c.Title, 120), clipRunes(c.Subtitle, 120), clipRunes(c.Summary, 300)
		data, _ := json.Marshal(c)
		out = append(out, Card{ID: c.Kind + "-" + c.ID, Data: data})
		if len(out) == 12 {
			break
		}
	}
	return out
}
