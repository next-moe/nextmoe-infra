package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	CrashPending        = "pending"
	CrashWaitingSymbols = "waiting_symbols"
	CrashDone           = "done"
	CrashFailed         = "failed"
	KindException       = "exception"
	KindContract        = "contract"
	KindServer          = "server"
	KindJava            = "java"
	KindNative          = "native"
	KindANR             = "anr"
	IssueOpen           = "open"
	IssueResolved       = "resolved"
	IssueIgnored        = "ignored"
)

// Crash has no receipt- or processing-granularity timestamp. A worker handles
// every crash row of one ingest request together, and a shared processing
// instant would link them — the same privacy invariant as telemetry_session.
type Crash struct {
	EventDay       time.Time      `gorm:"primaryKey;type:date;column:event_day;index:idx_telemetry_crash_issue_day_ver,priority:2" json:"event_day"`
	RecordUID      string         `gorm:"primaryKey;column:record_uid" json:"record_uid"`
	AppID          int64          `gorm:"not null;column:app_id" json:"app_id"`
	ServiceVersion string         `gorm:"not null;column:service_version;index:idx_telemetry_crash_issue_day_ver,priority:3" json:"service_version"`
	SessionID      *string        `gorm:"column:session_id" json:"session_id"`
	EventName      string         `gorm:"not null;column:event_name" json:"event_name"`
	Kind           string         `gorm:"not null;column:kind" json:"kind"`
	Handled        *bool          `gorm:"column:handled" json:"handled"`
	Status         string         `gorm:"not null;column:status;index:idx_telemetry_crash_status" json:"status"`
	Needs          string         `gorm:"not null;column:needs" json:"needs"`
	Attempts       int            `gorm:"not null;column:attempts" json:"attempts"`
	ExceptionType  string         `gorm:"not null;column:exception_type" json:"exception_type"`
	Message        string         `gorm:"not null;column:message" json:"message"`
	Stack          string         `gorm:"not null;column:stack" json:"stack"`
	Frames         datatypes.JSON `gorm:"type:jsonb;not null;default:'[]';column:frames" json:"frames"`
	Fingerprint    string         `gorm:"not null;column:fingerprint" json:"fingerprint"`
	IssueID        *int64         `gorm:"column:issue_id;index:idx_telemetry_crash_issue_day_ver,priority:1" json:"issue_id"`
}

func (Crash) TableName() string { return "telemetry_crash" }
