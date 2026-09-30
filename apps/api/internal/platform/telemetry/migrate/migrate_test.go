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

func TestPartitionedTableShape(t *testing.T) {
	var kind string
	if err := testDB.Raw(`SELECT relkind::text FROM pg_class WHERE relname = 'telemetry_event'`).Scan(&kind).Error; err != nil {
		t.Fatalf("relkind: %v", err)
	}
	if kind != "p" {
		t.Errorf("telemetry_event relkind=%q want p (partitioned)", kind)
	}

	today := "telemetry_event_p" + time.Now().UTC().Format("20060102")
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relname = ?`, today).Scan(&n).Error; err != nil {
		t.Fatalf("today partition: %v", err)
	}
	if n != 1 {
		t.Errorf("today's partition %s missing", today)
	}

	var cols string
	if err := testDB.Raw(`
		SELECT string_agg(a.attname, ',' ORDER BY array_position(i.indkey, a.attnum))
		  FROM pg_index i
		  JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		 WHERE i.indrelid = 'telemetry_event'::regclass AND i.indisprimary`).Scan(&cols).Error; err != nil {
		t.Fatalf("pk: %v", err)
	}
	if cols != "received_on,id" && !strings.Contains(cols, "received_on") {
		t.Errorf("primary key columns = %q want received_on,id", cols)
	}
	if cols != "received_on,id" {
		t.Errorf("primary key columns = %q want received_on,id", cols)
	}
}
