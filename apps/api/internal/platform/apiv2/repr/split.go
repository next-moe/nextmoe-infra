package repr

type SplitTitle struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Lang      string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47, or empty for a title with no language."`
	Title     string   `json:"title" minLength:"1" maxLength:"512" doc:"Must not be used as a discriminant."`
	TitleKind string   `json:"title_kind" enum:"official,alias,abbreviation" doc:"How the new work holds the title."`
}

type SplitRef struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Source     string   `json:"source" enum:"bangumi,erogamescape" doc:"A registry that lists editions as separate entries."`
	ExternalID string   `json:"external_id" minLength:"1" maxLength:"64" doc:"The entry's id in that registry. Must not be used as a discriminant beyond exact match."`
}

type SplitMovedCount struct {
	_     struct{} `json:"-" additionalProperties:"true"`
	Key   string   `json:"key" enum:"releases,tombstones,titles,credits,refs,rejections" doc:"releases: releases moved to the new work; tombstones: retired rows left on the original so the VNDB import does not mint the moved releases there again; titles: titles created on the new work; credits: credits limited to a moved release, moved with it; refs: source entries promoted to the new work's identity; rejections: records that keep those entries from being matched back to the original."`
	Count int64    `json:"count" minimum:"0" doc:"Rows."`
}

type SplitRequestRecord struct {
	_             struct{}          `json:"-" additionalProperties:"true"`
	Object        string            `json:"object" enum:"split_request" doc:"Type discriminant. Always split_request."`
	ID            string            `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Split request id."`
	State         string            `json:"state" enum:"open,approved,executed,rejected,withdrawn,undone" doc:"open awaits a reviewer; approved waits out the cooling-off; executed created the new work; rejected and withdrawn are closed; undone was executed and then put back."`
	TargetObject  string            `json:"target_object" enum:"work" doc:"Family of the entry. Only works can be split today."`
	TargetID      string            `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work the releases are split off."`
	TargetName    string            `json:"target_name" maxLength:"512" doc:"Its display name. Must not be used as a discriminant."`
	NewWorkID     *string           `json:"new_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work the split created. null until executed; after an undo it redirects to target_id."`
	NewWorkName   string            `json:"new_work_name" maxLength:"512" doc:"The created work's display name, empty until executed. Must not be used as a discriminant."`
	ReleaseIDs    []string          `json:"release_ids" doc:"Releases that move to the new work."`
	DisplayName   string            `json:"display_name" maxLength:"512" doc:"The new work's display name. Must not be used as a discriminant."`
	OLang         string            `json:"olang" maxLength:"16" format:"bcp47" doc:"The new work's original language, BCP-47."`
	Titles        []SplitTitle      `json:"titles" doc:"Titles the new work starts with."`
	PromoteRefs   []SplitRef        `json:"promote_refs" doc:"Source entries that become the new work's identity."`
	Note          string            `json:"note" maxLength:"2000" doc:"The requester's reason. Must not be used as a discriminant."`
	DecisionNote  string            `json:"decision_note" maxLength:"2000" doc:"The reviewer's reason for a rejection, or why the request closed by itself. Must not be used as a discriminant."`
	UndoNote      string            `json:"undo_note" maxLength:"2000" doc:"The reviewer's note when undoing. Must not be used as a discriminant."`
	ProposedByUID string            `json:"proposed_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the requester."`
	ProposedBy    *UserRef          `json:"proposed_by" doc:"Account behind proposed_by_uid. null only when the account directory could not be read."`
	AmendedByUID  *string           `json:"amended_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who last changed the selection. null when never amended."`
	AmendedBy     *UserRef          `json:"amended_by" doc:"Account behind amended_by_uid."`
	DecidedByUID  *string           `json:"decided_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who approved or rejected."`
	DecidedBy     *UserRef          `json:"decided_by" doc:"Account behind decided_by_uid."`
	UndoneByUID   *string           `json:"undone_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who undid the split."`
	UndoneBy      *UserRef          `json:"undone_by" doc:"Account behind undone_by_uid."`
	OriginSite    *string           `json:"origin_site" enum:"kungal,moyu,letmoe,nextmoe,editor" doc:"Property the edit was made from: kungal (the KUN Visual Novel forum), moyu, letmoe, nextmoe or editor (the NextMoe editor itself). null on rows recorded before attribution existed."`
	ProposedAt    string            `json:"proposed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	AmendedAt     *string           `json:"amended_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null when never amended."`
	DecidedAt     *string           `json:"decided_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null before a decision."`
	ExecuteAfter  *string           `json:"execute_after" format:"date-time" maxLength:"32" doc:"End of the cooling-off once approved. null before approval."`
	ExecutedAt    *string           `json:"executed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null until executed."`
	UndoneAt      *string           `json:"undone_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null unless undone."`
	UndoUntil     *string           `json:"undo_until" format:"date-time" maxLength:"32" doc:"Last moment a reviewer can undo the split. null unless executed."`
	UndoMergeID   *string           `json:"undo_merge_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The merge that folded the new work back into target_id on undo. null unless undone."`
	Moved         []SplitMovedCount `json:"moved" doc:"What the execution created or moved. Empty before execution."`
}

type SplitPreviewRelease struct {
	_                    struct{} `json:"-" additionalProperties:"true"`
	ID                   string   `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Release id."`
	Title                string   `json:"title" maxLength:"512" doc:"Release title; empty when unrecorded. Must not be used as a discriminant."`
	ReleaseKind          string   `json:"release_kind" enum:"default,digital,physical,trial,patch" doc:"Release class."`
	Platform             string   `json:"platform" maxLength:"64" doc:"Empty when unrecorded. Must not be used as a discriminant."`
	Lang                 string   `json:"lang" maxLength:"32" format:"bcp47" doc:"BCP-47. Empty if unrecorded."`
	ReleaseDate          *string  `json:"release_date" format:"date" maxLength:"10" doc:"Calendar date. null if undated."`
	ReleaseDatePrecision *string  `json:"release_date_precision" enum:"day,month,year" doc:"null when release_date is null."`
	VNDBRid              *string  `json:"vndb_rid" pattern:"^r[0-9]+$" maxLength:"16" doc:"VNDB release id. null when none."`
	Sources              []string `json:"sources" doc:"Registries that hold this release, e.g. vndb, dlsite, getchu. Open vocabulary."`
	LabelCount           int64    `json:"label_count" minimum:"0" doc:"Companies credited on the release; they move with it."`
	CreditCount          int64    `json:"credit_count" minimum:"0" doc:"Credits limited to this release; they move with it."`
	Selected             bool     `json:"selected" doc:"Whether release_ids= chose it."`
	Tombstone            bool     `json:"tombstone" doc:"Whether moving it leaves a retired row on the original for the VNDB import."`
}

