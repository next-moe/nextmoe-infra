package repr

// EditCapabilities answers "what may I do to this object", which is a different
// question from "what shape is this object" and therefore a different URL:
// B34 blacklists varying one URL's field set by credential, so the capability
// axis cannot ride on /v2/catalog/schemas/{object}, which is credential-less.
// Join the two on `key`; nothing the public schema already publishes is
// repeated here.
type EditCapabilities struct {
	_              struct{}          `json:"-" additionalProperties:"true"`
	Object         string            `json:"object" enum:"edit_capabilities" doc:"Type discriminant. Always edit_capabilities."`
	TargetObject   string            `json:"target_object" enum:"work,company,character,release,tag,engine,series" doc:"Family these capabilities describe."`
	EntityType     string            `json:"entity_type" enum:"catalog.work,catalog.label,catalog.character,catalog.release,catalog.tag,catalog.engine,catalog.series" doc:"Editing-engine type this family writes as."`
	EntityID       *string           `json:"entity_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entity these capabilities were evaluated against. null when the answer is type-level, which cannot express the owner channel."`
	QuotaTier      string            `json:"quota_tier" enum:"user,editor,reviewer" doc:"Quota tier of the bearer: reviewer holds a review permission on some edit family, editor holds catalog.edit.trusted without one, user is everyone else. GET /v2/me/edit-quota has the counters."`
	QuotaExhausted bool              `json:"quota_exhausted" doc:"true when the daily proposal limit or the open-proposal cap of the bearer's tier is used up; every can_propose and would_automerge below is then false."`
	Central        bool              `json:"central" doc:"true when the token's client is the central editor, which sees, decides and reverts proposals filed on any catalog site."`
	Fields         []FieldCapability `json:"fields" doc:"One entry per editable field, in the schema's order. Empty array, never null."`
}

type FieldCapability struct {
	_              struct{} `json:"-" additionalProperties:"true"`
	Key            string   `json:"key" maxLength:"128" pattern:"^[a-z0-9_]+(\\.[a-z0-9_]+)+$" doc:"Editing-engine field key. Joins /v2/catalog/schemas/{object} fields[].key. Must not be used as a discriminant."`
	Locked         bool     `json:"locked" doc:"true when the editing policy accepts no proposal of this field from anyone."`
	CanPropose     bool     `json:"can_propose" doc:"true when the bearer may file a proposal touching this field now: the policy allows it and the bearer's quota is not exhausted."`
	CanReview      bool     `json:"can_review" doc:"true when the bearer may decide a proposal touching this field. Always false for a developer-owned app, whatever roles the user carries."`
	WouldAutomerge bool     `json:"would_automerge" doc:"true when a proposal of this field by this bearer would apply immediately rather than queue. Requires entity_id for the owner channel. false for an editor whose daily direct-edit allowance is used up: the edit queues instead."`
}

type EditQuota struct {
	_             struct{}     `json:"-" additionalProperties:"true"`
	Object        string       `json:"object" enum:"edit_quota" doc:"Type discriminant. Always edit_quota."`
	QuotaTier     string       `json:"quota_tier" enum:"user,editor,reviewer" doc:"Quota tier of the bearer: reviewer holds a review permission on some edit family, editor holds catalog.edit.trusted without one, user is everyone else."`
	WindowHours   int          `json:"window_hours" minimum:"1" doc:"Length of the sliding window the daily counters are counted over."`
	Proposals     QuotaCounter `json:"proposals" doc:"Proposals filed inside the window, including ones that applied immediately. Exhausted means POST /v2/me/proposals answers 429 QUOTA_EXCEEDED."`
	DirectEdits   QuotaCounter `json:"direct_edits" doc:"Edits that applied without review inside the window. A user has none (limit 0). Once an editor's are used up, their further edits queue as ordinary proposals instead of failing. A reviewer's are unlimited (limit null)."`
	OpenProposals QuotaCounter `json:"open_proposals" doc:"Proposals of the bearer still awaiting a decision, on every site. Not windowed. Exhausted means POST /v2/me/proposals answers 429 QUOTA_EXCEEDED until one is decided or withdrawn."`
}

type QuotaCounter struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Limit     *int64   `json:"limit" minimum:"0" doc:"null when this tier has no limit on the counter."`
	Used      int64    `json:"used" minimum:"0" doc:"How much of the counter the bearer has used."`
	Remaining *int64   `json:"remaining" minimum:"0" doc:"limit minus used, floored at 0. null when limit is null."`
}
