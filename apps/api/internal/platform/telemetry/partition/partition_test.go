package partition_test

import (
	"os"
	"testing"
	"time"

	suitelock "api/internal/platform/telemetry/dbtest"
	"api/internal/platform/telemetry/migrate"
	"api/internal/platform/telemetry/partition"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("telemetry/partition")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("telemetry/partition", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("telemetry/partition", "telemetry migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func TestEnsureWindow(t *testing.T) {
	now := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	if err := partition.EnsurePartitions(testDB, now); err != nil {
		t.Fatal(err)
	}
	today := time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	n := 0
	for i := -30; i <= 0; i++ {
		assertRel(t, partition.Name(today.AddDate(0, 0, i)), 1)
		n++
	}
	if n != 31 {
		t.Errorf("lookback+today count=%d want 31", n)
	}
	fwd := 0
	for i := 0; i <= 3; i++ {
		assertRel(t, partition.Name(today.AddDate(0, 0, i)), 1)
		fwd++
	}
	if fwd != 4 {
		t.Errorf("today..+3 count=%d want 4", fwd)
	}
}

func TestDropExpired(t *testing.T) {
	now := time.Date(2026, 9, 29, 8, 0, 0, 0, time.UTC)
	if err := partition.EnsurePartitions(testDB, now); err != nil {
		t.Fatal(err)
	}
	old := now.AddDate(0, 0, -31)
	keepEdge := now.AddDate(0, 0, -30)
	if err := partition.EnsureDay(testDB, old); err != nil {
		t.Fatal(err)
	}
	if err := partition.EnsureDay(testDB, keepEdge); err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`CREATE TABLE IF NOT EXISTS telemetry_event_legacy PARTITION OF telemetry_event FOR VALUES FROM ('1990-01-01') TO ('1990-01-02')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := partition.DropExpiredPartitions(testDB, now); err != nil {
		t.Fatal(err)
	}
	assertRel(t, partition.Name(old), 0)
	assertRel(t, partition.Name(keepEdge), 1)
	assertRel(t, "telemetry_event_legacy", 1)
	assertRel(t, partition.Name(now), 1)
	if err := testDB.Exec(`DROP TABLE telemetry_event_legacy`).Error; err != nil {
		t.Fatal(err)
	}
}

func assertRel(t *testing.T, name string, want int64) {
	t.Helper()
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relname = ?`, name).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != want {
		t.Errorf("%s count=%d want %d", name, n, want)
	}
}
