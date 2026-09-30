package dto

import (
	"time"

	"api/internal/platform/telemetry/model"
)

type IssueDayPoint struct {
	Day    string `json:"day"`
	Events int    `json:"events"`
}

type IssueView struct {
	ID                int64  `json:"id"`
	AppID             int64  `json:"app_id"`
	Fingerprint       string `json:"fingerprint"`
	Kind              string `json:"kind"`
	Title             string `json:"title"`
	Culprit           string `json:"culprit"`
	Status            string `json:"status"`
	ResolvedInVersion string `json:"resolved_in_version"`
	Regressed         bool   `json:"regressed"`
	FirstSeenDay      string `json:"first_seen_day"`
	LastSeenDay       string `json:"last_seen_day"`
	FirstVersion      string `json:"first_version"`
	LastVersion       string `json:"last_version"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
}

type IssueListItem struct {
	IssueView
	Events   int             `json:"events"`
	Sessions int             `json:"sessions"`
	Series   []IssueDayPoint `json:"series"`
}

type IssueDailyView struct {
	Day            string `json:"day"`
	ServiceVersion string `json:"service_version"`
	Events         int    `json:"events"`
	Sessions       int    `json:"sessions"`
}

type IssueCrashView struct {
	EventDay       string   `json:"event_day"`
	ServiceVersion string   `json:"service_version"`
	Status         string   `json:"status"`
	Needs          string   `json:"needs"`
	ExceptionType  string   `json:"exception_type"`
	Message        string   `json:"message"`
	Stack          string   `json:"stack"`
	RawStack       string   `json:"raw_stack"`
	Breadcrumbs    []string `json:"app.breadcrumbs"`
	Handled        *bool    `json:"handled"`
	DeviceModel    string   `json:"device_model"`
	OSVersion      string   `json:"os_version"`
	APILevel       *int     `json:"api_level"`
	HostArch       string   `json:"host_arch"`
}

type IssueDetail struct {
	IssueView
	Daily   []IssueDailyView `json:"daily"`
	Crashes []IssueCrashView `json:"crashes"`
}

type UpdateIssueRequest struct {
	Status            string  `json:"status"`
	ResolvedInVersion *string `json:"resolved_in_version,omitempty"`
}

func IssueViewFrom(i model.Issue) IssueView {
	return IssueView{
		ID:                i.ID,
		AppID:             i.AppID,
		Fingerprint:       i.Fingerprint,
		Kind:              i.Kind,
		Title:             i.Title,
		Culprit:           i.Culprit,
		Status:            i.Status,
		ResolvedInVersion: i.ResolvedInVersion,
		Regressed:         i.Regressed,
		FirstSeenDay:      formatDate(i.FirstSeenDay),
		LastSeenDay:       formatDate(i.LastSeenDay),
		FirstVersion:      i.FirstVersion,
		LastVersion:       i.LastVersion,
		CreatedAt:         i.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:         i.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
