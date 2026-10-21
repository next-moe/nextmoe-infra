package repr

type DeletionRequestRecord struct {
	_             struct{} `json:"-" additionalProperties:"true"`
	Object        string   `json:"object" enum:"deletion_request" doc:"Type discriminant. Always deletion_request."`
	ID            string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Deletion request id."`
	State         string   `json:"state" enum:"open,approved,executed,rejected,withdrawn,restored" doc:"open awaits a reviewer; approved waits out the cooling-off (catalog.deletion_cooling_off_hours), after which the deletion runs; executed means the entry is deleted; restored means a reviewer brought it back under the same id."`
	TargetObject  string   `json:"target_object" enum:"work" doc:"Family of the entry. Only works can be deleted today."`
	TargetID      string   `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry to delete."`
	Reason        string   `json:"reason" enum:"not_a_work,out_of_scope,spam_or_test,other" doc:"not_a_work: no such work exists; out_of_scope: it exists but the catalog does not collect it; spam_or_test: vandalism or a test entry; other: see note. A duplicate is merged, not deleted."`
	Note          string   `json:"note" maxLength:"2000" doc:"The requester's explanation. Must not be used as a discriminant."`
	DecisionNote  string   `json:"decision_note" maxLength:"2000" doc:"The reviewer's reason for a rejection, or why the request closed by itself. Empty otherwise. Must not be used as a discriminant."`
	RestoreNote   string   `json:"restore_note" maxLength:"2000" doc:"The reviewer's note when restoring. Empty otherwise. Must not be used as a discriminant."`
	ProposedByUID string   `json:"proposed_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the requester."`
	ProposedBy    *UserRef `json:"proposed_by" doc:"Account behind proposed_by_uid. null only when the account directory could not be read."`
	DecidedByUID  *string  `json:"decided_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who approved or rejected it. null before a decision and when the request closed by itself."`
	DecidedBy     *UserRef `json:"decided_by" doc:"Account behind decided_by_uid."`
	RestoredByUID *string  `json:"restored_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who restored the entry. null unless restored."`
	RestoredBy    *UserRef `json:"restored_by" doc:"Account behind restored_by_uid."`
	OriginSite    *string  `json:"origin_site" maxLength:"32" doc:"Site the requester came from. null when not recorded. Must not be used as a discriminant."`
	ProposedAt    string   `json:"proposed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	DecidedAt     *string  `json:"decided_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null before a decision."`
	ExecuteAfter  *string  `json:"execute_after" format:"date-time" maxLength:"32" doc:"End of the cooling-off once approved. null before approval."`
	ExecutedAt    *string  `json:"executed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null until executed."`
	RestoredAt    *string  `json:"restored_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null unless restored."`
}

type DeletionAnchor struct {
	_          struct{} `json:"-" additionalProperties:"true"`
	Source     string   `json:"source" maxLength:"32" doc:"Source key, e.g. vndb, bangumi, erogamescape."`
	ExternalID string   `json:"external_id" maxLength:"256" doc:"The entry's id at that source."`
	Kind       string   `json:"kind" enum:"identity,probable,related" doc:"identity and probable links keep the importers from creating the work again while it is deleted; related links are ordinary links."`
}

type DeletionUserData struct {
	_           struct{} `json:"-" additionalProperties:"true"`
	Ratings     int64    `json:"ratings" minimum:"0" doc:"Ratings on the work. Kept but hidden while it is deleted."`
	FolderItems int64    `json:"folder_items" minimum:"0" doc:"Folder memberships of the work. Kept but hidden while it is deleted."`
	Playtimes   int64    `json:"playtimes" minimum:"0" doc:"Playtime reports on the work. Kept but hidden while it is deleted."`
	WorkStates  int64    `json:"work_states" minimum:"0" doc:"Play states on the work. Kept but hidden while it is deleted."`
}

type DeletionPreview struct {
	_                struct{}         `json:"-" additionalProperties:"true"`
	Object           string           `json:"object" enum:"deletion_preview" doc:"Type discriminant. Always deletion_preview."`
	TargetObject     string           `json:"target_object" enum:"work" doc:"Family of the entry."`
	TargetID         string           `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry."`
	DisplayName      string           `json:"display_name" maxLength:"512" doc:"The entry's display name. Must not be used as a discriminant."`
	Deleted          bool             `json:"deleted" doc:"true when the entry is already deleted."`
	MergedIntoID     *string          `json:"merged_into_id" pattern:"^[0-9]+$" maxLength:"20" doc:"Set when the entry was merged into another; it cannot be deleted then."`
	Anchors          []DeletionAnchor `json:"anchors" doc:"Links to source entries the work holds."`
	Releases         int64            `json:"releases" minimum:"0" doc:"Releases of the work."`
	UserData         DeletionUserData `json:"user_data" doc:"User records keyed to the work."`
	OpenProposals    int64            `json:"open_proposals" minimum:"0" doc:"Open edit proposals on the work. A deletion declines them."`
	ClaimSite        *string          `json:"claim_site" maxLength:"32" doc:"Site that claimed the work. null when unclaimed."`
	PendingRequestID *string          `json:"pending_request_id" pattern:"^[0-9]+$" maxLength:"20" doc:"An open or approved deletion request on the entry. null when none."`
}
