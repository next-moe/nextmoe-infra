package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

// sharedID draws from the sequence the legacy table also uses (idseq.Families).
// The sequences are named after the family, not <table>_id_seq: with that name
// the Atlas diff read the column as bigserial and emitted a fresh sequence,
// which would restart at 1 and collide with legacy ids.
func sharedID(seq string) ent.Field {
	incremental := false
	return field.Int64("id").Immutable().Annotations(entsql.Annotation{
		Incremental: &incremental,
		DefaultExpr: "nextval('canon_ids." + seq + "')",
	})
}
