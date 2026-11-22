package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

type Revision struct{ ent.Schema }

func (Revision) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("revision")}
}

func (Revision) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("action"),
		field.String("actor_kind"),
		field.String("actor_key"),
		field.String("note").Optional(),
		field.JSON("changes", map[string]any{}),
		// root is the public entity a change shows up on, so the change feed
		// names the work for a title or credit edit, not the child row.
		field.String("root_entity").Default(""),
		field.Int64("root_id").Default(0),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

func (Revision) Indexes() []ent.Index {
	return []ent.Index{index.Fields("entity", "entity_id"), index.Fields("root_entity", "root_id")}
}
