package repr

type MergePreview struct {
	_              struct{}              `json:"-" additionalProperties:"true"`
	Object         string                `json:"object" enum:"merge_preview" doc:"Type discriminant. Always merge_preview."`
	TargetObject   string                `json:"target_object" enum:"work,company,character,person,credit_name" doc:"Family of the two entries."`
	Source         MergePreviewSide      `json:"source" doc:"The entry that would be folded away."`
	Target         MergePreviewSide      `json:"target" doc:"The entry that would survive."`
	Blockers       []MergePreviewIssue   `json:"blockers" doc:"Reasons the merge cannot be filed or run as it stands. Empty array when nothing blocks it."`
	Warnings       []MergePreviewIssue   `json:"warnings" doc:"Consequences a reviewer should read before deciding, such as a source anchor being demoted. Empty array, never null."`
	Facts          []string              `json:"facts" doc:"Plain sentences stating evidence found in the data (shared releases, VNDB coverage, identical titles), offered as a starting point for the reason. Must not be used as a discriminant."`
	Fields         []MergePreviewField   `json:"fields" doc:"Fields whose surviving value can be chosen, with both sides' values."`
	Anchors        []MergePreviewAnchor  `json:"anchors" doc:"Every external id on either side and where it ends up."`
	Releases       []MergePreviewRelease `json:"releases" doc:"Live releases of both works (works only), with source releases that repeat a target release marked. Empty array for other families."`
	Titles         []MergePreviewTitle   `json:"titles" doc:"Titles of both works and the fate of each source title (works only). Empty array for other families."`
	Intros         []MergePreviewIntro   `json:"intros" doc:"Per language, which intro survives (works only). Empty array for other families."`
	Counts         []MergePreviewCount   `json:"counts" doc:"Rows on each side by kind, and how many of the source's already exist on the target."`
	Claim          *MergePreviewClaim    `json:"claim" doc:"Site claims on the two works and which one survives (works only). null for other families."`
	OpenRequestID  *string               `json:"open_request_id" pattern:"^[0-9]+$" maxLength:"20" doc:"An open request for this same pair, when there is one; filing another is refused 409."`
}

type MergePreviewSide struct {
	_    struct{} `json:"-" additionalProperties:"true"`
	ID   string   `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Catalog id."`
	Name string   `json:"name" maxLength:"512" doc:"Display name. Must not be used as a discriminant."`
	Live bool     `json:"live" doc:"false when the entry is already merged away or deleted."`
	Site *string  `json:"site" maxLength:"64" pattern:"^[a-z][a-z0-9_-]*$" doc:"Site that claims the work, null when unclaimed or for other families."`
}

type MergePreviewIssue struct {
	_       struct{} `json:"-" additionalProperties:"true"`
	Code    string   `json:"code" maxLength:"64" pattern:"^[a-z_]+$" doc:"Machine-readable kind: same_entity, not_live, cross_medium, hidden_survivor, conflicting_anchor, claim_dropped. Open vocabulary."`
	Message string   `json:"message" maxLength:"1000" doc:"Explanation for people, in Chinese. Must not be used as a discriminant."`
}

type MergePreviewField struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Key            string   `json:"key" maxLength:"64" pattern:"^[a-z_]+$" doc:"Field name as field_resolution takes it."`
	SourceValue    any      `json:"source_value" doc:"The source's value; null when empty."`
	TargetValue    any      `json:"target_value" doc:"The target's value; null when empty."`
	Default        string   `json:"default" enum:"source,target" doc:"What survives with no choice made: the target's value, or the source's when the target has none."`
	HumanProtected bool     `json:"human_protected" doc:"true when a person entered the target's value and the source's is a machine value: choosing source has no effect."`
}

type MergePreviewAnchor struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Side       string   `json:"side" enum:"source,target" doc:"Which entry holds the id now."`
	SourceKey  string   `json:"source_key" maxLength:"64" doc:"Registry key (vndb, bangumi, erogamescape, …). Must not be used as a discriminant."`
	ExternalID string   `json:"external_id" maxLength:"256" doc:"Verbatim upstream id. Must not be used as a discriminant beyond exact match."`
	KindBefore string   `json:"kind_before" enum:"identity,probable,related" doc:"Link kind now."`
	KindAfter  string   `json:"kind_after" enum:"identity,probable,related" doc:"Link kind after the merge."`
	Fate       string   `json:"fate" enum:"moves,stays,demoted_related,demoted_probable,duplicate" doc:"stays: a target id, unchanged. moves: carried to the target as it is. demoted_*: carried over at a weaker kind because the target already holds a different id from that source. duplicate: the target holds the same id; the source copy is dropped."`
	Reason     string   `json:"reason" maxLength:"500" doc:"Why, for a demotion or duplicate. Empty otherwise. Must not be used as a discriminant."`
}

