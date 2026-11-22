package registry

type Code struct {
	Code string `json:"code"`
	Doc  string `json:"doc"`
}

// RefusalCodes is the closed list fanin answers in the pool inbox (plan 05 §6,
// §15); ingest feeds them back to the model, so a code must never be renamed.
var RefusalCodes = []Code{
	{"CONTRACT_VERSION_UNSUPPORTED", "the row names a contract version fanin does not accept"},
	{"SNAPSHOT_MISSING", "an evidence snapshot is absent from object storage or its hash differs"},
	{"EXCERPT_NOT_ON_PAGE", "the cited lines are out of range or not in this lookup's snapshots"},
	{"VALUE_NOT_ENTAILED", "the excerpt does not support the value for its shape"},
	{"TIER_NOT_ALLOWED", "the source's grade is not allowed for this property"},
	{"PROPERTY_OUT_OF_SCOPE", "the program is not registered to write this property"},
	{"PROPERTY_UNKNOWN", "the property is not in the registry"},
	{"VOCAB_TERM_UNKNOWN", "the value is not a term of the property's vocabulary"},
	{"HUMAN_LOCKED", "a person wrote this field; agents do not overwrite it"},
	{"STRONGER_VALUE_EXISTS", "the current value rests on a stronger tier than this evidence"},
	{"ENTITY_HELD", "the entity is held (takedown, dispute or review)"},
	{"ENTITY_MERGED", "the entity was merged; resubmit against its survivor"},
	{"UNIQUE_CONFLICT", "another entity already holds this unique key"},
	{"IDENTITY_UNSURE", "identity could not be decided; a todo was opened"},
	{"HOST_EXCLUDED", "the evidence comes from an excluded host"},
}
