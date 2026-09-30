// Package migrate owns the kun_telemetry schema: AutoMigrate for the
// non-partitioned tables, raw SQL for the RANGE-partitioned event log
// (PARTITION BY RANGE (event_day), PRIMARY KEY (event_day, record_uid), one
// UTC-day partition named telemetry_event_pYYYYMMDD, no default partition —
// an insert for a day that was not pre-created fails instead of landing in a
// catch-all that retention would miss), and the parent indexes. Existing rows
// are none; this is a new database. Idempotent: safe on every deploy.
package migrate

import (
	"fmt"
	"time"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/partition"

	"gorm.io/gorm"
)

func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(
		&model.App{},
		&model.Session{},
		&model.DailyMetric{},
	); err != nil {
		return fmt.Errorf("telemetry automigrate: %w", err)
	}
	if err := rawSQL(db); err != nil {
		return err
	}
	return partition.EnsurePartitions(db, time.Now().UTC())
}

func rawSQL(db *gorm.DB) error {
	stmts := []struct{ name, sql string }{
		{"telemetry_event", `
			CREATE TABLE IF NOT EXISTS telemetry_event (
				record_uid text NOT NULL,
				event_day date NOT NULL,
				app_id bigint NOT NULL,
				service_version text NOT NULL,
				environment text NOT NULL,
				event_name text NOT NULL,
				severity smallint NOT NULL,
				event_time timestamptz NOT NULL,
				session_id text,
				os_name text NOT NULL,
				os_version text NOT NULL,
				api_level integer,
				device_model text NOT NULL,
				device_manufacturer text NOT NULL,
				host_arch text NOT NULL,
				sdk_version text NOT NULL,
				attributes jsonb NOT NULL,
				body text,
				PRIMARY KEY (event_day, record_uid)
			) PARTITION BY RANGE (event_day)`},
		{"idx_telemetry_event_app_name_day", `
			CREATE INDEX IF NOT EXISTS idx_telemetry_event_app_name_day
			    ON telemetry_event (app_id, event_name, event_day)`},
		{"idx_telemetry_event_app_session", `
			CREATE INDEX IF NOT EXISTS idx_telemetry_event_app_session
			    ON telemetry_event (app_id, session_id)
			 WHERE session_id IS NOT NULL`},
	}
	for _, s := range stmts {
		if err := db.Exec(s.sql).Error; err != nil {
			return fmt.Errorf("telemetry %s: %w", s.name, err)
		}
	}
	return nil
}
