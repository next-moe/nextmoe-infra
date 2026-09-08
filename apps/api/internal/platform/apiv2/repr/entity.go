package repr

type Tag struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"tag" doc:"Type discriminant. Always tag."`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog tag id."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Tier        string   `json:"tier" enum:"core,longtail,hidden" doc:"Tag inventory tier."`
	TagKind     string   `json:"tag_kind" enum:"content,meta" doc:"Tag vocabulary class."`
	WorkCount   int      `json:"work_count" minimum:"0" doc:"Works visible under the same NSFW gate."`
	IsSexual    bool     `json:"is_sexual" doc:"Whether this tag is in the sexual family. Filter on it before rendering a tag list to an SFW reader."`
	Intros      *[]Intro `json:"intros,omitempty" doc:"Tag descriptions, one per language. Present when include=intros, detail face only. Empty array if none. is_machine is always false here: the store carries no provenance for a tag description, so false records unknown, not human-written."`
}

type Company struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Object      string                   `json:"object" enum:"company" doc:"Type discriminant. Always company."`
	ID          string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog company id."`
	DisplayName string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Latin       *string                  `json:"latin" maxLength:"512" doc:"null if unrecorded. Must not be used as a discriminant."`
	Lang        *string                  `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag of display_name. null if unrecorded. Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	CompanyKind string                   `json:"company_kind" enum:"game_brand,bunko,publisher,anime_studio,doujin_circle,group" doc:"Company registry class. No other."`
	WorkCount   int                      `json:"work_count" minimum:"0" doc:"Works visible under the same NSFW gate."`
	Aliases     *[]EntityName            `json:"aliases,omitempty" doc:"Present when include=aliases. Empty array if none."`
	Logo        *Image                   `json:"logo,omitempty" doc:"Present only when include=logo and this company has a logo; absent otherwise."`
	Intros      *[]Intro                 `json:"intros,omitempty" doc:"Present when include=intros on the detail and batch lanes; the cursor list lane does not carry it. Empty array if none."`
	Links       *[]WorkLink              `json:"links,omitempty" doc:"Present when include=links on the detail and batch lanes; the cursor list lane does not carry it. Empty array if none."`
}

type CreditName struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Object      string                   `json:"object" enum:"credit_name" doc:"Type discriminant. Always credit_name."`
	ID          string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog credit-name id."`
	DisplayName string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Latin       *string                  `json:"latin" maxLength:"512" doc:"null if unrecorded. Must not be used as a discriminant."`
	Lang        *string                  `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag of display_name. null if unrecorded. Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	PersonID    *string                  `json:"person_id" pattern:"^[0-9]+$" maxLength:"20" doc:"null if this name is not linked to a person."`
	Gender      *string                  `json:"gender,omitempty" enum:"male,female,other" doc:"Person-level fact reached through the person link. Present on the detail face; absent when unrecorded or the name has no public person link."`
	BirthYear   *int                     `json:"birth_year,omitempty" minimum:"0" maximum:"9999" doc:"Person-level fact reached through the person link. Present on the detail face; absent when unrecorded or the name has no public person link. The three birth parts are independently fuzzy: a year can exist with no month or day."`
	BirthMonth  *int                     `json:"birth_month,omitempty" minimum:"1" maximum:"12" doc:"Person-level fact reached through the person link. Present on the detail face; absent when unrecorded or the name has no public person link."`
	BirthDay    *int                     `json:"birth_day,omitempty" minimum:"1" maximum:"31" doc:"Person-level fact reached through the person link. Present on the detail face; absent when unrecorded or the name has no public person link."`
	Aliases     *[]EntityName            `json:"aliases,omitempty" doc:"Alternate spellings of THIS credited name, not of the person. Present when include=aliases, detail face only. Empty array if none."`
	Photo       *Image                   `json:"photo,omitempty" doc:"Photograph of the linked person. Present only when include=photo and there is a photo; absent otherwise. Detail face only."`
	Siblings    *[]CreditName            `json:"siblings,omitempty" doc:"The same person's other publicly linked names, as basic entries. Present when include=siblings, detail face only. Empty array if none."`
	Intros      *[]Intro                 `json:"intros,omitempty" doc:"Biography of the linked person, one per language. Present when include=intros, detail face only. Empty array if none."`
	Links       *[]WorkLink              `json:"links,omitempty" doc:"External pages for the linked person. Present when include=links, detail face only. Empty array if none."`
	Refs        *[]Ref                   `json:"refs,omitempty" doc:"Exact upstream anchors of this credited name. Present when include=refs, detail face only. Empty array if none."`
}

type Character struct {
	_            struct{}                 `json:"-" additionalProperties:"true"`
	Object       string                   `json:"object" enum:"character" doc:"Type discriminant. Always character."`
	ID           string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog character id."`
	DisplayName  string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Latin        *string                  `json:"latin" maxLength:"512" doc:"null if unrecorded. Must not be used as a discriminant."`
	Lang         *string                  `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47 language tag of display_name. null if unrecorded. Must not be used as a discriminant."`
	Localized    map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	Gender       *string                  `json:"gender,omitempty" enum:"male,female,other" doc:"Present on view=full. null if unrecorded."`
	Birthday     *string                  `json:"birthday,omitempty" pattern:"^[0-1][0-9]-[0-3][0-9]$" maxLength:"5" doc:"MM-DD. Present on view=full. null if unrecorded. Not a date: there is no year."`
	HeightCm     *int                     `json:"height_cm,omitempty" minimum:"0" doc:"Present on view=full. null if unrecorded."`
	WeightKg     *int                     `json:"weight_kg,omitempty" minimum:"0" doc:"Present on view=full. null if unrecorded."`
	Measurements *Measurements            `json:"measurements,omitempty" doc:"Present on view=full. null if unrecorded."`
	BloodType    *string                  `json:"blood_type,omitempty" enum:"a,b,ab,o" doc:"Present on view=full. null if unrecorded."`
	InstanceOfID *string                  `json:"instance_of_id,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Another character this row is an instance of. Present on view=full. null if none."`
	Image        *Image                   `json:"image,omitempty" doc:"Present only when include=image and this character has an image; absent otherwise. Detail face only."`
	Figure       *Image                   `json:"figure,omitempty" doc:"Present only when include=figure and this character has a full-body figure cutout; absent otherwise. Detail face only."`
	Traits       *[]CharacterTrait        `json:"traits,omitempty" doc:"Present when include=traits, detail face only. Empty array if none."`
	Aliases      *[]EntityName            `json:"aliases,omitempty" doc:"Alternate spellings of THIS character name. Present when include=aliases, detail face only. Empty array if none."`
	Intros       *[]Intro                 `json:"intros,omitempty" doc:"Character descriptions, one per language. Present when include=intros, detail face only. Empty array if none."`
	Refs         *[]Ref                   `json:"refs,omitempty" doc:"Exact upstream anchors of this character. Present when include=refs, detail face only. Empty array if none."`
}

