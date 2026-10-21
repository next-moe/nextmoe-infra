package repr

type ProposalCreation struct {
	_      struct{}       `json:"-" additionalProperties:"true"`
	Params map[string]any `json:"params" doc:"The creation parameters the proposal was filed with (e.g. work_id for a release). Empty object when the family takes none."`
}

type CreateCapability struct {
	_              struct{}      `json:"-" additionalProperties:"true"`
	CanCreate      bool          `json:"can_create" doc:"true when the bearer may file a creation of this family now: the create policy allows it and the bearer's quota is not exhausted."`
	WouldAutomerge bool          `json:"would_automerge" doc:"true when such a creation would apply at once instead of queueing for review."`
	ParamSpecs     []CreateParam `json:"param_specs" doc:"Creation parameters POST /v2/me/creations accepts for this family in params. Empty array, never null."`
	RequiredFields []string      `json:"required_fields" doc:"Field keys a creation's patch must carry. Empty array, never null."`
}

type CreateParam struct {
	_         struct{} `json:"-" additionalProperties:"true"`
	Key       string   `json:"key" maxLength:"64" doc:"Parameter name. Must not be used as a discriminant."`
	FieldType string   `json:"field_type" maxLength:"32" doc:"Value kind in the schema face's field-type vocabulary (ref = a catalog id). Must not be used as a discriminant."`
	Required  bool     `json:"required" doc:"true when a creation without it is refused."`
}

type MergeRequestRecord struct {
	_               struct{}          `json:"-" additionalProperties:"true"`
	Object          string            `json:"object" enum:"merge_request" doc:"Type discriminant. Always merge_request."`
	ID              string            `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Merge request id."`
	State           string            `json:"state" enum:"open,approved,executed,rejected,withdrawn,undone" doc:"open awaits a reviewer; approved waits out the cooling-off, after which the merge runs; executed is done; undone means a reviewer took an executed merge back and source_id is live again."`
	TargetObject    string            `json:"target_object" enum:"work,company,character,person,credit_name" doc:"Family of the two entries."`
	SourceID        string            `json:"source_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that is folded away. Its id redirects to target_id once executed."`
	TargetID        string            `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that survives."`
	Note            string            `json:"note" maxLength:"4000" doc:"The requester's reason followed by decision notes. Must not be used as a discriminant."`
	ProposedByUID   string            `json:"proposed_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the requester."`
	ProposedBy      *UserRef          `json:"proposed_by" doc:"Account behind proposed_by_uid. null only when the account directory could not be read."`
	Origin          string            `json:"origin" maxLength:"32" doc:"Lane that filed it: editor for these faces; batch tools file their own. Open vocabulary; must not be used as a discriminant."`
	OriginSite      *string           `json:"origin_site" enum:"kungal,moyu,letmoe,nextmoe,editor" doc:"Property the edit was made from: kungal (the KUN forum), moyu, letmoe, nextmoe, or editor when the editor was opened directly. Attribution only, never a permission input. null for records older than origin tracking (2026-10-21) and for batch tools. GET /v2/vocabularies/origin_site gives display names and each property's user profile URL template."`
	ProposedAt      string            `json:"proposed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	ExecuteAfter    *string           `json:"execute_after" format:"date-time" maxLength:"32" doc:"End of the cooling-off once approved. null before approval."`
	ExecutedAt      *string           `json:"executed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null until executed."`
	FieldResolution map[string]string `json:"field_resolution" doc:"Fields resolved to the source's value, as field → source. A field not listed keeps the target's value (the source fills it only where the target is empty), and a value a person entered on the target is never replaced by a machine value from the source. Empty object when nothing was chosen."`
	HideReleaseIDs  []string          `json:"hide_release_ids" doc:"Source releases hidden once they land on the target, usually duplicates of a target release. Empty array, never null."`
	AmendedByUID    *string           `json:"amended_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who last changed the choices. null when unchanged."`
	AmendedBy       *UserRef          `json:"amended_by" doc:"Account behind amended_by_uid. null when unchanged or when the account directory could not be read."`
	AmendedAt       *string           `json:"amended_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null when unchanged."`
	UndoneAt        *string           `json:"undone_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null unless state is undone."`
	UndoneByUID     *string           `json:"undone_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the reviewer who undid the merge. null unless state is undone."`
	UndoneBy        *UserRef          `json:"undone_by" doc:"Account behind undone_by_uid. null unless state is undone or when the account directory could not be read."`
	UndoUntil       *string           `json:"undo_until" format:"date-time" maxLength:"32" doc:"Last moment a reviewer may undo this executed merge under the original id (catalog.merge_undo_window_days). null when it cannot be undone: not executed, undo switched off, or executed before undo was recorded."`
	Moved           map[string]int64  `json:"moved" doc:"For an executed or undone merge, how many rows of each table pointed at the source when it ran, keyed by table name. Empty object otherwise."`
	UndoReport      *MergeUndoReport  `json:"undo_report,omitempty" doc:"Present only on the response to decision=undo."`
}

type MergeUndoReport struct {
	_               struct{}         `json:"-" additionalProperties:"true"`
	Restored        map[string]int64 `json:"restored" doc:"Rows back on the source, keyed by table."`
	NotRestored     map[string]int64 `json:"not_restored" doc:"Recorded rows that could not come back, keyed by table: data both sides held and the merge folded into one row, or rows deleted since. Empty object when everything came back."`
	RevertedFields  []string         `json:"reverted_fields" doc:"Target columns put back to their pre-merge value."`
	KeptFields      []string         `json:"kept_fields" doc:"Target columns the merge overwrote but someone has edited since; left as they are."`
	RemovedInserted int64            `json:"removed_inserted" minimum:"0" doc:"Title and alias copies the merge had added to the target, removed."`
	UnhiddenRelease int64            `json:"unhidden_releases" minimum:"0" doc:"Releases the merge had hidden as duplicates, shown again."`
}
