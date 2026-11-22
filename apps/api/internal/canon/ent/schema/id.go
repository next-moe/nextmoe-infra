package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/field"
)

// publicID draws from the family's sequence in canon_ids (idseq.Families),
// which a legacy table may share. The sequences are named after the family,
// not <table>_id_seq: with that name the Atlas diff read the column as
// bigserial and emitted a fresh sequence, which would restart at 1 and collide
// with legacy ids.
func publicID(family string) ent.Field {
	incremental := false
	return field.Int64("id").Immutable().Annotations(entsql.Annotation{
		Incremental: &incremental,
		DefaultExpr: "nextval('canon_ids." + family + "')",
	})
}