type CharacterTrait struct {
	_              struct{}                 `json:"-" additionalProperties:"true"`
	Object         string                   `json:"object" enum:"character_trait" doc:"Type discriminant. Always character_trait."`
	ID             string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog trait id."`
	DisplayName    string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Group          *string                  `json:"group" maxLength:"512" doc:"Root trait group name. null for a root trait. Must not be used as a discriminant."`
	Localized      map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	GroupLocalized map[string]LocalizedText `json:"group_localized" doc:"Localized names of the root group, BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	Spoiler        string                   `json:"spoiler" enum:"none,minor,major" doc:"Spoiler level of this trait on this character."`
	IsSexual       bool                     `json:"is_sexual" doc:"Whether this trait is in the sexual family."`
	IsLie          bool                     `json:"is_lie" doc:"Whether this trait is an in-story deception."`
}

type Measurements struct {
	_       struct{} `json:"-" additionalProperties:"true"`
	BustCm  *int     `json:"bust_cm" minimum:"0" doc:"null if unrecorded."`
	WaistCm *int     `json:"waist_cm" minimum:"0" doc:"null if unrecorded."`
	HipCm   *int     `json:"hip_cm" minimum:"0" doc:"null if unrecorded."`
	Cup     *string  `json:"cup" maxLength:"8" doc:"Must not be used as a discriminant. null if unrecorded."`
}

type Person struct {
	_                   struct{} `json:"-" additionalProperties:"true"`
	Object              string   `json:"object" enum:"person" doc:"Type discriminant. Always person."`
	ID                  string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog person id."`
	DisplayName         string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	PrimaryCreditNameID *string  `json:"primary_credit_name_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Primary credited name. null if unset."`
	Gender              *string  `json:"gender" enum:"male,female,other" doc:"null if unrecorded."`
}

type Trait struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"trait" doc:"Type discriminant. Always trait."`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog trait id."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	NameZh      string   `json:"name_zh" maxLength:"512" doc:"Must not be used as a discriminant. Empty if unrecorded."`
	VndbTID     string   `json:"vndb_tid" maxLength:"32" doc:"Upstream VNDB trait id. Must not be used as a discriminant."`
	IsSexual    bool     `json:"is_sexual" doc:"Whether this trait is in the sexual family."`
}

type NameCredit struct {
	_      struct{}         `json:"-" additionalProperties:"true"`
	Object string           `json:"object" enum:"name_credit" doc:"Type discriminant. Always name_credit."`
	Work   Work             `json:"work" doc:"Work this name is credited on. view=basic."`
	Roles  []NameCreditRole `json:"roles" doc:"Roles on this work. Empty array, never null."`
}

type Appearance struct {
	_          struct{}     `json:"-" additionalProperties:"true"`
	Object     string       `json:"object" enum:"appearance" doc:"Type discriminant. Always appearance."`
	Work       Work         `json:"work" doc:"Work this character appears on. view=basic."`
	RosterRole string       `json:"roster_role" enum:"main,secondary,appears,unknown" doc:"Appearance strength on this work."`
	Spoiler    string       `json:"spoiler" enum:"none,minor,major" doc:"Spoiler level of this appearance."`
	Identity   *string      `json:"identity,omitempty" maxLength:"64" doc:"Opaque roster row identity for a catalog.work.roster.suppressed proposal on THIS work; echo it back, never rebuild it. The same key the mirror-image works/{id}.characters[] row publishes. Absent when the work is reached only through a voice credit, where roster_role is unknown and spoiler is none. Must not be used as a discriminant."`
	Voices     []CreditName `json:"voices" doc:"Voice credits on this appearance. Empty array, never null."`
}

type NameCreditRole struct {
	_             struct{} `json:"-" additionalProperties:"true"`
	RoleKey       string   `json:"role_key" maxLength:"64" doc:"Registry token; joins /v2/catalog/roles. Not always ASCII. Must not be used as a discriminant."`
	RoleName      string   `json:"role_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	CharacterID   *string  `json:"character_id" pattern:"^[0-9]+$" maxLength:"20" doc:"null if this credit is not a voice on a character."`
	CharacterName *string  `json:"character_name" maxLength:"512" doc:"Display name of the voiced character. null unless this credit is a voice on a character. Must not be used as a discriminant."`
}

