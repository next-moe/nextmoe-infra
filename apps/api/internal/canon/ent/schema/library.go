package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// LibraryPartProgress is a reader's progress on one volume, chapter or
// episode (plan 03 §7): media-neutral, since every medium with parts has it.
type LibraryPartProgress struct{ ent.Schema }

func (LibraryPartProgress) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("library_part_progress")}
}

func (LibraryPartProgress) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("actor_uid"),
		field.Int64("part_id"),
		field.String("state"),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (LibraryPartProgress) Indexes() []ent.Index {
	return []ent.Index{index.Fields("actor_uid", "part_id").Unique(), index.Fields("actor_uid", "updated_at")}
}

func (LibraryPartProgress) Edges() []ent.Edge {
	return []ent.Edge{edge.From("part", WorkPart.Type).Ref("progress").Field("part_id").Unique().Required()}
}

// LibraryTombstone records a removal so every device following /v3/me/changes
// removes it too (12 F16).
type LibraryTombstone struct{ ent.Schema }

func (LibraryTombstone) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("library_tombstone")}
}

func (LibraryTombstone) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("actor_uid"),
		field.String("kind"),
		field.Int64("subject_id"),
		field.Time("deleted_at").Default(timeNow),
	}
}

func (LibraryTombstone) Indexes() []ent.Index {
	return []ent.Index{index.Fields("actor_uid", "deleted_at")}
}
