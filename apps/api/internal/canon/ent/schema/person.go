package schema

import (
	r "api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

type Person struct{ ent.Schema }

func (Person) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("person")} }

func (Person) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("person"),
		with(field.Int64("primary_alias_id").Optional().Nillable(), r.Internal(r.ShapeRef, "The alias the person is shown under; one of its own aliases.")),
	}, stamps()...)
}

func (Person) Edges() []ent.Edge {
	return []ent.Edge{edge.To("aliases", PersonAlias.Type).Annotations(noAction)}
}

// PersonAlias rows are the legacy credit names, ids unchanged (D19).
type PersonAlias struct{ ent.Schema }

func (PersonAlias) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("person_alias")}
}

func (PersonAlias) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("person_alias"),
		with(field.Int64("person_id"), r.RefFact("person", "The person this name belongs to.", "R7.1")),
		with(field.String("name").NotEmpty(), r.Fact(r.ShapePersonName, "The name exactly as credited.", "R7.1")),
		with(field.String("latin").Optional(), r.Fact(r.ShapeText, "Romanization of the name.", "R7.1")),
		with(field.String("lang").Optional(), r.Fact(r.ShapeLang, "BCP-47 language of the name.", "R7.1")),
	}, stamps()...)
}

func (PersonAlias) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("person", Person.Type).Ref("aliases").Field("person_id").Unique().Required(),
		edge.To("credits", Credit.Type).Annotations(noAction),
	}
}

type Org struct{ ent.Schema }

func (Org) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("org")} }

func (Org) Fields() []ent.Field {
	return append([]ent.Field{
		publicID("org"),
		with(field.String("kind"), r.VocabFact("org_kind", "Company, brand, label, magazine, platform, circle or translation group.", "R7.2")),
		with(field.Int64("parent_id").Optional().Nillable(), r.RefFact("org", "The publisher a label belongs to.", "R7.2")),
	}, stamps()...)
}

func (Org) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("names", OrgName.Type).Annotations(noAction),
		edge.To("children", Org.Type).Annotations(noAction).From("parent").Field("parent_id").Unique(),
		edge.To("credits", Credit.Type).Annotations(noAction),
		edge.To("published", BookRelease.Type).Annotations(noAction),
		edge.To("labelled", BookRelease.Type).Annotations(noAction),
	}
}

type OrgName struct{ ent.Schema }

func (OrgName) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("org_name")}
}

func (OrgName) Fields() []ent.Field {
	return []ent.Field{
		with(field.Int64("org_id"), r.RefFact("org", "The organization this name belongs to.", "R7.2")),
		with(field.String("text").NotEmpty(), r.Fact(r.ShapeTitle, "A name of the organization as it presents itself.", "R7.2")),
		with(field.String("lang"), r.Fact(r.ShapeLang, "BCP-47 language of the name.", "R7.2")),
		with(field.Int("position").Default(0), r.Internal(r.ShapeInteger, "Display order among siblings; set by people and the system, not by agents.")),
	}
}

func (OrgName) Edges() []ent.Edge {
	return []ent.Edge{edge.From("org", Org.Type).Ref("names").Field("org_id").Unique().Required()}
}
