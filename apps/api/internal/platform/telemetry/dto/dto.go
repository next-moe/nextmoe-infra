package dto

import (
	"time"

	"api/internal/platform/telemetry/model"
)

type AppView struct {
	ID              int64               `json:"id"`
	ServiceName     string              `json:"service_name"`
	DisplayName     string              `json:"display_name"`
	IngestKey       string              `json:"ingest_key"`
	Enabled         bool                `json:"enabled"`
	HasSymbolsToken bool                `json:"has_symbols_token"`
	InAppPrefixes   []string            `json:"in_app_prefixes"`
	AlertSettings   model.AlertSettings `json:"alert_settings"`
	CreatedAt       string              `json:"created_at"`
	UpdatedAt       string              `json:"updated_at"`
}

type CreateAppRequest struct {
	ServiceName string `json:"service_name"`
	DisplayName string `json:"display_name"`
}

type UpdateAppRequest struct {
	DisplayName   *string             `json:"display_name,omitempty"`
	Enabled       *bool               `json:"enabled,omitempty"`
	InAppPrefixes *[]string           `json:"in_app_prefixes,omitempty"`
	AlertSettings *AlertSettingsInput `json:"alert_settings,omitempty"`
}

type AlertSettingsInput struct {
	CrashRate              *float64 `json:"crash_rate,omitempty"`
	AnrRate                *float64 `json:"anr_rate,omitempty"`
	MinSessions            *int     `json:"min_sessions,omitempty"`
	RegressionFactor       *float64 `json:"regression_factor,omitempty"`
	RegressionMinDelta     *float64 `json:"regression_min_delta,omitempty"`
	ServerFaultsPerHour    *int     `json:"server_faults_per_hour,omitempty"`
	SilentHours            *int     `json:"silent_hours,omitempty"`
	SilentMinDailySessions *int     `json:"silent_min_daily_sessions,omitempty"`
}

func (in AlertSettingsInput) Settings() model.AlertSettings {
	out := model.DefaultAlertSettings()
	if in.CrashRate != nil {
		out.CrashRate = *in.CrashRate
	}
	if in.AnrRate != nil {
		out.AnrRate = *in.AnrRate
	}
	if in.MinSessions != nil {
		out.MinSessions = *in.MinSessions
	}
	if in.RegressionFactor != nil {
		out.RegressionFactor = *in.RegressionFactor
	}
	if in.RegressionMinDelta != nil {
		out.RegressionMinDelta = *in.RegressionMinDelta
	}
	if in.ServerFaultsPerHour != nil {
		out.ServerFaultsPerHour = *in.ServerFaultsPerHour
	}
	if in.SilentHours != nil {
		out.SilentHours = *in.SilentHours
	}
	if in.SilentMinDailySessions != nil {
		out.SilentMinDailySessions = *in.SilentMinDailySessions
	}
	return out
}

type SymbolsTokenView struct {
	Token string `json:"token"`
}

type SymbolFileView struct {
	FileName string `json:"file_name"`
	Kind     string `json:"kind"`
	Arch     string `json:"arch"`
	BuildID  string `json:"build_id"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type EngineSymbolView struct {
	Variant   string `json:"variant"`
	Status    string `json:"status"`
	BuildID   string `json:"build_id"`
	LastError string `json:"last_error"`
}

type SymbolUploadView struct {
	ID             int64              `json:"id"`
	AppID          int64              `json:"app_id"`
	ServiceVersion string             `json:"service_version"`
	EngineRevision string             `json:"engine_revision"`
	CreatedAt      string             `json:"created_at"`
	Files          []SymbolFileView   `json:"files"`
	EngineSymbols  []EngineSymbolView `json:"engine_symbols,omitempty"`
}

type DailyMetricView struct {
	AppID             int64    `json:"app_id"`
	ServiceVersion    string   `json:"service_version"`
	Environment       string   `json:"environment"`
	Day               string   `json:"day"`
	Sessions          int      `json:"sessions"`
	CrashedSessions   int      `json:"crashed_sessions"`
	ANRSessions       int      `json:"anr_sessions"`
	UnhandledSessions int      `json:"unhandled_sessions"`
	AbnormalSessions  int      `json:"abnormal_sessions"`
	Exceptions        int      `json:"exceptions"`
	CrashJava         int      `json:"crash_java"`
	CrashNative       int      `json:"crash_native"`
	CrashANR          int      `json:"crash_anr"`
	StartupCount      int      `json:"startup_count"`
	TtidP50Ms         *float64 `json:"ttid_p50_ms"`
	TtidP90Ms         *float64 `json:"ttid_p90_ms"`
	JankFramesOver    int64    `json:"jank_frames_over"`
	JankFramesTotal   int64    `json:"jank_frames_total"`
	CrashRate         *float64 `json:"crash_rate"`
	ANRRate           *float64 `json:"anr_rate"`
	UnhandledRate     *float64 `json:"unhandled_rate"`
	JankRatio         *float64 `json:"jank_ratio"`
}

func formatDate(t time.Time) string {
	return t.Format("2006-01-02")
}

func AppViewFrom(a model.App) AppView {
	prefixes := []string(a.InAppPrefixes)
	if prefixes == nil {
		prefixes = []string{}
	}
	return AppView{
		ID:              a.ID,
		ServiceName:     a.ServiceName,
		DisplayName:     a.DisplayName,
		IngestKey:       a.IngestKey,
		Enabled:         a.Enabled,
		HasSymbolsToken: a.SymbolsTokenHash != nil && *a.SymbolsTokenHash != "",
		InAppPrefixes:   prefixes,
		AlertSettings:   a.AlertSettings.Effective(),
		CreatedAt:       a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:       a.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func ratio(num, den int) *float64 {
	if den == 0 {
		return nil
	}
	v := float64(num) / float64(den)
	return &v
}

func jankRatio(over, total int64) *float64 {
	if total == 0 {
		return nil
	}
	v := float64(over) / float64(total)
	return &v
}

func FromModel(r model.DailyMetric) DailyMetricView {
	return DailyMetricView{
		AppID:             r.AppID,
		ServiceVersion:    r.ServiceVersion,
		Environment:       r.Environment,
		Day:               formatDate(r.Day),
		Sessions:          r.Sessions,
		CrashedSessions:   r.CrashedSessions,
		ANRSessions:       r.ANRSessions,
		UnhandledSessions: r.UnhandledSessions,
		AbnormalSessions:  r.AbnormalSessions,
		Exceptions:        r.Exceptions,
		CrashJava:         r.CrashJava,
		CrashNative:       r.CrashNative,
		CrashANR:          r.CrashANR,
		StartupCount:      r.StartupCount,
		TtidP50Ms:         r.TtidP50Ms,
		TtidP90Ms:         r.TtidP90Ms,
		JankFramesOver:    r.JankFramesOver,
		JankFramesTotal:   r.JankFramesTotal,
		CrashRate:         ratio(r.CrashedSessions, r.Sessions),
		ANRRate:           ratio(r.ANRSessions, r.Sessions),
		UnhandledRate:     ratio(r.UnhandledSessions, r.Sessions),
		JankRatio:         jankRatio(r.JankFramesOver, r.JankFramesTotal),
	}
}