type MergePreviewRelease struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Side        string   `json:"side" enum:"source,target" doc:"Which work the release belongs to now."`
	ID          string   `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Release id."`
	Title       string   `json:"title" maxLength:"512" doc:"Release title; empty when unrecorded. Must not be used as a discriminant."`
	ReleaseKind          string  `json:"release_kind" enum:"default,digital,physical,trial,patch" doc:"Release class."`
	Platform             string  `json:"platform" maxLength:"64" doc:"Empty when unrecorded. Must not be used as a discriminant."`
	Lang                 string  `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47. Empty if unrecorded."`
	ReleaseDate          *string `json:"release_date" format:"date" maxLength:"10" doc:"Calendar date. null if undated."`
	ReleaseDatePrecision *string `json:"release_date_precision" enum:"day,month,year" doc:"null when release_date is null. month dates sit on the 1st; year dates sit on January 1."`
	VNDBRid              *string `json:"vndb_rid" pattern:"^r[0-9]+$" maxLength:"16" doc:"VNDB release id. null when none."`
	DuplicateOf          *string `json:"duplicate_of" pattern:"^[0-9]+$" maxLength:"20" doc:"For a source release, the target release it repeats; null when none."`
	DuplicateBy          *string `json:"duplicate_by" enum:"vndb,platform_date" doc:"How duplicate_of was matched: vndb (same VNDB release) or platform_date (same platform and full date, compatible release class). null when not a duplicate."`
}

type MergePreviewTitle struct {
	_     struct{} `json:"-" additionalProperties:"true"`
	Side  string   `json:"side" enum:"source,target" doc:"Which work holds the title now."`
	Title string   `json:"title" maxLength:"1024" doc:"The title. Must not be used as a discriminant."`
	Lang      string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47. Empty for a language-less alias."`
	TitleKind string   `json:"title_kind" enum:"official,alias,abbreviation" doc:"search_hint is internal and never appears on this type."`
	Fate  string   `json:"fate" enum:"stays,moves,becomes_alias,duplicate,dropped" doc:"For a source title: moves as it is; becomes_alias when the target already shows a title in that language; duplicate when the target has it; dropped for a machine translation in a taken language."`
}

type MergePreviewIntro struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Lang       string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47."`
	SourceHas  bool     `json:"source_has" doc:"The source has an intro in this language."`
	TargetHas  bool     `json:"target_has" doc:"The target has an intro in this language."`
	SourceText string   `json:"source_text" maxLength:"800" doc:"Start of the source's intro. Must not be used as a discriminant."`
	TargetText string   `json:"target_text" maxLength:"800" doc:"Start of the target's intro. Must not be used as a discriminant."`
	Fate       string   `json:"fate" enum:"stays,moves,kept_out,replaces_machine" doc:"moves: the target has none; kept_out: the target's own text stays and the source's is archived; replaces_machine: the source's source text replaces the target's machine translation."`
}

type MergePreviewCount struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Key             string   `json:"key" maxLength:"128" pattern:"^[a-z_.]+$" doc:"What is counted: tags, credits, characters, companies, relations, series, covers, screenshots, platforms, user_ratings, user_folders, user_states, user_playtime for works; table.column for other families. Open vocabulary."`
	OnSource        int64    `json:"on_source" minimum:"0" doc:"Rows on the source."`
	OnTarget        int64    `json:"on_target" minimum:"0" doc:"Rows on the target."`
	AlreadyOnTarget int64    `json:"already_on_target" minimum:"0" doc:"Of the source's rows, how many the target already has; they fold into one."`
}

type MergePreviewClaim struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	SourceSite *string  `json:"source_site" maxLength:"64" pattern:"^[a-z][a-z0-9_-]*$" doc:"Site claiming the source; null when unclaimed."`
	TargetSite *string  `json:"target_site" maxLength:"64" pattern:"^[a-z][a-z0-9_-]*$" doc:"Site claiming the target; null when unclaimed."`
	Fate       string   `json:"fate" enum:"none,moves,dropped" doc:"moves: the target is unclaimed and takes the source's claim; dropped: both are claimed and the target's stays; none: the source is unclaimed."`
}
