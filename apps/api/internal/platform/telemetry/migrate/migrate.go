// Package migrate owns the kun_telemetry schema: AutoMigrate for the
// non-partitioned tables, raw SQL for the RANGE-partitioned event log (one
// UTC-day partition, no default partition — an insert for a day that was not
// pre-created fails instead of landing in a catch-all that retention would
// miss), and the parent indexes. Existing rows are none; this is a new
// database. Idempotent: safe on every deploy.
package migrate

import (
	"fmt"
	"time"

	"api/internal/platform/telemetry/model"

	"gorm.io/gorm"
)

func Run(db *gorm.DB) error {
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		return fmt.Errorf("telemetry pgcrypto: %w", err)
	}
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
	return ensurePartitions(db, time.Now().UTC())
}

func ensurePartitions(db *gorm.DB, now time.Time) error {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	for i := -1; i <= 3; i++ {
		if err := ensureDayPartition(db, today.AddDate(0, 0, i)); err != nil {
			return err
		}
	}
	return nil
}

func ensureDayPartition(db *gorm.DB, day time.Time) error {
	d := time.Date(day.UTC().Year(), day.UTC().Month(), day.UTC().Day(), 0, 0, 0, 0, time.UTC)
	name := "telemetry_event_p" + d.Format("20060102")
	from := d.Format("2006-01-02")
	to := d.AddDate(0, 0, 1).Format("2006-01-02")
	stmt := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF telemetry_event FOR VALUES FROM ('%s') TO ('%s')`,
		name, from, to,
	)
	if err := db.Exec(stmt).Error; err != nil {
		return fmt.Errorf("create partition %s: %w", name, err)
	}
	return nil
}

func rawSQL(db *gorm.DB) error {
	stmts := []struct{ name, sql string }{
		{"telemetry_event", `
			CREATE TABLE IF NOT EXISTS telemetry_event (
				id uuid NOT NULL DEFAULT gen_random_uuid(),
				received_on date NOT NULL,
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
				PRIMARY KEY (received_on, id)
			) PARTITION BY RANGE (received_on)`},
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
