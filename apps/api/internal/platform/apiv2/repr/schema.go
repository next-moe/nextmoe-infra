package repr

type ObjectSchema struct {
	_                struct{}      `json:"-" additionalProperties:"true"`
	Object           string        `json:"object" enum:"object_schema" doc:"Type discriminant. Always object_schema."`
	TargetObject     string        `json:"target_object" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait" doc:"Family this schema describes."`
	EntityType       string        `json:"entity_type" enum:"catalog.work,catalog.label,catalog.character,catalog.release,catalog.tag,catalog.engine,catalog.series,catalog.person,catalog.credit_name,catalog.trait" doc:"Editing-engine type this family writes as."`
	Include          []string      `json:"include" doc:"include= tokens the family's DETAIL face takes. Empty array, never null. full_set is a subset."`
	FullSet          []string      `json:"full_set" doc:"Tokens view=full expands on the detail face. A subset of include. Empty array, never null."`
	ListInclude      []string      `json:"list_include" doc:"include= tokens the family's COLLECTION face takes, which is not always the detail vocabulary: a list hydrates in batch, so blocks whose only shape is per-record stay on the detail face and on the sub-resources. Empty array, never null."`
	ListFullSet      []string      `json:"list_full_set" doc:"Tokens view=full expands on the collection face. A subset of list_include. Empty array, never null."`
	CreationDisabled bool          `json:"creation_disabled" doc:"true when new rows of this family cannot be created. Always true on release."`
	Fields           []SchemaField `json:"fields" doc:"Editable fields. Empty array, never null. Actor capabilities are not evaluated."`
}

type SchemaField struct {
	_             struct{}       `json:"-" additionalProperties:"true"`
	Key           string         `json:"key" maxLength:"128" pattern:"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" doc:"Editing-engine field key. Must not be used as a discriminant."`
	FieldType     string         `json:"field_type" enum:"text,i18nmap,enum,int,date,list,ref,imagehash" doc:"Control type. Not a domain vocabulary."`
	DiffHint      string         `json:"diff_hint" enum:"inline,lines,items,image" doc:"How a diff of this field should be rendered."`
	Deprecated    bool           `json:"deprecated" doc:"true when writes of this key are rejected."`
	MaxSuppressed int            `json:"max_suppressed" minimum:"0" doc:"Cap on this field's suppression set. 0 when the field has none."`
	MaxElements   int            `json:"max_elements" minimum:"0" doc:"Cap on a list field's element count. 0 for scalar fields."`
	Vocabulary    string         `json:"vocabulary" maxLength:"64" pattern:"^[a-z_]*$" doc:"Names the /v2/vocabularies vocabulary whose tokens this field accepts. Empty when the field has none. On an integer-coded field the tokens are labels and the wire carries base plus the token's index in the vocabulary's published order."`
	Encoding      *string        `json:"encoding,omitempty" enum:"token,int" maxLength:"5" doc:"How a write of this field carries the vocabulary value. Present exactly when vocabulary is set, absent otherwise. int means the wire carries base plus the token's index in the vocabulary's published order; token means the wire carries the token string itself. A vocabulary's published order is contractual only where it is read as int — under token encoding the order is presentational."`
	Base          int            `json:"base" minimum:"0" doc:"Wire code of the vocabulary's first token on an integer-coded field. 0 unless the model reserves 0 for null."`
	Nullable      bool           `json:"nullable" doc:"true when null is accepted and clears the stored value."`
	Element       *SchemaElement `json:"element,omitempty" doc:"Shape of one element of a list field. Absent on scalar fields and on list fields whose shape is not yet declared."`
}

type SchemaElement struct {
	_       struct{}              `json:"-" additionalProperties:"true"`
	Type    string                `json:"type" enum:"object,text,ref" doc:"Element type. text elements are strings, ref elements are decimal ids of another entity."`
	Members []SchemaElementMember `json:"members" doc:"Member fields of an object element, in validation order. Empty array for scalar elements, never null."`
}

type SchemaElementMember struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Key        string   `json:"key" maxLength:"64" doc:"Member key inside the element object. Must not be used as a discriminant."`
	Type       string   `json:"type" enum:"text,int,enum,bool,ref,imagehash" doc:"Member value type. ref is the decimal id of another entity."`
	Vocabulary string   `json:"vocabulary" maxLength:"64" pattern:"^[a-z_]*$" doc:"Names the /v2/vocabularies vocabulary whose tokens this member accepts. Empty when the member has none. On an int member the tokens are labels and the wire carries base plus the token's index in the vocabulary's published order."`
	Base       int      `json:"base" minimum:"0" doc:"Wire code of the vocabulary's first token on an int member. 0 unless the model reserves 0 for null."`
	Nullable   bool     `json:"nullable" doc:"true when the member may be absent or empty in at least one valid element; the field's validator decides when."`
}
