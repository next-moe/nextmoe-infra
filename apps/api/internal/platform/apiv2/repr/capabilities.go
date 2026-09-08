package repr

// EditCapabilities answers "what may I do to this object", which is a different
// question from "what shape is this object" and therefore a different URL:
// B34 blacklists varying one URL's field set by credential, so the capability
// axis cannot ride on /v2/catalog/schemas/{object}, which is credential-less.
// Join the two on `key`; nothing the public schema already publishes is
// repeated here.
type EditCapabilities struct {
	_            struct{}          `json:"-" additionalProperties:"true"`
	Object       string            `json:"object" enum:"edit_capabilities" doc:"Type discriminant. Always edit_capabilities."`
	TargetObject string            `json:"target_object" enum:"work,company,character,release,tag,engine,series" doc:"Family these capabilities describe."`
	EntityType   string            `json:"entity_type" enum:"catalog.work,catalog.label,catalog.character,catalog.release,catalog.tag,catalog.engine,catalog.series" doc:"Editing-engine type this family writes as."`
	EntityID     *string           `json:"entity_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entity these capabilities were evaluated against. null when the answer is type-level, which cannot express the owner channel."`
	Fields       []FieldCapability `json:"fields" doc:"One entry per editable field, in the schema's order. Empty array, never null."`
}

type FieldCapability struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Key            string   `json:"key" maxLength:"128" pattern:"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" doc:"Editing-engine field key. Joins /v2/catalog/schemas/{object} fields[].key. Must not be used as a discriminant."`
	Locked         bool     `json:"locked" doc:"true when this site's overlay accepts no proposal of this field from anyone."`
	CanPropose     bool     `json:"can_propose" doc:"true when the bearer may file a proposal touching this field."`
	CanReview      bool     `json:"can_review" doc:"true when the bearer may decide a proposal touching this field. Always false for a developer-owned app, whatever roles the user carries."`
	WouldAutomerge bool     `json:"would_automerge" doc:"true when a proposal of this field by this bearer would apply immediately rather than queue. Requires entity_id wherever the site grants the owner channel."`
}
