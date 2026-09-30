package store

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	suitelock "api/internal/platform/telemetry/dbtest"
	"api/internal/platform/telemetry/migrate"
	"api/internal/platform/telemetry/otlp"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB *gorm.DB
	st     *Store
	recUID atomic.Uint64
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("telemetry/store")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("telemetry/store", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("telemetry/store", "telemetry migration failed: %v", err)
	}
	testDB = db
	st = New(db)
	if err := st.EnsurePartitions(context.Background(), receiptNow()); err != nil {
		release()
		dbtest.SkipMainf("telemetry/store", "ensure partitions: %v", err)
	}
	code := m.Run()
	release()
	os.Exit(code)
}

func truncate(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(`TRUNCATE telemetry_issue_daily, telemetry_crash, telemetry_issue, telemetry_symbol_file, telemetry_symbol_upload, telemetry_engine_symbol, telemetry_blob, telemetry_event, telemetry_session, telemetry_daily_metric, telemetry_app RESTART IDENTITY CASCADE`).Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func receiptNow() time.Time {
	return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
}

func rec(name, sid, version, env string, ts time.Time, attrs map[string]any) otlp.Record {
	if attrs == nil {
		attrs = map[string]any{}
	}
	receipt := receiptNow()
	day, keep := otlp.ClassifyDay(ts, receipt)
	if !keep {
		day = otlp.DateUTC(ts)
	}
	n := recUID.Add(1)
	return otlp.Record{
		ServiceVersion: version,
		Environment:    env,
		EventName:      name,
		EventTime:      ts,
		EventDay:       day,
		SessionID:      sid,
		RecordUID:      fmt.Sprintf("%032x", n),
		Attributes:     attrs,
	}
}

func write(t *testing.T, appID int64, recs ...otlp.Record) {
	t.Helper()
	if err := st.Write(context.Background(), appID, receiptNow(), otlp.Batch{Records: recs}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func writeAt(t *testing.T, appID int64, at time.Time, recs ...otlp.Record) {
	t.Helper()
	if err := st.Write(context.Background(), appID, at, otlp.Batch{Records: recs}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func sid(n byte) string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = 'a'
	}
	b[31] = '0' + n
	return string(b)
}
