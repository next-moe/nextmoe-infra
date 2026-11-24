package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// IdentityKey is what a candidate search matches on (plan 06 §4): normalized
// titles and names, and registry authorities. Only the commit service writes
// it, together with the row a key is derived from (06 §6 I3).
type IdentityKey struct{ ent.Schema }

func (IdentityKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("identity_key")}
}

func (IdentityKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("kind"),
		field.String("key"),
		field.String("medium").Default(""),
	}
}

func (IdentityKey) Indexes() []ent.Index {
	return []ent.Index{index.Fields("entity", "entity_id", "kind", "key").Unique(), index.Fields("entity", "key")}
}

// ProgramState is a registered program's operating mode (D6, D29). A program
// with no row is in review.
type ProgramState struct{ ent.Schema }

func (ProgramState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("program_state")}
}

func (ProgramState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("mode"),
		field.String("note").Default(""),
		field.String("set_by"),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}
