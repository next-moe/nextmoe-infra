package model

import "time"

type DailyMetric struct {
	AppID             int64     `gorm:"primaryKey;column:app_id" json:"app_id"`
	ServiceVersion    string    `gorm:"primaryKey;column:service_version" json:"service_version"`
	Environment       string    `gorm:"primaryKey;column:environment" json:"environment"`
	Day               time.Time `gorm:"primaryKey;type:date;column:day" json:"day"`
	Sessions          int       `gorm:"not null;column:sessions" json:"sessions"`
	CrashedSessions   int       `gorm:"not null;column:crashed_sessions" json:"crashed_sessions"`
	ANRSessions       int       `gorm:"not null;column:anr_sessions" json:"anr_sessions"`
	UnhandledSessions int       `gorm:"not null;column:unhandled_sessions" json:"unhandled_sessions"`
	AbnormalSessions  int       `gorm:"not null;column:abnormal_sessions" json:"abnormal_sessions"`
	Exceptions        int       `gorm:"not null;column:exceptions" json:"exceptions"`
	CrashJava         int       `gorm:"not null;column:crash_java" json:"crash_java"`
	CrashNative       int       `gorm:"not null;column:crash_native" json:"crash_native"`
	CrashANR          int       `gorm:"not null;column:crash_anr" json:"crash_anr"`
	StartupCount      int       `gorm:"not null;column:startup_count" json:"startup_count"`
	TtidP50Ms         *float64  `gorm:"column:ttid_p50_ms" json:"ttid_p50_ms"`
	TtidP90Ms         *float64  `gorm:"column:ttid_p90_ms" json:"ttid_p90_ms"`
	JankFramesOver    int64     `gorm:"not null;column:jank_frames_over" json:"jank_frames_over"`
	JankFramesTotal   int64     `gorm:"not null;column:jank_frames_total" json:"jank_frames_total"`
	UpdatedAt         time.Time `gorm:"column:updated_at" json:"updated_at"`
}

func (DailyMetric) TableName() string { return "telemetry_daily_metric" }
