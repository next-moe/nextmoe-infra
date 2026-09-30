package handler

import (
	"os"
	"testing"

	suitelock "api/internal/platform/telemetry/dbtest"
	"api/internal/platform/telemetry/migrate"
	"api/internal/platform/telemetry/store"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB *gorm.DB
	st     *store.Store
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("telemetry/handler")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("telemetry/handler", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("telemetry/handler", "telemetry migration failed: %v", err)
	}
	testDB = db
	st = store.New(db)
	code := m.Run()
	release()
	os.Exit(code)
}
