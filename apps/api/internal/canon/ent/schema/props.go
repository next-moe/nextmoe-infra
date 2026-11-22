package schema

import (
	"api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

func with(f ent.Field, p registry.Property) ent.Field {
	f.Descriptor().Annotations = append(f.Descriptor().Annotations, p)
	return f
}

// uniqueIndexes derives the unique indexes from the registry, so a key is
// declared once (plan 06 §3) and cannot be unique in the contract but not here.
func uniqueIndexes(fields []ent.Field) []ent.Index {
	var out []ent.Index
	for _, f := range fields {
		for _, a := range f.Descriptor().Annotations {
			if p, ok := a.(registry.Property); ok && p.Unique == registry.UniqueGlobal {
				out = append(out, index.Fields(f.Descriptor().Name).Unique())
			}
		}
	}
	return out
}

// noAction overrides ent's ON DELETE SET NULL for optional edges: canon never
// hard-deletes an entity, it merges with a redirect, so a delete reaching a
// referenced row is a bug the database must refuse.
var noAction = entsql.OnDelete(entsql.NoAction)

func partialDate(name string) ent.Field {
	return field.String(name).Optional().Nillable()
}

func partialDateCheck(column string) string {
	return column + ` ~ '^([0-9]{4}(-[0-9]{2}(-[0-9]{2})?)?|unknown)$'`
}

func numeric(precision string) map[string]string {
	return map[string]string{dialect.Postgres: "numeric(" + precision + ")"}
}

func stamps() []ent.Field {
	return []ent.Field{
		with(field.JSON("field_state", map[string]any{}).Default(map[string]any{}), registry.Internal(registry.ShapeText, "Who wrote each field and on what grade (plan 03 §4).")),
		with(field.Int("version").Default(1), registry.Internal(registry.ShapeInteger, "Optimistic concurrency version, bumped by every commit.")),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}
