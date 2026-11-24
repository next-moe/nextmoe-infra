package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// EvidenceSnapshot and EvidenceRecord are the prod-side evidence archive
// (D21): append-only copies of what supported a committed value, so the pool
// can be discarded.
type EvidenceSnapshot struct{ ent.Schema }

func (EvidenceSnapshot) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("evidence_snapshot")}
}

func (EvidenceSnapshot) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("requested_url"),
		field.String("final_url"),
		field.String("host"),
		field.Time("fetched_at"),
		field.Strings("lines"),
		field.Time("archived_at").Default(timeNow).Immutable(),
	}
}

type EvidenceRecord struct{ ent.Schema }

func (EvidenceRecord) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("evidence_record")}
}

func (EvidenceRecord) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.String("source"),
		field.String("key"),
		field.String("hash"),
		field.Time("fetched_at"),
		field.JSON("fields", map[string]any{}),
		field.Time("archived_at").Default(timeNow).Immutable(),
	}
}

// CommitKey makes a changeset idempotent: fanin keys it by pool and outbox id,
// so a crash between commit and cursor advance cannot write it twice.
type CommitKey struct{ ent.Schema }

func (CommitKey) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("commit_key")}
}

func (CommitKey) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.JSON("ids", map[string]int64{}),
		field.String("batch").Default(""),
		field.Time("created_at").Default(timeNow).Immutable(),
	}
}

type FaninCursor struct{ ent.Schema }

func (FaninCursor) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Table("fanin_cursor")}
}

func (FaninCursor) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").Immutable(),
		field.Int64("after").Default(0),
		field.Time("updated_at").Default(timeNow).UpdateDefault(timeNow),
	}
}
