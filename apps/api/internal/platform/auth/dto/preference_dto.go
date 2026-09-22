package dto

import "encoding/json"

type AdultConfirmationResponse struct {
	AdultConfirmedAt string `json:"adult_confirmed_at"`
}

type UpdateNSFWDisplayRequest struct {
	NSFWDisplay string `json:"nsfw_display" validate:"required,oneof=hide blur show"`
}

type NSFWDisplayResponse struct {
	NSFWDisplay      string  `json:"nsfw_display"`
	AdultConfirmedAt *string `json:"adult_confirmed_at"`
}

type PreferenceSummaryResponse struct {
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
	SizeBytes int64  `json:"size_bytes"`
}

type PreferenceDocResponse struct {
	Namespace string          `json:"namespace"`
	Doc       json.RawMessage `json:"doc"`
	Version   int             `json:"version"`
	UpdatedAt *string         `json:"updated_at"`
}

type PutPreferenceRequest struct {
	Doc json.RawMessage `json:"doc"`
}
