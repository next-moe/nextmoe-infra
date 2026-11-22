package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

type Work struct {
	ent.Schema
}

func (Work) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("work")}
}

func (Work) Fields() []ent.Field {
	return []ent.Field{
		sharedID("work"),
		field.String("medium").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
