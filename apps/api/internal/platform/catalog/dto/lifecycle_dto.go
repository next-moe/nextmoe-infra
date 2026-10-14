package dto

type StaffClaimActionRequest struct {
	Reason string `json:"reason,omitempty" doc:"Moderator note; REQUIRED for decline, recorded on the event"`
}

type ClaimTransitionInfo struct {
	CurrentState string   `json:"current_state"`
	AllowedFrom  []string `json:"allowed_from"`
}
