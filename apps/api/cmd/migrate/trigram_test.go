package main

import (
	"fmt"
	"strings"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUsersTrigramIndexesServeSubstringSearchAndHealInvalidBuilds(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pgcrypto`).Error; err != nil {
		dbtest.Skipf(t, "pgcrypto: %v", err)
	}
	if err := db.AutoMigrate(&authModel.User{}); err != nil {
		dbtest.Skipf(t, "migrate users: %v", err)
	}
	if err := ensureUsersTrigramIndexes(db); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := ensureUsersTrigramIndexes(db); err != nil {
		t.Fatalf("second run must be a no-op: %v", err)
	}

	tag := time.Now().UnixNano() % 1e8
	u := &authModel.User{Name: fmt.Sprintf("姬邵华%08d", tag), Email: fmt.Sprintf("tg%d@example.test", tag)}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM users WHERE id = ?`, u.ID) })

	var plan []string
	err = db.Transaction(func(tx *gorm.DB) error {
		tx.Exec(`SET LOCAL enable_seqscan = off`)
		return tx.Raw(`EXPLAIN SELECT id FROM users WHERE name ILIKE ?`, "%邵华%").Scan(&plan).Error
	})
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	if !strings.Contains(strings.Join(plan, "\n"), "idx_users_name_trgm") {
		t.Fatalf("name ILIKE does not use the trigram index:\n%s", strings.Join(plan, "\n"))
	}

	db.Exec(`UPDATE pg_index SET indisvalid = false WHERE indexrelid = 'idx_users_name_trgm'::regclass`)
	if err := ensureUsersTrigramIndexes(db); err != nil {
		t.Fatalf("heal run: %v", err)
	}
	var valid bool
	db.Raw(`SELECT i.indisvalid FROM pg_index i JOIN pg_class c ON c.oid = i.indexrelid WHERE c.relname = 'idx_users_name_trgm'`).Scan(&valid)
	if !valid {
		t.Fatal("an invalid trigram index was not rebuilt")
	}
}
