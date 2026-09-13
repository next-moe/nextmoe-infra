package mcpface

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true}

func applySpecTools(s *mcp.Server, up *Upstream, raw []byte) ([]string, error) {
	tools, err := ToolsFromSpec(raw)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tools))
	t := &toolsRunner{up: up}
	for i := range tools {
		td := tools[i]
		schema := map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}
		props := schema["properties"].(map[string]any)
		for _, name := range td.Params {
			props[name] = map[string]any{"type": "string", "description": name}
		}
		if len(td.Required) > 0 {
			schema["required"] = td.Required
		}
		mcp.AddTool(s, &mcp.Tool{
			Name:        td.Name,
			Title:       td.Name,
			Description: td.Description,
			InputSchema: schema,
			Annotations: readOnly,
		}, t.handler(td))
		names = append(names, td.Name)
	}
	return names, nil
}

type toolsRunner struct {
	up *Upstream
}

func (t *toolsRunner) handler(td ToolDesc) func(context.Context, *mcp.CallToolRequest, map[string]any) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, in map[string]any) (*mcp.CallToolResult, any, error) {
		path := td.Path
		q := url.Values{}
		for k, v := range in {
			if v == nil {
				continue
			}
			s := fmt.Sprint(v)
			if s == "" {
				continue
			}
			placeholder := "{" + k + "}"
			if strings.Contains(path, placeholder) {
				path = strings.ReplaceAll(path, placeholder, url.PathEscape(s))
				continue
			}
			q.Set(k, s)
		}
		if strings.Contains(path, "{") {
			return errorResult("missing path parameter in " + td.Path), nil, nil
		}

		var header http.Header
		if req != nil && req.Extra != nil {
			header = req.Extra.Header
		}
		token, ok := bearerToken(header)
		if td.NeedsKey && !ok {
			return authError(), nil, nil
		}
		auth := ""
		if ok {
			auth = "Bearer " + token
		}
		return t.run(ctx, req, td.Name, path, q, auth)
	}
}

func (t *toolsRunner) run(ctx context.Context, req *mcp.CallToolRequest, tool, path string, query url.Values, authorization string) (*mcp.CallToolResult, any, error) {
	status, body, err := t.up.Get(ctx, path, query, authorization)
	if err != nil {
		return errorResult("Upstream request failed: " + err.Error() + ". Try again later."), nil, nil
	}
	_ = req
	_ = tool
	return mapUpstream(status, body), nil, nil
}
