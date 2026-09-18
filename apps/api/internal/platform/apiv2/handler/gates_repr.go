package handler

import (
	"fmt"
	"reflect"
	"strings"

	"api/internal/platform/apiv2/repr"

	"github.com/danielgtaylor/huma/v2"
)

var g8Exceptions = map[string]bool{
	"object":     true,
	"state":      true,
	"value":      true,
	"source":     true, // news_item.source is a news_source object; Image/Ref/Intro.source is the open sources key
	"companies":  true, // catalog_stats.companies is a count; work.companies is the include block
	"characters": true, // catalog_stats.characters is a count; work.characters is the include block
	// news_submission.status is the item's lifecycle stage, the same concept 07 §2.1
	// admits for `state`; the news write face is contractually spelled `status`
	// (06 §7.2 / 03 §2.4). Problem.status and playtime_batch_item.status are HTTP
	// status codes and stay integers.
	"status": true,
}

func CheckG7toG16(doc *huma.OpenAPI) []string {
	var errs []string
	errs = append(errs, checkG7(doc)...)
	errs = append(errs, checkG8(doc)...)
	errs = append(errs, checkG9(doc)...)
	errs = append(errs, checkG14(doc)...)
	errs = append(errs, checkG16(doc)...)
	return errs
}

func checkG7(doc *huma.OpenAPI) []string {
	var errs []string
	seen := map[*huma.Schema]bool{}
	visit := func(s *huma.Schema) {
		walkSchema(s, seen, func(schema *huma.Schema) {
			if schema == nil || schema.Properties == nil {
				return
			}
			for name, p := range schema.Properties {
				if p == nil {
					continue
				}
				if name != "id" && !strings.HasSuffix(name, "_id") {
					continue
				}
				r := deref(doc, p)
				if r == nil {
					continue
				}
				if r.Type != "string" {
					errs = append(errs, "G7: "+name+" must be a JSON string, got "+r.Type)
				}
			}
		})
	}
	visitComponents(doc, visit)
	return errs
}

func checkG8(doc *huma.OpenAPI) []string {
	type occ struct {
		where string
		s     *huma.Schema
	}
	byName := map[string][]occ{}
	seen := map[*huma.Schema]bool{}
	var errs []string
	visit := func(where string, s *huma.Schema) {
		walkSchema(s, seen, func(schema *huma.Schema) {
			if schema == nil || schema.Properties == nil {
				return
			}
			for name, p := range schema.Properties {
				if name == "kind" {
					errs = append(errs, "G8: property kind is forbidden")
				}
				byName[name] = append(byName[name], occ{where + "." + name, deref(doc, p)})
			}
		})
	}
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, schema := range doc.Components.Schemas.Map() {
			visit("schema "+name, schema)
		}
	}
	for name, occs := range byName {
		if g8Exceptions[name] || len(occs) < 2 {
			continue
		}
		first := occs[0].s
		for _, o := range occs[1:] {
			if !schemaSame(first, o.s) {
				errs = append(errs, "G8: property "+name+" has diverging schemas ("+occs[0].where+" vs "+o.where+")")
				break
			}
		}
	}
	return errs
}

func schemaSame(a, b *huma.Schema) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Type == b.Type
}

func checkG9(doc *huma.OpenAPI) []string {
	var errs []string
	seen := map[*huma.Schema]bool{}
	walkSchemaAll(doc, seen, func(s *huma.Schema) {
		if s == nil {
			return
		}
		if (s.Type == "array" || s.Items != nil) && s.Nullable {
			errs = append(errs, "G9: array schema is nullable")
		}
		if s.Properties != nil {
			ap, ok := s.AdditionalProperties.(bool)
			if !ok || !ap {
				errs = append(errs, "G9: object schema must have additionalProperties: true")
			}
		}
	})
	return unique(errs)
}

func checkG14(doc *huma.OpenAPI) []string {
	var errs []string
	seen := map[*huma.Schema]bool{}
	walkSchemaAll(doc, seen, func(s *huma.Schema) {
		if s == nil || s.Properties == nil {
			return
		}
		for name, p := range s.Properties {
			if name == "$schema" || p == nil {
				continue
			}
			r := deref(doc, p)
			if r == nil {
				continue
			}
			if r.Type == "string" || (r.Type == "" && r.Enum != nil) {
				if !stringClassed(r) {
					errs = append(errs, "G14: string property "+name+" is not in C1–C5")
				}
				if r.MaxLength == nil && len(r.Enum) == 0 {
					errs = append(errs, "G14: string property "+name+" has no maxLength")
				}
			}
			if r.Type == "integer" || r.Type == "number" {
				if r.Minimum == nil {
					errs = append(errs, "G14: numeric property "+name+" has no minimum")
				}
			}
		}
	})
	return unique(errs)
}

func stringClassed(s *huma.Schema) bool {
	if len(s.Enum) > 0 {
		return true
	}
	if s.Format != "" || s.Pattern != "" {
		return true
	}
	if s.Extensions != nil {
		if v, ok := s.Extensions["x-vocabulary"]; ok && fmt.Sprint(v) != "" {
			return true
		}
	}
	if s.MaxLength != nil && strings.Contains(strings.ToLower(s.Description), "must not be used as a discriminant") {
		return true
	}
	return false
}

