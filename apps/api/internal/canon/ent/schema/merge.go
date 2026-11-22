package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Redirect answers a merged id with its survivor (I3); it is flattened on
// write, so a chain of merges never needs two hops.
type Redirect struct{ ent.Schema }

func (Redirect) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("redirect")}
}

func (Redirect) Fields() []ent.Field {
	return []ent.Field{
		field.String("entity"),
		field.Int64("old_id"),
		field.Int64("new_id"),
		field.Int("merge_id"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

func (Redirect) Indexes() []ent.Index {
	return []ent.Index{index.Fields("entity", "old_id"), index.Fields("entity", "new_id")}
}

// MergeRecord archives what a merge changed, so an unmerge can put it back.
type MergeRecord struct{ ent.Schema }

func (MergeRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("merge_record")}
}

func (MergeRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("entity"),
		field.Int64("loser_id"),
		field.Int64("survivor_id"),
		field.String("by"),
		field.String("reason"),
		field.JSON("loser_row", map[string]any{}),
		field.JSON("movements", []map[string]any{}),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("undone_at").Optional().Nillable(),
		field.String("undone_by").Optional(),
	}
}
