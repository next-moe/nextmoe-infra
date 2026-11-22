package schema

import (
	r "api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Credit attaches one person alias or one org to exactly one target: a work,
// a part or a release (plan 03 §2).
type Credit struct{ ent.Schema }

func (Credit) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("credit"), entsql.Checks(map[string]string{
		"credit_one_target": "num_nonnulls(work_id, part_id, release_id) = 1",
		"credit_one_holder": "num_nonnulls(alias_id, org_id) = 1",
	})}
}

func (Credit) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("credit"),
		with(field.Int64("work_id").Optional().Nillable(), r.RefFact("work", "Credited on the whole work.", "R7.1")),
		with(field.Int64("part_id").Optional().Nillable(), r.RefFact("work_part", "Credited on one part.", "R7.1")),
		with(field.Int64("release_id").Optional().Nillable(), r.RefFact("release", "Credited on one edition, e.g. a translator.", "R7.1")),
		with(field.Int64("alias_id").Optional().Nillable(), r.RefFact("person_alias", "The name as credited.", "R7.1")),
		with(field.Int64("org_id").Optional().Nillable(), r.RefFact("org", "An organization credited instead of a person.", "R7.2")),
		with(field.String("role"), r.VocabFact("book_role", "Role in the medium module's vocabulary.", "R7.1")),
		field.Int("position").Default(0),
	}, stamps()...)
}

func (Credit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("work", Work.Type).Ref("credits").Field("work_id").Unique(),
		edge.From("part", WorkPart.Type).Ref("credits").Field("part_id").Unique(),
		edge.From("release", Release.Type).Ref("credits").Field("release_id").Unique(),
		edge.From("alias", PersonAlias.Type).Ref("credits").Field("alias_id").Unique(),
		edge.From("org", Org.Type).Ref("credits").Field("org_id").Unique(),
	}
}
