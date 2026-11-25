package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Todo is a human work item (plan 07 §4): one row per kind and subject, so the
// same unresolved question is never filed twice.
type Todo struct{ ent.Schema }

func (Todo) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("todo")} }

func (Todo) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind"),
		field.String("subject_key").Unique(),
		// group_key is the question whose newest content this is; a newer row
		// for the same group supersedes the open one.
		field.String("group_key").Default(""),
		field.String("program").Default(""),
		field.String("state").Default("open"),
		field.String("detail").Default(""),
		field.String("source").Default(""),
		// waiting is every pool row answered "todo" with this content.
		field.Strings("waiting").Optional(),
		field.JSON("suggestion", map[string]any{}).Optional(),
		// changeset is the suggestion after fanin verified it: what accepting
		// commits, evidence and tiers included, so a decision never re-reads the pool.
		field.JSON("changeset", map[string]any{}).Optional(),
		field.JSON("questions", []map[string]any{}).Optional(),
		field.JSON("notes", []map[string]any{}).Optional(),
		field.String("resolved_by").Optional(),
		// answered is set once fanin has told the pool how a person settled
		// a todo that came from the pool.
		field.Bool("answered").Default(false),
		field.JSON("resolution", map[string]any{}).Optional(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}

func (Todo) Indexes() []ent.Index {
	return []ent.Index{index.Fields("state", "kind"), index.Fields("group_key")}
}

// Hold stops agents from writing an entity or any value pointing at it
// (plan 07 §4); a takedown places one too.
type Hold struct{ ent.Schema }

func (Hold) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("hold")} }

func (Hold) Fields() []ent.Field {
	return []ent.Field{
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("reason"),
		field.String("created_by"),
		field.Int("takedown_id").Optional().Nillable(),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("released_at").Optional().Nillable(),
	}
}

func (Hold) Indexes() []ent.Index { return []ent.Index{index.Fields("entity", "entity_id")} }

// Takedown is one notice-and-takedown action (plan 08 §3); restoring sets
// restored_at instead of deleting the row.
type Takedown struct{ ent.Schema }

func (Takedown) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("takedown")}
}

func (Takedown) Fields() []ent.Field {
	return []ent.Field{
		field.String("notice_id"),
		field.String("scope"),
		field.String("entity"),
		field.Int64("entity_id"),
		field.String("asset_hash").Optional(),
		field.String("reason"),
		field.String("created_by"),
		field.Time("created_at").Default(timeNow).Immutable(),
		field.Time("restored_at").Optional().Nillable(),
		field.String("restored_by").Optional(),
		field.String("restore_note").Optional(),
		// exclude_host is set when the rights holder also asks that their
		// site not be crawled (plan 08 §4.5).
		field.String("exclude_host").Optional(),
		field.Time("pushed_at").Optional().Nillable(),
		field.Time("restore_pushed_at").Optional().Nillable(),
	}
}

func (Takedown) Indexes() []ent.Index { return []ent.Index{index.Fields("entity", "entity_id")} }

// Patrol is a commit of an agent a person has looked at (plan 07 §4). It marks
// and changes nothing, and no agent ever writes one.
type Patrol struct{ ent.Schema }

func (Patrol) Annotations() []schema.Annotation { return []schema.Annotation{entsql.Table("patrol")} }

func (Patrol) Fields() []ent.Field {
	return []ent.Field{
		field.String("batch").Unique(),
		field.String("by"),
		field.Time("at").Default(timeNow).Immutable(),
	}
}

// TitleVerdict is a model's reading of one set of titles, keyed by the set, so
// a work whose titles did not change is never judged twice.
type TitleVerdict struct{ ent.Schema }

func (TitleVerdict) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("title_verdict")}
}

func (TitleVerdict) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("class"),
		field.Float("confidence"),
		field.String("model"),
		field.String("prompt_version"),
		field.Time("judged_at").Default(timeNow),
	}
}
