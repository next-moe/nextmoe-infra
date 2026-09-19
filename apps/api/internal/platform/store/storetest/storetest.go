// Package storetest provisions a test database carrying the store tables.
package storetest

import (
	"context"
	"database/sql"
	"fmt"
	"os"

	"api/internal/platform/store/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Advisory-lock keys share ONE keyspace across the whole instance, so this must
// stay distinct from every other suite's ("stor" in ASCII; see news/dbtest).
const suiteLockKey = 0x73746f72

// Open connects to the test database and AutoMigrates the store models, which
// is exactly what `go run ./cmd/migrate` does for them in production.
//
// A missing database SKIPS the package — and a skipped package still reports
// ok, so acceptance for any DB-backed change must run with an explicit
// TEST_DATABASE_DSN and read the -v PASS counts, never the ok line.
func Open() (*gorm.DB, func(), bool) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("store suite")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Fprintf(os.Stderr, "SKIP: cannot connect to test database: %v\n", err)
		return nil, nil, false
	}
	sqlDB, _ := db.DB()
	release := acquireSuiteLock(sqlDB)

	if err := model.RekeyCouponsByOwner(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "SKIP: store coupon re-key failed: %v\n", err)
		return nil, nil, false
	}
	if err := db.AutoMigrate(model.AllModels()...); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "SKIP: store migration failed: %v\n", err)
		return nil, nil, false
	}
	return db, release, true
}

func Truncate(db *gorm.DB) error {
	return db.Exec(`TRUNCATE store_price_quotes, store_link_daily_stats, store_coupon_links,
		store_purchase_links, store_campaigns, store_coupon_batches, store_coupons,
		store_coupon_shares RESTART IDENTITY CASCADE`).Error
}

func acquireSuiteLock(db *sql.DB) func() {
	if db == nil {
		return func() {}
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return func() {}
	}
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", suiteLockKey); err != nil {
		_ = conn.Close()
		return func() {}
	}
	return func() {
		_, _ = conn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", suiteLockKey)
		_ = conn.Close()
	}
}
