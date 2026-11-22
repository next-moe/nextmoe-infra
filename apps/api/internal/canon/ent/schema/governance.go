package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Todo is a human work item (plan 07 §4): one row per kind and subject, so the
// same unresolved question is never filed twice.
type Todo struct{ ent.Schema }

func (Todo) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("todo")} }

func (Todo) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind"),
		field.String("subject_key").Unique(),
		field.String("state").Default("open"),
		field.String("detail").Default(""),
		field.String("source").Default(""),
		field.JSON("suggestion", map[string]any{}).Optional(),
		field.String("resolved_by").Optional(),
		// answered is set once fanin has told the pool how a person settled
		// a todo that came from the pool.
		field.Bool("answered").Default(false),
		field.JSON("resolution", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Todo) Indexes() []ent.Index { return []ent.Index{index.Fields("state", "kind")} }

// Hold stops agents from writing an entity or any value pointing at it
// (plan 07 §4); a takedown places one too.
type Hold struct{ ent.Schema }

func (Hold) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("hold")} }

func (Hold) Fields() []ent.Field {
	return []ent.Field{
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("reason"),
		field.String("created_by"),
		field.Int("takedown_id").Optional().Nillable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("released_at").Optional().Nillable(),
	}
}

func (Hold) Indexes() []ent.Index { return []ent.Index{index.Fields("entity", "entity_id")} }

// Takedown is one notice-and-takedown action (plan 08 §3); restoring sets
// restored_at instead of deleting the row.
type Takedown struct{ ent.Schema }

func (Takedown) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("takedown")}
}

func (Takedown) Fields() []ent.Field {
	return []ent.Field{
		field.String("notice_id"),
		field.String("scope"),
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("asset_hash").Optional(),
		field.String("reason"),
		field.String("created_by"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("restored_at").Optional().Nillable(),
		field.String("restored_by").Optional(),
		field.String("restore_note").Optional(),
	}
}

func (Takedown) Indexes() []ent.Index { return []ent.Index{index.Fields("entity", "entity_id")} }
