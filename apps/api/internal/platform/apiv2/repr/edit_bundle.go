package repr

type EditBundle struct {
	_            struct{}          `json:"-" additionalProperties:"true"`
	Object       string            `json:"object" enum:"edit_bundle" doc:"Type discriminant. Always edit_bundle."`
	FamilySchema ObjectSchema      `json:"schema" doc:"The body GET /v2/catalog/schemas/{object} answers."`
	Snapshot     SnapshotRecord    `json:"snapshot" doc:"The body GET /v2/moderation/snapshots/{object}/{id} answers."`
	Capabilities *EditCapabilities `json:"capabilities,omitempty" doc:"The body GET /v2/me/edit-capabilities/{object}?entity_id={id} answers. Absent when that face would have answered an error."`
	Context      *EditContext      `json:"context,omitempty" doc:"The body GET /v2/me/edit-context/{object}/{id} answers. Absent when that face would have answered an error."`
	Quota        *EditQuota        `json:"quota,omitempty" doc:"The body GET /v2/me/edit-quota answers. Absent when that face would have answered an error."`
}

type RefName struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Object      string                   `json:"object" enum:"ref_name" doc:"Type discriminant. Always ref_name."`
	Key         string                   `json:"key" pattern:"^[a-z_]+:[0-9]+$" maxLength:"64" doc:"family:id as requested in keys=, e.g. tag:12."`
	Family      string                   `json:"family" enum:"work,tag,company,engine,series,character,credit_name,person,role,trait,release" doc:"Family of the entity."`
	ID          string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog id."`
	DisplayName string                   `json:"display_name" maxLength:"512" doc:"The display_name the family's ids= batch lane answers; for a release, its title, empty when it has none. Must not be used as a discriminant."`
	NameZh      *string                  `json:"name_zh,omitempty" maxLength:"512" doc:"A Chinese name when one is recorded: a trait's name_zh, otherwise the zh-Hans, zh-Hant or zh entry of localized. Absent if none. Must not be used as a discriminant."`
	Lang        *string                  `json:"lang,omitempty" maxLength:"32" format:"bcp47" doc:"BCP-47 language of display_name: a work's olang, or the lang of a company, character or credit name. Absent if unrecorded or the family carries none. Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"The localized names that family's ids= batch lane answers (works with include=titles), BCP-47 keys. Empty object if none or the family carries none. Must not be used as a discriminant."`
}
