package handler

import (
	"reflect"
	"slices"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

var skipNullablePasses bool

func markNullablePointers(doc *huma.OpenAPI) {
	if doc == nil || doc.Components == nil || doc.Components.Schemas == nil {
		return
	}
	for name, schema := range doc.Components.Schemas.Map() {
		t := doc.Components.Schemas.TypeFromRef("#/components/schemas/" + name)
		if t == nil || schema == nil {
			continue
		}
		nullablePointers(t, schema)
	}
}

// A downstream standard validator rejected NewsItem.banner: null and 23
// nullable enums whose enum lists omitted null. Huma's validator short-circuits
// null before the enum check, so no test here had seen it.
func nullablePointers(t reflect.Type, s *huma.Schema) {
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
			nullablePointers(f.Type, s)
			continue
		}
		p := s.Properties[name]
		if p == nil || f.Type.Kind() != reflect.Pointer || omitsNil(opts) || pointerElemIsCollection(f.Type) {
			continue
		}
		if p.Ref == "" {
			p.Nullable = true
			continue
		}
		s.Properties[name] = &huma.Schema{
			Description: p.Description,
			AnyOf:       []*huma.Schema{{Ref: p.Ref}, {Type: "null"}},
		}
	}
}

func markNullableEnums(doc *huma.OpenAPI) {
	walkSchemaAllWithRequests(doc, map[*huma.Schema]bool{}, func(s *huma.Schema) {
		nullableEnums(s)
	})
}

func nullableEnums(s *huma.Schema) {
	if s == nil || !schemaAdmitsNull(s) || len(s.Enum) == 0 || slices.Contains(s.Enum, nil) {
		return
	}
	s.Enum = append(s.Enum, nil)
	if s.Extensions == nil {
		s.Extensions = map[string]any{}
	}
	if _, ok := s.Extensions["x-vocabulary-closed"]; !ok {
		s.Extensions["x-vocabulary-closed"] = true
	}
}

func schemaAdmitsNull(s *huma.Schema) bool {
	if s == nil {
		return false
	}
	if s.Nullable || s.Type == "null" {
		return true
	}
	return nullUnionOther(s) != nil
}

func nullUnionOther(s *huma.Schema) *huma.Schema {
	if s == nil || len(s.AnyOf) != 2 {
		return nil
	}
	var other *huma.Schema
	sawNull := false
	for _, m := range s.AnyOf {
		if m == nil {
			return nil
		}
		if isBareNullSchema(m) {
			if sawNull {
				return nil
			}
			sawNull = true
			continue
		}
		if other != nil {
			return nil
		}
		other = m
	}
	if !sawNull {
		return nil
	}
	return other
}

func isBareNullSchema(s *huma.Schema) bool {
	return s != nil && s.Type == "null" && s.Ref == "" && !s.Nullable &&
		s.Properties == nil && s.Items == nil &&
		len(s.AnyOf) == 0 && len(s.OneOf) == 0 && len(s.AllOf) == 0
}

func omitsNil(opts string) bool {
	for _, opt := range strings.Split(opts, ",") {
		if opt == "omitempty" || opt == "omitzero" {
			return true
		}
	}
	return false
}

func pointerElemIsCollection(t reflect.Type) bool {
	if t.Kind() != reflect.Pointer {
		return false
	}
	switch t.Elem().Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return true
	default:
		return false
	}
}
