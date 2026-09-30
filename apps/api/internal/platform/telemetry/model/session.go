package model

import "time"

// Session has no updated_at (and no other receipt-granularity timestamp). A
// transaction's now() is identical for every session a batch touches, which
// would link them into a device history — the privacy invariant the ingest
// contract forbids ("do not keep any same-batch association").
type Session struct {
	AppID          int64      `gorm:"primaryKey;column:app_id;index:idx_telemetry_session_app_id_day,priority:1" json:"app_id"`
	SessionID      string     `gorm:"primaryKey;column:session_id" json:"session_id"`
	ServiceVersion string     `gorm:"not null;column:service_version" json:"service_version"`
	Environment    string     `gorm:"not null;column:environment" json:"environment"`
	Day            time.Time  `gorm:"type:date;not null;column:day;index:idx_telemetry_session_day;index:idx_telemetry_session_app_id_day,priority:2" json:"day"`
	StartedAt      *time.Time `gorm:"column:started_at" json:"started_at"`
	EndedAt        *time.Time `gorm:"column:ended_at" json:"ended_at"`
	Status         *string    `gorm:"column:status" json:"status"`
	Errors         *int       `gorm:"column:errors" json:"errors"`
	ANR            bool       `gorm:"not null;column:anr" json:"anr"`
}

func (Session) TableName() string { return "telemetry_session" }
