package mcpface

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"
	"strings"
)

type specDoc struct {
	Paths map[string]specPathItem `json:"paths"`
}

type specPathItem struct {
	Get        *specOp     `json:"get"`
	Parameters []specParam `json:"parameters"`
}

type specOp struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Parameters  []specParam           `json:"parameters"`
	Security    []map[string][]string `json:"security"`
}

type specParam struct {
	Name        string     `json:"name"`
	In          string     `json:"in"`
	Required    bool       `json:"required"`
	Description string     `json:"description"`
	Schema      specSchema `json:"schema"`
}

type specSchema struct {
	Description string   `json:"description"`
	Enum        []string `json:"enum"`
}

// Every v2 query and path parameter is declared `type: string` and parsed
// server-side, so a string-only input schema loses no type information. What
// the tools did lose was each parameter's description: they described a
// parameter by its own name.
type ParamDoc struct {
	Description string
	Enum        []string
}

type ToolDesc struct {
	Name        string
	Method      string
	Path        string
	Summary     string
	Description string
	Params      []string
	ParamDocs   map[string]ParamDoc
	Required    []string
	NeedsKey    bool
}

const appKeyScheme = "applicationKey"

func mcpToolPrefixes(path string) bool {
	return strings.HasPrefix(path, "/v2/catalog") ||
		strings.HasPrefix(path, "/v2/news") ||
		strings.HasPrefix(path, "/v2/problems") ||
		strings.HasPrefix(path, "/v2/vocabularies")
}

func takesAppKey(security []map[string][]string) bool {
	return slices.ContainsFunc(security, func(req map[string][]string) bool {
		_, ok := req[appKeyScheme]
		return ok
	})
}

func ToolsFromSpec(raw []byte) ([]ToolDesc, error) {
	var doc specDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	out := make([]ToolDesc, 0, len(doc.Paths))
	for path, item := range doc.Paths {
		if item.Get == nil || !mcpToolPrefixes(path) {
			continue
		}
		op := item.Get
		name := op.OperationID
		if name == "" {
			continue
		}
		desc := strings.TrimSpace(op.Description)
		if desc == "" {
			desc = strings.TrimSpace(op.Summary)
		}
		params := append([]specParam{}, item.Parameters...)
		params = append(params, op.Parameters...)
		td := ToolDesc{
			Name: name, Method: http.MethodGet, Path: path,
			Summary: strings.TrimSpace(op.Summary), Description: desc,
			ParamDocs: map[string]ParamDoc{},
			NeedsKey:  takesAppKey(op.Security),
		}
		seen := map[string]bool{}
		for _, p := range params {
			if p.Name == "" || p.In == "header" || seen[p.Name] {
				continue
			}
			seen[p.Name] = true
			td.Params = append(td.Params, p.Name)
			pd := ParamDoc{Description: strings.TrimSpace(p.Description), Enum: p.Schema.Enum}
			if pd.Description == "" {
				pd.Description = strings.TrimSpace(p.Schema.Description)
			}
			td.ParamDocs[p.Name] = pd
			if p.Required || p.In == "path" {
				td.Required = append(td.Required, p.Name)
			}
		}
		out = append(out, td)
	}
	// doc.Paths is a map, so the order this loop produced is random per process.
	// cmd/gen-v2-portal writes a committed file from this slice and CI diffs it,
	// which an unsorted slice fails on roughly every second run.
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
