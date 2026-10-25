package handler

import (
	"context"

	"api/internal/platform/apiv2/collect"
	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/repr"
	"api/internal/platform/catalog/editspec"
	"api/internal/platform/editing"
)

func (c *Catalog) GetSchema(_ context.Context, object string) (repr.ObjectSchema, error) {
	entityType := schemaEntityType(object)
	if entityType == "" {
		return repr.ObjectSchema{}, problem.New(problem.CodeNotFound, "", "", "No schema for family "+object+".")
	}
	if c == nil || c.EditTypes == nil {
		return repr.ObjectSchema{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog schemas are not bound.")
	}
	spec, ok := c.EditTypes.Type(entityType)
	if !ok {
		return repr.ObjectSchema{}, problem.New(problem.CodeServiceUnavailable, "", "", "catalog schemas are not bound.")
	}
	read, _ := collect.ObjectSpec(object)
	list, _ := collect.ObjectListSpec(object)
	fields := make([]repr.SchemaField, 0, len(spec.Fields))
	for i := range spec.Fields {
		f := spec.Fields[i]
		maxEl := 0
		if f.Kind == editing.KindList {
			maxEl = f.MaxElements
			if maxEl <= 0 {
				maxEl = editing.DefaultMaxElements
			}
		}
		sf := repr.SchemaField{
			Key:           f.Key,
			FieldType:     string(f.Kind),
			DiffHint:      f.DiffHint,
			Deprecated:    f.Deprecated,
			MaxSuppressed: f.MaxSuppressed,
			MaxElements:   maxEl,
		}
		if v := f.Value; v != nil {
			sf.Vocabulary = v.Vocabulary
			if v.Vocabulary != "" {
				encoding := "token"
				if v.Coded {
					encoding = "int"
				}
				sf.Encoding = &encoding
			}
			sf.Base = v.Base
			sf.Nullable = v.Nullable
			if v.Element != nil {
				el := repr.SchemaElement{
					Type:    v.Element.Type,
					Members: make([]repr.SchemaElementMember, 0, len(v.Element.Members)),
				}
				for _, m := range v.Element.Members {
					el.Members = append(el.Members, repr.SchemaElementMember{
						Key: m.Key, Type: m.Type, Vocabulary: m.Vocabulary,
						Base: m.Base, Nullable: m.Nullable,
					})
				}
				sf.Element = &el
			}
		}
		fields = append(fields, sf)
	}
	return repr.ObjectSchema{
		Object:           "object_schema",
		TargetObject:     object,
		EntityType:       entityType,
		Include:          copyStrings(read.Include),
		FullSet:          copyStrings(read.FullSet),
		ListInclude:      copyStrings(list.Include),
		ListFullSet:      copyStrings(list.FullSet),
		CreationDisabled: object == "release",
		Fields:           fields,
	}, nil
}

func schemaEntityType(object string) string {
	switch object {
	case "work":
		return editspec.TypeWork
	case "company":
		return editspec.TypeLabel
	case "character":
		return editspec.TypeCharacter
	case "release":
		return editspec.TypeRelease
	case "tag":
		return editspec.TypeTag
	case "engine":
		return editspec.TypeEngine
	case "series":
		return editspec.TypeSeries
	case "person":
		return editspec.TypePerson
	case "credit_name":
		return editspec.TypeCreditName
	case "trait":
		return editspec.TypeTrait
	case "walkthrough":
		return editspec.TypeWalkthrough
	default:
		return ""
	}
}

func copyStrings(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