type Series struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"series" doc:"Type discriminant. Always series."`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog series id."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	WorkCount   int      `json:"work_count" minimum:"0" doc:"Member works visible under the same NSFW gate."`
	HasNSFW     bool     `json:"has_nsfw" doc:"Whether any member work sits behind the r18 display gate. Counted over the same live-claimed members as work_count but NOT narrowed by nsfw=, so a series can report true while work_count hides every such member. Carried on every lane."`
	Intros      *[]Intro `json:"intros,omitempty" doc:"Series descriptions, one per language. Present when include=intros, detail face only. Empty array if none. is_machine is always false here: the store carries no provenance for a series description, so false records unknown, not human-written."`
	Refs        *[]Ref   `json:"refs,omitempty" doc:"Exact upstream anchors of this series. Present when include=refs, detail face only. Empty array if none."`
}

type Engine struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"engine" doc:"Type discriminant. Always engine."`
	ID          string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog engine id."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	WorkCount   int      `json:"work_count" minimum:"0" doc:"Works visible under the same NSFW gate."`
	Description string   `json:"description" maxLength:"8000" doc:"Free-text description. Empty if unrecorded. Must not be used as a discriminant."`
	Aliases     []string `json:"aliases" doc:"Alternate spellings of the engine name. Empty array if none."`
}

type Role struct {
	_           struct{}                 `json:"-" additionalProperties:"true"`
	Object      string                   `json:"object" enum:"role" doc:"Type discriminant. Always role."`
	ID          string                   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog role id."`
	Key         string                   `json:"key" maxLength:"64" doc:"Registry token, unique per role; joins credit groups' role_key. Not always ASCII. Must not be used as a discriminant."`
	Category    string                   `json:"category" maxLength:"64" doc:"Open grouping vocabulary. Empty if uncategorized. Must not be used as a discriminant."`
	DisplayName string                   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	Localized   map[string]LocalizedText `json:"localized" doc:"BCP-47 keys. Empty object if none. Must not be used as a discriminant."`
	Deprecated  bool                     `json:"deprecated" doc:"true when the registry has retired this role for new credits."`
}

type NewsSource struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"news_source" doc:"Type discriminant. Always news_source."`
	Name        string   `json:"name" maxLength:"64" pattern:"^[a-z][a-z0-9_]*$" doc:"Source key."`
	DisplayName string   `json:"display_name" maxLength:"512" doc:"Must not be used as a discriminant."`
	HomepageURL string   `json:"homepage_url" format:"uri" maxLength:"512" doc:"The source's own site. Empty string when the source has none."`
}

type NewsItem struct {
	_           struct{}   `json:"-" additionalProperties:"true"`
	Object      string     `json:"object" enum:"news_item" doc:"Type discriminant. Always news_item."`
	ID          string     `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"News item id."`
	Title       string     `json:"title" maxLength:"512" doc:"Must not be used as a discriminant."`
	Summary     string     `json:"summary" maxLength:"8000" doc:"Source-provided lede. Must not be used as a discriminant."`
	Source      NewsSource `json:"source" doc:"Attribution. Required on view=basic."`
	SourceURL   string     `json:"source_url" format:"uri" maxLength:"1024" doc:"Canonical link to the original item."`
	Banner      *Image     `json:"banner" doc:"Lead image. null when the item has none."`
	PublishedAt string     `json:"published_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type CatalogStats struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Object      string   `json:"object" enum:"catalog_stats" doc:"Type discriminant. Always catalog_stats."`
	Works       int64    `json:"works" minimum:"0" doc:"Live works."`
	Labels      int64    `json:"companies" minimum:"0" doc:"Company registry rows."`
	Characters  int64    `json:"characters" minimum:"0" doc:"Live characters."`
	CreditNames int64    `json:"credit_names" minimum:"0" doc:"Live credit names."`
	Persons     int64    `json:"persons" minimum:"0" doc:"Live persons."`
}
