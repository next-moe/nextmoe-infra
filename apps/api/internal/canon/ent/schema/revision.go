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
		// reviewed_by approved an agent's write (D29); the actor stays the agent.
		field.String("reviewed_by").Default(""),
		// batch is shared by every revision of one commit, the unit an undo takes back.
		field.String("batch").Default(""),
		// undoes is the revision an undo or remove took back.
		field.Int64("undoes").Default(0),
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
	return []ent.Index{index.Fields("entity", "entity_id"), index.Fields("root_entity", "root_id"), index.Fields("batch"), index.Fields("undoes"), index.Fields("actor_key", "created_at")}
}