func checkG16(doc *huma.OpenAPI) []string {
	var errs []string
	seen := map[string]string{}
	for _, p := range repr.Prefixes {
		if prev, ok := seen[p.Prefix]; ok {
			errs = append(errs, "G16: prefix "+p.Prefix+" issued by both "+prev+" and "+p.Use)
		}
		seen[p.Prefix] = p.Use
	}
	foundReq, foundCur := false, false
	walkSchemaAll(doc, map[*huma.Schema]bool{}, func(s *huma.Schema) {
		if s == nil || s.Properties == nil {
			return
		}
		if p := s.Properties["request_id"]; p != nil {
			r := deref(doc, p)
			if r != nil && strings.Contains(r.Pattern, "req_") {
				foundReq = true
			}
		}
		if p := s.Properties["next_cursor"]; p != nil {
			r := deref(doc, p)
			if r != nil && strings.Contains(r.Pattern, "cur_") {
				foundCur = true
			}
		}
	})
	if !foundReq {
		errs = append(errs, "G16: request_id is not constrained to the req_ prefix")
	}
	_ = foundCur
	_ = doc
	return errs
}

func visitComponents(doc *huma.OpenAPI, fn func(*huma.Schema)) {
	if doc.Components != nil && doc.Components.Schemas != nil {
		for _, schema := range doc.Components.Schemas.Map() {
			fn(schema)
		}
	}
}

func walkSchemaAll(doc *huma.OpenAPI, seen map[*huma.Schema]bool, fn func(*huma.Schema)) {
	visit := func(s *huma.Schema) { walkSchema(s, seen, fn) }
	visitComponents(doc, visit)
	for _, item := range doc.Paths {
		for _, op := range pathOps(item) {
			for _, p := range op.Parameters {
				if p != nil {
					visit(p.Schema)
				}
			}
			for _, resp := range op.Responses {
				if resp == nil {
					continue
				}
				for _, c := range resp.Content {
					if c != nil {
						visit(deref(doc, c.Schema))
					}
				}
			}
		}
	}
}

func walkSchemaAllWithRequests(doc *huma.OpenAPI, seen map[*huma.Schema]bool, fn func(*huma.Schema)) {
	walkSchemaAll(doc, seen, fn)
	for _, item := range doc.Paths {
		for _, op := range pathOps(item) {
			if op.RequestBody == nil {
				continue
			}
			for _, c := range op.RequestBody.Content {
				if c != nil {
					walkSchema(c.Schema, seen, fn)
				}
			}
		}
	}
}

func CheckG18(doc *huma.OpenAPI) []string {
	var errs []string
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, schema := range doc.Components.Schemas.Map() {
			t := doc.Components.Schemas.TypeFromRef("#/components/schemas/" + name)
			if t == nil || schema == nil {
				continue
			}
			errs = append(errs, checkG18Pointers(name, t, schema)...)
		}
	}
	var enumErrs []string
	seen := map[*huma.Schema]bool{}
	var visit func(string, *huma.Schema)
	visit = func(where string, s *huma.Schema) {
		if s == nil || seen[s] {
			return
		}
		seen[s] = true
		if len(s.Enum) > 0 && schemaAdmitsNull(s) && !enumHasNull(s.Enum) {
			enumErrs = append(enumErrs, "G18: "+where+" admits null but enum does not list null")
		}
		for name, p := range s.Properties {
			visit(where+"."+name, p)
		}
		visit(where+"[]", s.Items)
		for i, x := range s.AnyOf {
			visit(fmt.Sprintf("%s.anyOf[%d]", where, i), x)
		}
		for i, x := range s.OneOf {
			visit(fmt.Sprintf("%s.oneOf[%d]", where, i), x)
		}
		for i, x := range s.AllOf {
			visit(fmt.Sprintf("%s.allOf[%d]", where, i), x)
		}
		visit(where+".not", s.Not)
	}
	if doc.Components != nil && doc.Components.Schemas != nil {
		for name, schema := range doc.Components.Schemas.Map() {
			visit("schema "+name, schema)
		}
	}
	for path, item := range doc.Paths {
		for _, op := range pathOps(item) {
			for _, p := range op.Parameters {
				if p != nil {
					visit(fmt.Sprintf("%s %s param %s", opMethod(op), path, p.Name), p.Schema)
				}
			}
			if op.RequestBody != nil {
				for mt, c := range op.RequestBody.Content {
					if c != nil {
						visit(fmt.Sprintf("%s %s request %s", opMethod(op), path, mt), c.Schema)
					}
				}
			}
			for status, resp := range op.Responses {
				if resp == nil {
					continue
				}
				for mt, c := range resp.Content {
					if c != nil {
						visit(fmt.Sprintf("%s %s %s %s", opMethod(op), path, status, mt), c.Schema)
					}
				}
			}
		}
	}
	errs = append(errs, enumErrs...)
	return unique(errs)
}

func checkG18Pointers(schemaName string, t reflect.Type, s *huma.Schema) []string {
	var errs []string
	var walk func(reflect.Type, *huma.Schema)
	walk = func(t reflect.Type, s *huma.Schema) {
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct || s == nil {
			return
		}
		for i := range t.NumField() {
			f := t.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.Anonymous && name == "" {
				walk(f.Type, s)
				continue
			}
			p := s.Properties[name]
			if p == nil || f.Type.Kind() != reflect.Pointer || omitsNil(opts) || pointerElemIsCollection(f.Type) {
				continue
			}
			if !schemaAdmitsNull(p) {
				errs = append(errs, "G18: "+schemaName+"."+name+" is a non-omitted pointer but null does not validate")
			}
		}
	}
	walk(t, s)
	return errs
}

func enumHasNull(enum []any) bool {
	for _, v := range enum {
		if v == nil {
			return true
		}
	}
	return false
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
