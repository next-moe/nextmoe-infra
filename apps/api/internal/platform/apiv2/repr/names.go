package repr

type LocalizedText struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Value     string   `json:"value" maxLength:"512" doc:"Must not be used as a discriminant."`
	IsMachine bool     `json:"is_machine" doc:"Whether this value is machine-translated."`
}

type WorkTitle struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Lang      string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag."`
	Title     string   `json:"title" maxLength:"512" doc:"Must not be used as a discriminant."`
	Latin     *string  `json:"latin" maxLength:"512" doc:"Romanization. null if unrecorded. Must not be used as a discriminant."`
	TitleKind string   `json:"title_kind" enum:"official,alias,abbreviation" doc:"search_hint is internal and never appears on this type."`
	IsMachine bool     `json:"is_machine" doc:"Whether this title is machine-translated."`
}

type EntityName struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Lang      string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag."`
	Value     string   `json:"value" maxLength:"512" doc:"Must not be used as a discriminant."`
	AliasKind string   `json:"alias_kind" enum:"translation,spelling_variant" doc:"search_hint is internal and never appears on this type."`
	IsMachine bool     `json:"is_machine" doc:"Whether this name is machine-translated."`
}

type Intro struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Lang      string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag."`
	Value     string   `json:"value" maxLength:"20000" doc:"Must not be used as a discriminant."`
	IsMachine bool     `json:"is_machine" doc:"Whether this intro is machine-translated."`
	Source    string   `json:"source" maxLength:"64" doc:"Open vocabulary sources. Must not be used as a discriminant."`
}
