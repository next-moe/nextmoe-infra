package dto

import (
	"time"

	"api/internal/platform/telemetry/model"
)

type AppView struct {
	ID          int64  `json:"id"`
	ServiceName string `json:"service_name"`
	DisplayName string `json:"display_name"`
	IngestKey   string `json:"ingest_key"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type CreateAppRequest struct {
	ServiceName string `json:"service_name"`
	DisplayName string `json:"display_name"`
}

type UpdateAppRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
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
	return AppView{
		ID:          a.ID,
		ServiceName: a.ServiceName,
		DisplayName: a.DisplayName,
		IngestKey:   a.IngestKey,
		Enabled:     a.Enabled,
		CreatedAt:   a.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:   a.UpdatedAt.UTC().Format(time.RFC3339),
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
