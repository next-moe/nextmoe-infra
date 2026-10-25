package repr

type Revision struct {
	_             struct{}     `json:"-" additionalProperties:"true"`
	Object        string       `json:"object" enum:"revision" doc:"Type discriminant. Always revision."`
	ID            string       `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Revision id. This is the value POST /v2/moderation/reverts takes as revision_id."`
	TargetObject  string       `json:"target_object" enum:"work,company,character,release,tag,engine,series,person,credit_name,trait,walkthrough" doc:"Family of the entity this revision is on."`
	EntityID      string       `json:"entity_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog id of the entity this revision is on."`
	SiteWorkID    *string      `json:"site_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The claiming site's own work id, which on kungal, moyu and letmoe is the catalog id. null unless target_object is work and the claiming site equals site."`
	Seq           int          `json:"seq" minimum:"1" doc:"Position in this entity's revision chain, 1-based and contiguous."`
	Action        string       `json:"action" enum:"created,merged,direct,reverted" doc:"How this revision came about."`
	ChangedFields []string     `json:"changed_fields" doc:"Field keys this revision touched. Empty array, never null."`
	ActorUID      string       `json:"actor_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Central account id of whoever caused this revision, the same id on every first-party site. Not a catalog id."`
	AmenderUID    *string      `json:"amender_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the last amender. null when nobody amended."`
	Actor         *UserRef     `json:"actor" doc:"Account behind actor_uid. null only when the account directory could not be read."`
	Amender       *UserRef     `json:"amender" doc:"Account behind amender_uid. null when nobody amended, or when the account directory could not be read."`
	ProposalID    *string      `json:"proposal_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Proposal this revision merged. null for direct edits and imports."`
	Site          string       `json:"site" maxLength:"64" doc:"Tenant this revision was recorded under. Open vocabulary; must not be used as a discriminant."`
	OriginSite    *string      `json:"origin_site" enum:"kungal,moyu,letmoe,nextmoe,editor" doc:"Property the edit was made from: kungal (the KUN forum), moyu, letmoe, nextmoe, or editor when the editor was opened directly. Attribution only, never a permission input. null for records older than origin tracking (2026-10-21) and for batch tools. GET /v2/vocabularies/origin_site gives display names and each property's user profile URL template."`
	CreatedAt     string       `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	Diff          *[]FieldDiff `json:"diff,omitempty" doc:"Present when include=diff. Field-level change set against diff_base, or against the preceding revision when diff_base is absent. Empty array if nothing differs."`
	DiffBase      *string      `json:"diff_base,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Present when include=diff. Revision the diff was taken against. null when this is the first revision of the entity."`
	HasBaseline   *bool        `json:"has_baseline,omitempty" doc:"true on an entity's first revision when the state before that edit was recorded. diff_base=baseline diffs against it, and POST /v2/moderation/reverts with baseline=true restores it. Absent otherwise."`
	DiffFromBase  *bool        `json:"diff_from_baseline,omitempty" doc:"Present with diff when the diff was taken against the state recorded before the entity's first edit — asked for with diff_base=baseline, or taken by default on a first revision that has one. diff_base is then null."`
}

// field_type and diff_hint are deliberately not repeated here: they belong to
// the field, not to one change of it, and GET /v2/catalog/schemas/{object}
// already publishes both keyed by this same key.
type FieldDiff struct {
	_    struct{} `json:"-" additionalProperties:"true"`
	Key  string   `json:"key" maxLength:"128" pattern:"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" doc:"Editing-engine field key. Must not be used as a discriminant."`
	From any      `json:"from" doc:"Value before. null when the field was unset or the revision has no predecessor."`
	To   any      `json:"to" doc:"Value after. null when the field was cleared."`
}

type Amendment struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Object     string   `json:"object" enum:"amendment" doc:"Type discriminant. Always amendment."`
	ID         string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Amendment id."`
	Seq        int      `json:"seq" minimum:"1" doc:"Position in this proposal's amendment chain."`
	AmenderUID string   `json:"amender_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Central account id of the amender. Not a catalog id."`
	Amender    *UserRef `json:"amender" doc:"Account behind amender_uid. null only when the account directory could not be read."`
	Note       string   `json:"note" maxLength:"2000" doc:"Amender's own summary. Must not be used as a discriminant."`
	CreatedAt  string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type EditImage struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Object         string   `json:"object" enum:"edit_image" doc:"Type discriminant. Always edit_image."`
	Preset         string   `json:"preset" enum:"cover,screenshot,photo" doc:"Which editor slot these bytes were sized for."`
	URL            string   `json:"url" format:"uri" maxLength:"512" doc:"Absolute image URL. Never a bare hash."`
	Hash           string   `json:"hash" minLength:"64" maxLength:"64" pattern:"^[0-9a-f]{64}$" doc:"Image-service content hash. This is the value an edit proposal carries."`
	Width          *int     `json:"width" minimum:"0" maximum:"65535" doc:"Pixel width. null if unknown."`
	Height         *int     `json:"height" minimum:"0" maximum:"65535" doc:"Pixel height. null if unknown."`
	Thumbhash      *string  `json:"thumbhash" maxLength:"128" pattern:"^[A-Za-z0-9+/=_-]+$" doc:"Thumbhash. null if unknown."`
	Sexual         *string  `json:"sexual" enum:"safe,suggestive,explicit" doc:"Sexual depiction. null means not assessed. An upload receipt is always null; the assessment lands later."`
	Violence       *string  `json:"violence" enum:"tame,violent,brutal" doc:"Violent depiction. null means not assessed. Currently always null."`
	SizeBytes      int64    `json:"size_bytes" minimum:"0" doc:"Stored byte length after the image service re-encoded the upload."`
	IsDeduplicated bool     `json:"is_deduplicated" doc:"True when these bytes already existed and no new object was stored."`
}
