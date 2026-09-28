package wireshape

import (
	"reflect"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

var Skip bool

// Huma published every pointer-to-struct field as a bare, required $ref and
// every slice as [array, null]. The wire is the other way round on both: a nil
// pointer without omitempty is sent as null, and no response list is ever null.
// kungal-apps generated its Dart client from the chat document and it could not
// parse a single plain message. A nullable enum also left null out of its list,
// so community's follow states, which send viewer_notify: null for a user the
// viewer does not follow, failed their own document.
func Publish(doc *huma.OpenAPI) {
	if Skip {
		return
	}
	reg := doc.Components.Schemas
	requests := requestSchemas(doc)
	for name, s := range reg.Map() {
		if t := reg.TypeFromRef("#/components/schemas/" + name); t != nil {
			nullablePointers(t, s)
		}
		if !requests[name] {
			arraysNeverNull(s)
		}
		nullInEnums(s)
	}
}

func Operations(item *huma.PathItem) []*huma.Operation {
	var ops []*huma.Operation
	for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Patch, item.Delete} {
		if op != nil {
			ops = append(ops, op)
		}
	}
	return ops
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

func nullInEnums(s *huma.Schema) {
	if s == nil {
		return
	}
	if s.Nullable && len(s.Enum) > 0 && !slices.Contains(s.Enum, nil) {
		s.Enum = append(s.Enum, nil)
	}
	nullInEnums(s.Items)
	for _, p := range s.Properties {
		nullInEnums(p)
	}
	for _, x := range s.AnyOf {
		nullInEnums(x)
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
		for _, op := range Operations(item) {
			if op.RequestBody != nil {
				for _, mt := range op.RequestBody.Content {
					walk(mt.Schema)
				}
			}
		}
	}
	return seen
}
