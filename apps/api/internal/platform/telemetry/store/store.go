package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/telemetry/ingest"
	"api/internal/platform/telemetry/otlp"

	"gorm.io/gorm"
)

type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) Write(ctx context.Context, appID int64, receivedAt time.Time, batch otlp.Batch) error {
	if len(batch.Records) == 0 {
		return nil
	}
	receivedAt = receivedAt.UTC()
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := insertEvents(tx, appID, batch.Records); err != nil {
			return err
		}
		return upsertSessions(tx, appID, receivedAt, batch.Records)
	})
}

func insertEvents(tx *gorm.DB, appID int64, recs []otlp.Record) error {
	var b strings.Builder
	b.WriteString(`INSERT INTO telemetry_event (
		record_uid, event_day, app_id, service_version, environment,
		event_name, severity, event_time, session_id,
		os_name, os_version, api_level, device_model, device_manufacturer,
		host_arch, sdk_version, attributes, body
	) VALUES `)
	args := make([]any, 0, len(recs)*18)
	for i, r := range recs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)")
		attrs := make(map[string]any, len(r.Attributes))
		for k, v := range r.Attributes {
			if k == "session.previous_id" {
				continue
			}
			attrs[k] = v
		}
		attr, err := json.Marshal(attrs)
		if err != nil {
			return fmt.Errorf("marshal attributes: %w", err)
		}
		if len(attr) == 0 || string(attr) == "null" {
			attr = []byte("{}")
		}
		var sid any
		if r.SessionID != "" {
			sid = r.SessionID
		}
		var api any
		if r.APILevel != nil {
			api = *r.APILevel
		}
		var body any
		if r.Body != nil {
			body = *r.Body
		}
		args = append(args,
			r.RecordUID, r.EventDay.Format("2006-01-02"), appID, r.ServiceVersion, r.Environment,
			r.EventName, r.Severity, r.EventTime, sid,
			r.OSName, r.OSVersion, api, r.DeviceModel, r.DeviceManufacturer,
			r.HostArch, r.SDKVersion, string(attr), body,
		)
	}
	b.WriteString(` ON CONFLICT (event_day, record_uid) DO NOTHING`)
	if err := tx.Exec(b.String(), args...).Error; err != nil {
		return fmt.Errorf("insert events: %w", err)
	}
	return nil
}

func (s *Store) ListApps(ctx context.Context) ([]ingest.AppInfo, error) {
	var rows []struct {
		ID          int64
		ServiceName string
		Enabled     bool
		IngestKey   string
	}
	if err := s.db.WithContext(ctx).Table("telemetry_app").
		Select("id, service_name, enabled, ingest_key").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ingest.AppInfo, len(rows))
	for i, r := range rows {
		out[i] = ingest.AppInfo{ID: r.ID, ServiceName: r.ServiceName, Enabled: r.Enabled, IngestKey: r.IngestKey}
	}
	return out, nil
}
