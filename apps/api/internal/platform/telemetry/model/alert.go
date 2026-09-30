package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	AlertKindEmail = "email"

	UrgencyImmediate = "immediate"
	UrgencyDigest    = "digest"

	AlertQueued     = "queued"
	AlertSent       = "sent"
	AlertFailed     = "failed"
	AlertSuppressed = "suppressed"

	RuleContractNew       = "contract_new"
	RuleIssueNew          = "issue_new"
	RuleIssueRegressed    = "issue_regressed"
	RuleCrashRate         = "crash_rate"
	RuleAnrRate           = "anr_rate"
	RuleCrashRegression   = "crash_regression"
	RuleServerFaults      = "server_faults"
	RuleSymbolsMissing    = "symbols_missing"
	RuleSilentApp         = "silent_app"
	RuleEngineFetchFailed = "engine_fetch_failed"

	DefaultCrashRate              = 0.0109
	DefaultAnrRate                = 0.0047
	DefaultMinSessions            = 200
	DefaultRegressionFactor       = 2.0
	DefaultRegressionMinDelta     = 0.005
	DefaultServerFaultsPerHour    = 50
	DefaultSilentHours            = 6
	DefaultSilentMinDailySessions = 50
)

type AlertSettings struct {
	CrashRate              float64 `json:"crash_rate"`
	AnrRate                float64 `json:"anr_rate"`
	MinSessions            int     `json:"min_sessions"`
	RegressionFactor       float64 `json:"regression_factor"`
	RegressionMinDelta     float64 `json:"regression_min_delta"`
	ServerFaultsPerHour    int     `json:"server_faults_per_hour"`
	SilentHours            int     `json:"silent_hours"`
	SilentMinDailySessions int     `json:"silent_min_daily_sessions"`
}

type alertSettingsJSON struct {
	CrashRate              *float64 `json:"crash_rate"`
	AnrRate                *float64 `json:"anr_rate"`
	MinSessions            *int     `json:"min_sessions"`
	RegressionFactor       *float64 `json:"regression_factor"`
	RegressionMinDelta     *float64 `json:"regression_min_delta"`
	ServerFaultsPerHour    *int     `json:"server_faults_per_hour"`
	SilentHours            *int     `json:"silent_hours"`
	SilentMinDailySessions *int     `json:"silent_min_daily_sessions"`
}

func DefaultAlertSettings() AlertSettings {
	return AlertSettings{
		CrashRate:              DefaultCrashRate,
		AnrRate:                DefaultAnrRate,
		MinSessions:            DefaultMinSessions,
		RegressionFactor:       DefaultRegressionFactor,
		RegressionMinDelta:     DefaultRegressionMinDelta,
		ServerFaultsPerHour:    DefaultServerFaultsPerHour,
		SilentHours:            DefaultSilentHours,
		SilentMinDailySessions: DefaultSilentMinDailySessions,
	}
}

func (s AlertSettings) IsZero() bool { return s == AlertSettings{} }

func (s *AlertSettings) Effective() AlertSettings {
	if s == nil || s.IsZero() {
		return DefaultAlertSettings()
	}
	return *s
}

func decodeAlertSettings(b []byte) (AlertSettings, error) {
	out := DefaultAlertSettings()
	if len(b) == 0 || string(b) == "null" {
		return out, nil
	}
	var raw alertSettingsJSON
	if err := json.Unmarshal(b, &raw); err != nil {
		return AlertSettings{}, err
	}
	if raw.CrashRate != nil {
		out.CrashRate = *raw.CrashRate
	}
	if raw.AnrRate != nil {
		out.AnrRate = *raw.AnrRate
	}
	if raw.MinSessions != nil {
		out.MinSessions = *raw.MinSessions
	}
	if raw.RegressionFactor != nil {
		out.RegressionFactor = *raw.RegressionFactor
	}
	if raw.RegressionMinDelta != nil {
		out.RegressionMinDelta = *raw.RegressionMinDelta
	}
	if raw.ServerFaultsPerHour != nil {
		out.ServerFaultsPerHour = *raw.ServerFaultsPerHour
	}
	if raw.SilentHours != nil {
		out.SilentHours = *raw.SilentHours
	}
	if raw.SilentMinDailySessions != nil {
		out.SilentMinDailySessions = *raw.SilentMinDailySessions
	}
	return out, nil
}

func (s *AlertSettings) UnmarshalJSON(b []byte) error {
	v, err := decodeAlertSettings(b)
	if err != nil {
		return err
	}
	*s = v
	return nil
}

func (s AlertSettings) MarshalJSON() ([]byte, error) {
	e := s.Effective()
	type out AlertSettings
	return json.Marshal(out(e))
}

