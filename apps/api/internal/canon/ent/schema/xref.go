package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// XrefLink is the non-canon read model of third-party database ids (D22): it
// is rebuilt from its sources, may be dropped at any time, and canon never
// reads it. It keeps refs=vndb: lookups working for moyu and partners.
type XrefLink struct{ ent.Schema }

func (XrefLink) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("xref_link")}
}

func (XrefLink) Fields() []ent.Field {
	return []ent.Field{
		field.String("source"),
		field.String("external_id"),
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("origin").Default("legacy"),
		field.Time("imported_at").Default(timeNow),
	}
}

func (XrefLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("source", "external_id", "entity").Unique(),
		index.Fields("entity", "entity_id"),
	}
}