type SplitPreviewRef struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Source     string   `json:"source" enum:"bangumi,erogamescape" doc:"Registry."`
	ExternalID string   `json:"external_id" maxLength:"64" doc:"Entry id. Must not be used as a discriminant beyond exact match."`
	AnchorKind string   `json:"anchor_kind" enum:"probable,related" doc:"How it is linked to the original today."`
	HeldByID   *string  `json:"held_by_id" pattern:"^[0-9]+$" maxLength:"20" doc:"A work that already holds it as its identity; it cannot be promoted then. null when free."`
	Selected   bool     `json:"selected" doc:"Whether promote= chose it."`
}

type SplitPreviewCount struct {
	_     struct{} `json:"-" additionalProperties:"true"`
	Key   string   `json:"key" enum:"releases,release_labels,release_refs,credits,tombstones,characters,tags,titles" doc:"What is counted."`
	Count int64    `json:"count" minimum:"0" doc:"Rows."`
}

type SplitPreview struct {
	_             struct{}              `json:"-" additionalProperties:"true"`
	Object        string                `json:"object" enum:"split_preview" doc:"Type discriminant. Always split_preview."`
	TargetObject  string                `json:"target_object" enum:"work" doc:"Family of the entry."`
	TargetID      string                `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work."`
	TargetName    string                `json:"target_name" maxLength:"512" doc:"Its display name. Must not be used as a discriminant."`
	OLang         string                `json:"olang" maxLength:"16" format:"bcp47" doc:"Its original language."`
	VNDBAnchor    *string               `json:"vndb_anchor" pattern:"^v[0-9]+$" maxLength:"16" doc:"The VNDB VN the work is anchored to. null when none."`
	Releases      []SplitPreviewRelease `json:"releases" doc:"Every live release of the work, oldest first."`
	Candidates    []SplitPreviewRef     `json:"candidates" doc:"Separate registry entries linked to the work that could become the new work's identity."`
	SuggestedName string                `json:"suggested_name" maxLength:"512" doc:"A display name derived from the chosen releases' titles. Empty when nothing is chosen. Must not be used as a discriminant."`
	SuggestedLang string                `json:"suggested_lang" maxLength:"16" format:"bcp47" doc:"The original language to start from."`
	Titles        []SplitTitle          `json:"titles" doc:"Titles derived from the chosen releases."`
	Moves         []SplitPreviewCount   `json:"moves" doc:"What goes to the new work."`
	Stays         []SplitPreviewCount   `json:"stays" doc:"What stays with the original."`
	Blockers      []MergePreviewIssue   `json:"blockers" doc:"Reasons the split cannot be filed as chosen."`
	Warnings      []MergePreviewIssue   `json:"warnings" doc:"Consequences to read before filing."`
	Facts         []MergePreviewIssue   `json:"facts" doc:"What the registries say about the chosen releases, for the reviewer to weigh."`
	PendingID     *string               `json:"pending_request_id" pattern:"^[0-9]+$" maxLength:"20" doc:"An open or approved split request on the work. null when none."`
}

type SplitEvent struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Object     string   `json:"object" enum:"split" doc:"Type discriminant. Always split."`
	ID         string   `json:"id" pattern:"^[0-9]+$" maxLength:"20" doc:"Split request id."`
	State      string   `json:"state" enum:"executed,undone" doc:"executed: new_work_id now holds release_ids. undone: they are back on original_id and new_work_id redirects to it (see /v2/catalog/redirects)."`
	OriginalID string   `json:"original_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work the releases left."`
	NewWorkID  string   `json:"new_work_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The work the split created."`
	ReleaseIDs []string `json:"release_ids" doc:"Releases that moved."`
	ChangedAt  string   `json:"changed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. When the split was executed or undone, whichever is later; the cursor resumes from it, so an undo re-surfaces the split at the tail."`
}
