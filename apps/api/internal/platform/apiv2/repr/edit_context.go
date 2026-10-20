package repr

type EditContext struct {
	_            struct{}           `json:"-" additionalProperties:"true"`
	Object       string             `json:"object" enum:"edit_context" doc:"Type discriminant. Always edit_context."`
	TargetObject string             `json:"target_object" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait" doc:"Family of the entity."`
	EntityType   string             `json:"entity_type" maxLength:"64" doc:"Editing-engine type. Must not be used as a discriminant."`
	EntityID     string             `json:"entity_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog id of the entity."`
	Fields       []EditContextField `json:"fields" doc:"One entry per registered field, in the schema's order, including every .suppressed companion. Empty array, never null."`
}

type EditContextField struct {
	_             struct{}           `json:"-" additionalProperties:"true"`
	Key           string             `json:"key" maxLength:"128" pattern:"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" doc:"Editing-engine field key. Joins /v2/catalog/schemas/{object} fields[].key. Must not be used as a discriminant."`
	Value         any                `json:"value" doc:"The value a proposal is written against: the engine snapshot, in the shape a patch sends. For a list field whose importers keep their own rows this is the human lane only; rows lists everything stored. null when unset."`
	Provenance    []ColumnProvenance `json:"provenance" doc:"For a scalar field, who wrote each column it lands in, newest first. Empty array for list fields and for columns that record no provenance."`
	Rows          *[]EditContextRow  `json:"rows" doc:"For a list field whose rows are stored per source, every stored row plus every suppression whose row is gone. null for scalar fields, .suppressed companions, and list fields stored as one value on the entity row."`
	RowsTruncated bool               `json:"rows_truncated" doc:"true when the field holds more stored rows than the face returns; the first rows by source and position are kept."`
}

type ColumnProvenance struct {
	_       struct{}          `json:"-" additionalProperties:"true"`
	Column  string            `json:"column" maxLength:"64" doc:"Database column the field lands in. Must not be used as a discriminant."`
	History []ProvenanceEntry `json:"history" doc:"Recorded writes, newest first. The first entry is whoever set the current value. Empty array when nothing was recorded."`
}

type ProvenanceEntry struct {
	_      struct{} `json:"-" additionalProperties:"true"`
	Source string   `json:"source" maxLength:"64" doc:"Writer key: a catalog source (see the sources vocabulary), or user / curated for a person. Open vocabulary; must not be used as a discriminant."`
	Human  bool     `json:"human" doc:"true when a person wrote this value. Importers never overwrite a column whose newest entry is human."`
	At     *string  `json:"at" maxLength:"40" doc:"When the write was recorded, as the writer stamped it (normally RFC 3339). null when it recorded no time. Must not be used as a discriminant."`
}

type EditContextRow struct {
	_                 struct{}       `json:"-" additionalProperties:"true"`
	Source            *EditSource    `json:"source" doc:"Lane that stored the row. null when the table records no source, and for a suppression whose row is gone."`
	HumanLane         bool           `json:"human_lane" doc:"true when the row belongs to the human lane, which the field's value edits directly."`
	Identity          *string        `json:"identity" maxLength:"512" doc:"Row identity key. Sending it in the field's .suppressed companion hides the row from every read face. null when the field has no suppression companion, or the row cannot be keyed (an upstream tag with no canonical tag). Opaque; must not be used as a discriminant."`
	Suppressed        bool           `json:"suppressed" doc:"true when the field's suppression set hides this row."`
	Present           bool           `json:"present" doc:"false for a suppression whose row is no longer stored: the importer dropped it, and the suppression keeps it hidden if it comes back."`
	Value             any            `json:"value" doc:"The stored row, using the field's element member names where they apply. null when present is false."`
	MachineTranslated bool           `json:"machine_translated" doc:"true for a row machine-translated from another row."`
	FieldProvenance   map[string]any `json:"field_provenance" doc:"Per-column write history of this one row, for tables that keep it. null otherwise."`
}

type EditSource struct {
	_       struct{} `json:"-" additionalProperties:"true"`
	Key     string   `json:"key" maxLength:"64" doc:"catalog_source key. Open vocabulary (the sources vocabulary); must not be used as a discriminant."`
	Display string   `json:"display" maxLength:"128" doc:"Human-readable name from the sources vocabulary, or the key when the vocabulary has none. Must not be used as a discriminant."`
}

type UserRef struct {
	_      struct{} `json:"-" additionalProperties:"true"`
	Object string   `json:"object" enum:"user" doc:"Type discriminant. Always user."`
	ID     string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The central account id, the same value the record's *_uid field carries."`
	Name   string   `json:"name" maxLength:"64" doc:"Display name. An erased or removed account reads 已注销#<id>. Must not be used as a discriminant."`
	Avatar *string  `json:"avatar" format:"uri" maxLength:"512" doc:"Absolute avatar URL. null when the account has none."`
}