func (s *AlertSettings) Scan(value any) error {
	if value == nil {
		*s = DefaultAlertSettings()
		return nil
	}
	var b []byte
	switch v := value.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("alert_settings: unsupported type %T", value)
	}
	decoded, err := decodeAlertSettings(b)
	if err != nil {
		return err
	}
	*s = decoded
	return nil
}

func (s AlertSettings) Value() (driver.Value, error) {
	if s.IsZero() {
		return nil, nil
	}
	return json.Marshal(s)
}

func (s AlertSettings) Validate() error {
	switch {
	case s.CrashRate <= 0 || s.CrashRate >= 1:
		return fmt.Errorf("crash_rate must be in (0, 1)")
	case s.AnrRate <= 0 || s.AnrRate >= 1:
		return fmt.Errorf("anr_rate must be in (0, 1)")
	case s.MinSessions < 1 || s.MinSessions > 1_000_000:
		return fmt.Errorf("min_sessions must be in [1, 1000000]")
	case s.RegressionFactor < 1 || s.RegressionFactor > 100:
		return fmt.Errorf("regression_factor must be in [1, 100]")
	case s.RegressionMinDelta < 0 || s.RegressionMinDelta > 1:
		return fmt.Errorf("regression_min_delta must be in [0, 1]")
	case s.ServerFaultsPerHour < 1 || s.ServerFaultsPerHour > 1_000_000:
		return fmt.Errorf("server_faults_per_hour must be in [1, 1000000]")
	case s.SilentHours < 1 || s.SilentHours > 168:
		return fmt.Errorf("silent_hours must be in [1, 168]")
	case s.SilentMinDailySessions < 1 || s.SilentMinDailySessions > 1_000_000:
		return fmt.Errorf("silent_min_daily_sessions must be in [1, 1000000]")
	}
	return nil
}

func ParseEmailTarget(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	if s == "" || len(s) > 254 {
		return "", fmt.Errorf("email target must be a bare address of at most 254 bytes")
	}
	addr, err := mail.ParseAddress(s)
	if err != nil {
		return "", fmt.Errorf("email target is not a valid address")
	}
	if addr.Name != "" || !strings.EqualFold(s, addr.Address) {
		return "", fmt.Errorf("email target must be a bare address without a display name")
	}
	out := strings.ToLower(addr.Address)
	if len(out) > 254 {
		return "", fmt.Errorf("email target must be a bare address of at most 254 bytes")
	}
	return out, nil
}

type AlertChannel struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false;type:bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY;default:(-)" json:"id"`
	Kind      string    `gorm:"not null;column:kind;uniqueIndex:idx_telemetry_alert_channel_kind_target,priority:1" json:"kind"`
	Target    string    `gorm:"not null;column:target;uniqueIndex:idx_telemetry_alert_channel_kind_target,priority:2" json:"target"`
	Enabled   bool      `gorm:"not null;column:enabled" json:"enabled"`
	CreatedAt time.Time `gorm:"not null;column:created_at" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;column:updated_at" json:"updated_at"`
}

func (AlertChannel) TableName() string { return "telemetry_alert_channel" }

type Alert struct {
	ID         int64          `gorm:"primaryKey;autoIncrement:false;type:bigint GENERATED BY DEFAULT AS IDENTITY PRIMARY KEY;default:(-)" json:"id"`
	AppID      int64          `gorm:"not null;column:app_id;uniqueIndex:idx_telemetry_alert_dedup,priority:2" json:"app_id"`
	Rule       string         `gorm:"not null;column:rule;uniqueIndex:idx_telemetry_alert_dedup,priority:1" json:"rule"`
	SubjectKey string         `gorm:"not null;column:subject_key;uniqueIndex:idx_telemetry_alert_dedup,priority:3" json:"subject_key"`
	Urgency    string         `gorm:"not null;column:urgency;index:idx_telemetry_alert_status_urgency_created,priority:2" json:"urgency"`
	Title      string         `gorm:"not null;column:title" json:"title"`
	Facts      datatypes.JSON `gorm:"type:jsonb;not null;default:'{}';column:facts" json:"facts"`
	Status     string         `gorm:"not null;column:status;index:idx_telemetry_alert_status_urgency_created,priority:1" json:"status"`
	Attempts   int            `gorm:"not null;column:attempts" json:"attempts"`
	LastError  string         `gorm:"not null;column:last_error" json:"last_error"`
	CreatedAt  time.Time      `gorm:"not null;column:created_at;index:idx_telemetry_alert_status_urgency_created,priority:3" json:"created_at"`
	SentAt     *time.Time     `gorm:"column:sent_at" json:"sent_at"`
}

func (Alert) TableName() string { return "telemetry_alert" }
