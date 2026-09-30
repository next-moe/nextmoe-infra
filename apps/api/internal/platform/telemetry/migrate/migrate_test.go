package migrate

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	suitelock "api/internal/platform/telemetry/dbtest"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("telemetry/migrate")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("telemetry/migrate", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := Run(db); err != nil {
		release()
		dbtest.SkipMainf("telemetry/migrate", "telemetry migration failed: %v", err)
	}
	if err := Run(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "telemetry migration is NOT idempotent: %v\n", err)
		os.Exit(1)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func TestRunIdempotent(t *testing.T) {
	if err := Run(testDB); err != nil {
		t.Fatalf("third Run: %v", err)
	}
}

func TestSymbolTablesShape(t *testing.T) {
	want := map[string][]string{
		"telemetry_symbol_upload": {"id", "app_id", "service_version", "engine_revision", "created_at"},
		"telemetry_symbol_file":   {"id", "upload_id", "app_id", "service_version", "file_name", "kind", "arch", "build_id", "sha256", "size", "created_at"},
		"telemetry_blob":          {"sha256", "size", "created_at"},
		"telemetry_engine_symbol": {"engine_revision", "variant", "status", "build_id", "sha256", "size", "attempts", "next_attempt_at", "last_error", "updated_at"},
	}
	for table, cols := range want {
		var got []string
		if err := testDB.Raw(`
			SELECT column_name FROM information_schema.columns
			 WHERE table_name = ? ORDER BY ordinal_position`, table).Scan(&got).Error; err != nil {
			t.Fatalf("%s columns: %v", table, err)
		}
		have := map[string]bool{}
		for _, c := range got {
			have[c] = true
		}
		for _, c := range cols {
			if !have[c] {
				t.Errorf("%s missing column %s (have %v)", table, c, got)
			}
		}
	}
	var appCols []string
	if err := testDB.Raw(`
		SELECT column_name FROM information_schema.columns
		 WHERE table_name = 'telemetry_app'`).Scan(&appCols).Error; err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range appCols {
		if c == "symbols_token_hash" {
			found = true
		}
	}
	if !found {
		t.Fatal("telemetry_app.symbols_token_hash missing")
	}

	var upPK, filePK, blobPK, engPK string
	mustPK := func(table string, dest *string) {
		t.Helper()
		if err := testDB.Raw(`
			SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum))
			  FROM pg_index i
			  JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
			 WHERE i.indrelid = ?::regclass AND i.indisprimary`, table).Scan(dest).Error; err != nil {
			t.Fatalf("pk %s: %v", table, err)
		}
	}
	mustPK("telemetry_symbol_upload", &upPK)
	mustPK("telemetry_symbol_file", &filePK)
	mustPK("telemetry_blob", &blobPK)
	mustPK("telemetry_engine_symbol", &engPK)
	if upPK != "id" || filePK != "id" || blobPK != "sha256" || engPK != "engine_revision,variant" {
		t.Errorf("pks upload=%q file=%q blob=%q engine=%q", upPK, filePK, blobPK, engPK)
	}
}

func TestPartitionedTableShape(t *testing.T) {
	var kind string
	if err := testDB.Raw(`SELECT relkind::text FROM pg_class WHERE relname = 'telemetry_event'`).Scan(&kind).Error; err != nil {
		t.Fatalf("relkind: %v", err)
	}
	if kind != "p" {
		t.Errorf("telemetry_event relkind=%q want p (partitioned)", kind)
	}

	var partkey string
	if err := testDB.Raw(`SELECT pg_get_partkeydef('telemetry_event'::regclass)`).Scan(&partkey).Error; err != nil {
		t.Fatalf("partkey: %v", err)
	}
	if !strings.Contains(partkey, "event_day") {
		t.Errorf("partition key %q does not contain event_day", partkey)
	}

	var cols string
	if err := testDB.Raw(`
		SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum))
		  FROM pg_index i
		  JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		 WHERE i.indrelid = 'telemetry_event'::regclass AND i.indisprimary`).Scan(&cols).Error; err != nil {
		t.Fatalf("pk: %v", err)
	}
	if cols != "event_day,record_uid" {
		t.Errorf("primary key columns = %q want event_day,record_uid", cols)
	}

	var colNames []string
	if err := testDB.Raw(`
		SELECT column_name FROM information_schema.columns
		 WHERE table_name = 'telemetry_event' ORDER BY ordinal_position`).Scan(&colNames).Error; err != nil {
		t.Fatalf("columns: %v", err)
	}
	wantCols := map[string]bool{
		"record_uid": true, "event_day": true, "app_id": true, "service_version": true,
		"environment": true, "event_name": true, "severity": true, "event_time": true,
		"session_id": true, "os_name": true, "os_version": true, "api_level": true,
		"device_model": true, "device_manufacturer": true, "host_arch": true,
		"sdk_version": true, "attributes": true, "body": true,
	}
	for _, c := range colNames {
		if !wantCols[c] {
			t.Errorf("unexpected column %s", c)
		}
		delete(wantCols, c)
	}
	if len(wantCols) > 0 {
		t.Errorf("missing columns %v", wantCols)
	}

	today := time.Now().UTC()
	base := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	for i := -30; i <= 3; i++ {
		name := "telemetry_event_p" + base.AddDate(0, 0, i).Format("20060102")
		var n int64
		if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relname = ?`, name).Scan(&n).Error; err != nil {
			t.Fatalf("partition %s: %v", name, err)
		}
		if n != 1 {
			t.Errorf("missing partition %s", name)
		}
	}
}
