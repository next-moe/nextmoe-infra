package schema

import (
	r "api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Work struct{ ent.Schema }

func (Work) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("work")} }

func (Work) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("work"),
		with(field.String("medium"), r.VocabFact("medium", "Medium by form (D15); fixed when the work is created.", "R1.1")),
		with(field.String("olang").Optional(), r.Fact(r.ShapeLang, "BCP-47 language of the original edition.", "R4.1")),
		with(field.String("status").Default("unknown"), r.VocabFact("work_status", "Publication status of the whole work.", "R4.3")),
		with(field.String("content_rating").Default("all_ages"), r.VocabFact("content_rating", "Age rating of the work itself.", "R1.3")),
		with(field.String("display_safety").Default("safe"), r.Internal(r.ShapeVocab, "Whether cover and synopsis may show on public pages; set by the safety pipeline, never by ingest.")),
	}, stamps()...)
}

func (Work) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("titles", WorkTitle.Type).Annotations(noAction),
		edge.To("parts", WorkPart.Type).Annotations(noAction),
		edge.To("contents", ReleaseContent.Type).Annotations(noAction),
		edge.To("book", BookWork.Type).Unique().Annotations(noAction),
		edge.To("credits", Credit.Type).Annotations(noAction),
	}
}

type WorkTitle struct{ ent.Schema }

func (WorkTitle) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("work_title")}
}

func (WorkTitle) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("work_id"),
		with(field.String("text").NotEmpty(), r.Fact(r.ShapeTitle, "A title exactly as printed.", "R3.1")),
		with(field.String("lang"), r.Fact(r.ShapeLang, "BCP-47 language of the title.", "R3.1")),
		with(field.String("kind"), r.VocabFact("title_kind", "Official, alternative, romanized or translated.", "R3.2")),
		with(field.Bool("machine").Default(false), r.Internal(r.ShapeBool, "True when the title is a machine translation.")),
		field.Int("position").Default(0),
	}
}

func (WorkTitle) Edges() []ent.Edge {
	return []ent.Edge{edge.From("work", Work.Type).Ref("titles").Field("work_id").Unique().Required()}
}

type WorkPart struct{ ent.Schema }

func (WorkPart) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("work_part"), entsql.Checks(map[string]string{"work_part_released_shape": partialDateCheck("released")})}
}

func (WorkPart) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("work_part"),
		field.Int64("work_id"),
		field.Int64("parent_id").Optional().Nillable(),
		with(field.String("kind"), r.VocabFact("part_kind", "Volume, chapter, season, episode or disc.", "R4.2")),
		with(field.Float("ordinal").Optional().Nillable().SchemaType(numeric("10,2")), r.Fact(r.ShapeOrdinal, "Sort position within the parent; 8.5 is allowed.", "R4.2")),
		with(field.String("label").Optional(), r.Fact(r.ShapeText, "Volume or episode label exactly as printed, e.g. 「上」 or 「第3巻」.", "R4.2")),
		with(partialDate("released"), r.Fact(r.ShapePartialDate, "First release of this part; precision as stated, never completed.", "R8.1")),
	}, stamps()...)
}

func (WorkPart) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("work", Work.Type).Ref("parts").Field("work_id").Unique().Required(),
		edge.To("children", WorkPart.Type).Annotations(noAction).From("parent").Field("parent_id").Unique(),
		edge.To("contents", ReleaseContent.Type).Annotations(noAction),
		edge.To("credits", Credit.Type).Annotations(noAction),
	}
}
