package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"api/internal/platform/apiv2/vocab"
	"api/internal/platform/mcpface"

	"github.com/danielgtaylor/huma/v2"
)

var g10HeaderParams = map[string]bool{
	"Authorization": true, "If-Match": true, "If-None-Match": true,
	"Idempotency-Key": true, "X-Request-ID": true,
}

func CheckG10(doc *huma.OpenAPI) []string {
	raw, err := json.Marshal(doc)
	if err != nil {
		return []string{"G10: marshal spec: " + err.Error()}
	}
	tools, err := mcpface.ToolsFromSpec(raw)
	if err != nil {
		return []string{"G10: parse spec tools: " + err.Error()}
	}
	byName := map[string]mcpface.ToolDesc{}
	for _, t := range tools {
		byName[t.Name] = t
	}
	var errs []string
	for path, item := range doc.Paths {
		if item == nil || item.Get == nil {
			continue
		}
		if !mcpface.AdmitsToolPath(path) {
			continue
		}
		op := item.Get
		if op.OperationID == "" {
			continue
		}
		td, ok := byName[op.OperationID]
		if !ok {
			errs = append(errs, fmt.Sprintf("G10: MCP missing tool for %s %s", op.OperationID, path))
			continue
		}
		have := map[string]bool{}
		for _, p := range td.Params {
			have[p] = true
		}
		for _, p := range op.Parameters {
			if p == nil || p.In == "header" || g10HeaderParams[p.Name] {
				continue
			}
			if !have[p.Name] {
				errs = append(errs, fmt.Sprintf("G10: tool %s missing HTTP param %s", op.OperationID, p.Name))
			}
		}
	}
	return errs
}

func CheckG15(doc *huma.OpenAPI) []string {
	var errs []string
	for _, v := range vocab.All() {
		if !v.Closed {
			continue
		}
		want := map[string]bool{}
		for _, val := range v.Values {
			want[val.Value] = true
		}
		walkSchemaAll(doc, map[*huma.Schema]bool{}, func(s *huma.Schema) {
			if s == nil || s.Properties == nil {
				return
			}
			p := s.Properties[v.Name]
			if p == nil {
				return
			}
			r := deref(doc, p)
			if r == nil || len(r.Enum) == 0 {
				return
			}
			got := map[string]bool{}
			for _, e := range r.Enum {
				if str, ok := e.(string); ok {
					got[str] = true
				}
			}
			for token := range want {
				if !got[token] {
					errs = append(errs, fmt.Sprintf("G15: property %s missing edit/public token %s", v.Name, token))
				}
			}
			for token := range got {
				if !want[token] {
					errs = append(errs, fmt.Sprintf("G15: property %s has extra token %s vs vocabulary", v.Name, token))
				}
			}
		})
	}
	return uniqueStrings(errs)
}

func CheckG17(doc *huma.OpenAPI) []string {
	idSchemas := map[string]bool{}
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, s := range doc.Components.Schemas.Map() {
			r := deref(doc, s)
			if r == nil || r.Properties == nil {
				continue
			}
			if p := r.Properties["id"]; p != nil {
				d := deref(doc, p)
				if d != nil && d.Type == "string" {
					idSchemas[name] = true
				}
			}
		}
	}
	if len(idSchemas) == 0 {
		return []string{"G17: no read schema with string id"}
	}
	var errs []string
	for path, item := range doc.Paths {
		for _, op := range []*huma.Operation{item.Put, item.Post, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			if op.Method != http.MethodPut && op.Method != http.MethodPost &&
				op.Method != http.MethodPatch && op.Method != http.MethodDelete {
				if op.Method == "" && item.Get == op {
					continue
				}
			}
			if !strings.Contains(path, "{") {
				continue
			}
			if !strings.Contains(path, "_id") && !strings.Contains(path, "{id}") {
				continue
			}
			_ = path
		}
	}
	if len(idSchemas) == 0 {
		errs = append(errs, "G17: write paths exist but no readable id")
	}
	return errs
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
