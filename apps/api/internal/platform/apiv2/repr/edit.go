package repr

type Revision struct {
	_             struct{}     `json:"-" additionalProperties:"true"`
	Object        string       `json:"object" enum:"revision" doc:"Type discriminant. Always revision."`
	ID            string       `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Revision id. This is the value POST /v2/moderation/reverts takes as revision_id."`
	TargetObject  string       `json:"target_object" enum:"work,company,character,release,tag,engine,series" doc:"Family of the entity this revision is on."`
	EntityID      string       `json:"entity_id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Catalog id of the entity this revision is on."`
	SiteWorkID    *string      `json:"site_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The claiming site's own work id, which on kungal, moyu and letmoe is the catalog id. null unless target_object is work and the claiming site equals site."`
	Seq           int          `json:"seq" minimum:"1" doc:"Position in this entity's revision chain, 1-based and contiguous."`
	Action        string       `json:"action" enum:"created,merged,direct,reverted" doc:"How this revision came about."`
	ChangedFields []string     `json:"changed_fields" doc:"Field keys this revision touched. Empty array, never null."`
	ActorUID      string       `json:"actor_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The claiming site's own user id of whoever caused this revision. Not a catalog id."`
	AmenderUID    *string      `json:"amender_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"The claiming site's own user id of the last amender. null when nobody amended."`
	ProposalID    *string      `json:"proposal_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Proposal this revision merged. null for direct edits and imports."`
	Site          string       `json:"site" maxLength:"64" doc:"Tenant this revision was recorded under. Open vocabulary; must not be used as a discriminant."`
	CreatedAt     string       `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	Diff          *[]FieldDiff `json:"diff,omitempty" doc:"Present when include=diff. Field-level change set against diff_base, or against the preceding revision when diff_base is absent. Empty array if nothing differs."`
	DiffBase      *string      `json:"diff_base,omitempty" pattern:"^[0-9]+$" maxLength:"20" doc:"Present when include=diff. Revision the diff was taken against. null when this is the first revision of the entity."`
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
	AmenderUID string   `json:"amender_uid" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"The claiming site's own user id of the amender. Not a catalog id."`
	Note       string   `json:"note" maxLength:"2000" doc:"Amender's own summary. Must not be used as a discriminant."`
	CreatedAt  string   `json:"created_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
}

type EditImage struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Object         string   `json:"object" enum:"edit_image" doc:"Type discriminant. Always edit_image."`
	Preset         string   `json:"preset" enum:"cover,screenshot" doc:"Which editor slot these bytes were sized for."`
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
