package handler

import (
	"reflect"
	"strings"

	"api/internal/platform/apiv2/problem"

	"github.com/danielgtaylor/huma/v2"
)

var skipWireShapes bool

// Huma published every pointer-to-struct field as a bare, required $ref, every
// slice as [array, null], and every error as its own ErrorModel. The wire is the
// other way round on all three: a message without media sends "media": null, no
// list is ever null, and errors are problem.Problem. kungal-apps generated its
// Dart client from this document and it could not parse a single plain message.
func publishWireShapes(doc *huma.OpenAPI) {
	if skipWireShapes {
		return
	}
	reg := doc.Components.Schemas
	problemRef := reg.Schema(reflect.TypeFor[problem.Problem](), true, "Problem")
	requests := requestSchemas(doc)
	for name, s := range reg.Map() {
		if t := reg.TypeFromRef("#/components/schemas/" + name); t != nil {
			nullablePointers(t, s)
		}
		if !requests[name] {
			arraysNeverNull(s)
		}
	}
	for _, item := range doc.Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Patch, item.Delete} {
			if op == nil {
				continue
			}
			for status, resp := range op.Responses {
				if status >= "400" && resp != nil {
					resp.Content = map[string]*huma.MediaType{"application/problem+json": {Schema: problemRef}}
				}
			}
		}
	}
	delete(reg.Map(), "ErrorModel")
	delete(reg.Map(), "ErrorDetail")
}

func nullablePointers(t reflect.Type, s *huma.Schema) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || s == nil {
		return
	}
	for f := range t.Fields() {
		name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			nullablePointers(f.Type, s)
			continue
		}
		p := s.Properties[name]
		if p == nil || f.Type.Kind() != reflect.Pointer || strings.Contains(","+opts+",", ",omitempty,") {
			continue
		}
		if p.Ref == "" {
			p.Nullable = true
			continue
		}
		s.Properties[name] = &huma.Schema{Description: p.Description, AnyOf: []*huma.Schema{{Ref: p.Ref}, {Type: "null"}}}
	}
}

func arraysNeverNull(s *huma.Schema) {
	if s == nil {
		return
	}
	if s.Items != nil {
		s.Nullable = false
		arraysNeverNull(s.Items)
	}
	for _, p := range s.Properties {
		arraysNeverNull(p)
	}
}

// A request may still send null for a list: the server has always accepted it,
// so only the schemas a response alone uses are narrowed.
func requestSchemas(doc *huma.OpenAPI) map[string]bool {
	seen := map[string]bool{}
	var walk func(*huma.Schema)
	walk = func(s *huma.Schema) {
		if s == nil {
			return
		}
		if name, ok := strings.CutPrefix(s.Ref, "#/components/schemas/"); ok {
			if seen[name] {
				return
			}
			seen[name] = true
			walk(doc.Components.Schemas.Map()[name])
			return
		}
		walk(s.Items)
		for _, p := range s.Properties {
			walk(p)
		}
		for _, x := range s.AnyOf {
			walk(x)
		}
	}
	for _, item := range doc.Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Patch, item.Delete} {
			if op != nil && op.RequestBody != nil {
				for _, mt := range op.RequestBody.Content {
					walk(mt.Schema)
				}
			}
		}
	}
	return seen
}
