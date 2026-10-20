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
	_             struct{} `json:"-" additionalProperties:"true"`
	Object        string   `json:"object" enum:"merge_request" doc:"Type discriminant. Always merge_request."`
	ID            string   `json:"id" pattern:"^[0-9]+$" minLength:"1" maxLength:"20" doc:"Merge request id."`
	State         string   `json:"state" enum:"open,approved,executed,rejected,withdrawn" doc:"open awaits a reviewer; approved waits out the cooling-off, after which the merge runs; executed is done."`
	TargetObject  string   `json:"target_object" enum:"work,company,character,person,credit_name" doc:"Family of the two entries."`
	SourceID      string   `json:"source_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that is folded away. Its id redirects to target_id once executed."`
	TargetID      string   `json:"target_id" pattern:"^[0-9]+$" maxLength:"20" doc:"The entry that survives."`
	Note          string   `json:"note" maxLength:"4000" doc:"The requester's reason followed by decision notes. Must not be used as a discriminant."`
	ProposedByUID string   `json:"proposed_by_uid" pattern:"^[0-9]+$" maxLength:"20" doc:"Central account id of the requester."`
	ProposedBy    *UserRef `json:"proposed_by" doc:"Account behind proposed_by_uid. null only when the account directory could not be read."`
	Origin        string   `json:"origin" maxLength:"32" doc:"Lane that filed it: editor for these faces; batch tools file their own. Open vocabulary; must not be used as a discriminant."`
	ProposedAt    string   `json:"proposed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC."`
	ExecuteAfter  *string  `json:"execute_after" format:"date-time" maxLength:"32" doc:"End of the cooling-off once approved. null before approval."`
	ExecutedAt    *string  `json:"executed_at" format:"date-time" maxLength:"32" doc:"RFC 3339 UTC. null until executed."`
}
