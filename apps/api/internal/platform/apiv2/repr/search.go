package repr

type SearchHit struct {
	_             struct{}                 `json:"-" additionalProperties:"true"`
	Object        string                   `json:"object" enum:"search_result" doc:"Type discriminant. Always search_result."`
	TargetObject  string                   `json:"target_object" enum:"work,character,credit_name,company,tag,series,engine,trait" doc:"Family of the hit."`
	ID            string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog id of the hit."`
	DisplayName   string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Latin         *string                  `json:"latin" maxLength:"512" doc:"null if unrecorded. Must not be used as a discriminant."`
	Localized     map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	Sources       []string                 `json:"sources" doc:"Open vocabulary sources. Empty array, never null."`
	ContentRating *string                  `json:"content_rating" enum:"all_ages,sensitive,r18" doc:"Set on work hits. null otherwise."`
	Tier          *string                  `json:"tier" enum:"core,longtail,hidden" doc:"Set on tag hits. null otherwise."`
	TagKind       *string                  `json:"tag_kind" enum:"content,meta" doc:"Set on tag hits. null otherwise."`
	IsSexual      *bool                    `json:"is_sexual" doc:"Set on trait hits. null otherwise."`
	TraitPath     *TraitPath               `json:"trait_path" doc:"Set on trait hits. null otherwise."`
}

type TraitPath struct {
	_              struct{}                 `json:"-" additionalProperties:"true"`
	GroupID        *string                  `json:"group_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog trait id of the root group. null for a root trait."`
	Group          *string                  `json:"group" maxLength:"512" doc:"Root trait group name. null for a root trait. Must not be used as a discriminant."`
	GroupLocalized map[string]LocalizedText `json:"group_localized" doc:"Localized names of the root group, BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	Parents        []TraitRef               `json:"parents" doc:"Direct parents, ordered by id. Empty array for a root. Never null."`
}
