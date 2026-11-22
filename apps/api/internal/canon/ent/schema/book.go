package schema

import (
	r "api/internal/canon/registry"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// BookWork and BookRelease are the book module's extension tables (plan 03 §3):
// keyed by the core id only, never referencing another module.
type BookWork struct{ ent.Schema }

func (BookWork) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("book_work")}
}

func (BookWork) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("work_id").Unique(),
		with(field.String("category"), r.VocabFact("book_category", "Light novel, light bungei, shin bungei or general.", "R1.4")),
	}
}

func (BookWork) Edges() []ent.Edge {
	return []ent.Edge{edge.From("work", Work.Type).Ref("book").Field("work_id").Unique().Required()}
}

type BookRelease struct{ ent.Schema }

func (BookRelease) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("book_release")}
}

func (BookRelease) Fields() []ent.Field {
	isbn := r.Fact(r.ShapeISBN13, "ISBN-13 of this edition; the check digit must hold.", "R4.2", "R8.2")
	isbn.Unique = r.UniqueGlobal
	jan := r.Fact(r.ShapeJAN, "JAN of this edition; a set's JAN stands for the set only.", "R4.2", "R8.2")
	jan.Unique = r.UniqueGlobal
	return []ent.Field{
		field.Int64("release_id").Unique(),
		with(field.String("isbn13").Optional().Nillable(), isbn),
		with(field.String("jan").Optional().Nillable(), jan),
		with(field.Int64("publisher_id").Optional().Nillable(), r.RefFact("org", "Publisher of record.", "R7.2")),
		with(field.Int64("label_id").Optional().Nillable(), r.RefFact("org", "The label (レーベル) the edition is published under.", "R7.2")),
		with(field.String("label_number").Optional(), r.Fact(r.ShapeText, "The label's own serial, e.g. 4347 in 「電撃文庫 ; 4347」.", "R4.2")),
		with(field.Int("pages").Optional().Nillable(), r.Fact(r.ShapeInteger, "Page count.", "R8.2")),
		with(field.String("price_amount").Optional().Nillable().SchemaType(numeric("12,2")), r.Fact(r.ShapeMoney, "List price, as printed.", "R8.2")),
		with(field.String("price_currency").Optional(), r.Fact(r.ShapeCurrency, "ISO 4217 currency of the price.", "R8.2")),
		with(field.Bool("price_tax_included").Optional().Nillable(), r.Fact(r.ShapeBool, "Whether the printed price includes tax.", "R8.2")),
	}
}

func (BookRelease) Indexes() []ent.Index { return uniqueIndexes(BookRelease{}.Fields()) }

func (BookRelease) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("release", Release.Type).Ref("book").Field("release_id").Unique().Required(),
		edge.From("publisher", Org.Type).Ref("published").Field("publisher_id").Unique(),
		edge.From("label", Org.Type).Ref("labelled").Field("label_id").Unique(),
	}
}
