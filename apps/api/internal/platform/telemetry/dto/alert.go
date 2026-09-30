package dto

import (
	"encoding/json"
	"time"

	"api/internal/platform/telemetry/model"
)

type AlertChannelView struct {
	ID        int64  `json:"id"`
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Enabled   bool   `json:"enabled"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type CreateAlertChannelRequest struct {
	Kind    string `json:"kind"`
	Target  string `json:"target"`
	Enabled *bool  `json:"enabled,omitempty"`
}

type UpdateAlertChannelRequest struct {
	Target  *string `json:"target,omitempty"`
	Enabled *bool   `json:"enabled,omitempty"`
}

type AlertChannelTestResult struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
}

type AlertView struct {
	ID         int64           `json:"id"`
	AppID      int64           `json:"app_id"`
	Rule       string          `json:"rule"`
	SubjectKey string          `json:"subject_key"`
	Urgency    string          `json:"urgency"`
	Title      string          `json:"title"`
	Facts      json.RawMessage `json:"facts"`
	Status     string          `json:"status"`
	Attempts   int             `json:"attempts"`
	LastError  string          `json:"last_error"`
	CreatedAt  string          `json:"created_at"`
	SentAt     *string         `json:"sent_at"`
}

func AlertChannelViewFrom(c model.AlertChannel) AlertChannelView {
	return AlertChannelView{
		ID:        c.ID,
		Kind:      c.Kind,
		Target:    c.Target,
		Enabled:   c.Enabled,
		CreatedAt: c.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt: c.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func AlertViewFrom(a model.Alert) AlertView {
	facts := json.RawMessage(a.Facts)
	if len(facts) == 0 {
		facts = json.RawMessage(`{}`)
	}
	var sent *string
	if a.SentAt != nil {
		s := a.SentAt.UTC().Format(time.RFC3339)
		sent = &s
	}
	return AlertView{
		ID:         a.ID,
		AppID:      a.AppID,
		Rule:       a.Rule,
		SubjectKey: a.SubjectKey,
		Urgency:    a.Urgency,
		Title:      a.Title,
		Facts:      facts,
		Status:     a.Status,
		Attempts:   a.Attempts,
		LastError:  a.LastError,
		CreatedAt:  a.CreatedAt.UTC().Format(time.RFC3339),
		SentAt:     sent,
	}
}
