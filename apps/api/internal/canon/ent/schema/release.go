package schema

import (
	r "api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Release struct{ ent.Schema }

func (Release) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("release"), entsql.Checks(map[string]string{"release_released_shape": partialDateCheck("released")})}
}

func (Release) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("release"),
		with(field.String("lang"), r.Fact(r.ShapeLang, "BCP-47 language of this edition's text.", "R4.4")),
		with(field.String("edition_kind").Default("standard"), r.VocabFact("edition_kind", "Standard, limited or reprint.", "R4.2")),
		with(field.String("format"), r.VocabFact("format", "Physical or digital form.", "R4.2")),
		with(partialDate("released"), r.Fact(r.ShapePartialDate, "Release date; precision as stated, never completed.", "R8.1")),
		with(field.String("release_status").Default("released"), r.VocabFact("release_status", "Announced, released or cancelled.", "R8.1")),
		with(field.String("official_url").Optional(), r.Fact(r.ShapeURL, "The publisher's own page for this edition.", "R2.2")),
	}, stamps()...)
}

func (Release) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("contents", ReleaseContent.Type).Annotations(noAction),
		edge.To("book", BookRelease.Type).Unique().Annotations(noAction),
		edge.To("credits", Credit.Type).Annotations(noAction),
	}
}

type ReleaseContent struct{ ent.Schema }

func (ReleaseContent) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("release_content")}
}

func (ReleaseContent) Fields() []ent.Field {
	return []ent.Field{
		with(field.Int64("release_id"), r.RefFact("release", "The edition.", "R4.2")),
		with(field.Int64("work_id"), r.RefFact("work", "A work this edition contains.", "R4.2", "R5")),
		with(field.Int64("part_id").Optional().Nillable(), r.RefFact("work_part", "The volume or episode it contains, when it is one part.", "R4.2")),
		with(field.String("portion").Default("full"), r.VocabFact("portion", "Whether the whole part is contained.", "R4.2")),
		field.Int("position").Default(0),
	}
}

func (ReleaseContent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("release", Release.Type).Ref("contents").Field("release_id").Unique().Required(),
		edge.From("work", Work.Type).Ref("contents").Field("work_id").Unique().Required(),
		edge.From("part", WorkPart.Type).Ref("contents").Field("part_id").Unique(),
	}
}
