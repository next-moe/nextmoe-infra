package main

import (
	"fmt"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserActivityBackfillFromSessionsRunsOnce(t *testing.T) {
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
	if err := db.AutoMigrate(&authModel.User{}, &authModel.Session{}); err != nil {
		dbtest.Skipf(t, "migrate: %v", err)
	}
	if err := db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS last_login_at, DROP COLUMN IF EXISTS last_active_at`).Error; err != nil {
		t.Fatalf("drop the columns: %v", err)
	}

	tag := time.Now().UnixNano() % 1e8
	seen := fmt.Sprintf("act%08d", tag)
	unseen := fmt.Sprintf("noact%08d", tag)
	for _, name := range []string{seen, unseen} {
		if err := db.Exec(`INSERT INTO users (name, email) VALUES (?, ?)`, name, name+"@example.test").Error; err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM sessions WHERE user_id IN (SELECT id FROM users WHERE name IN (?, ?))`, seen, unseen)
		db.Exec(`DELETE FROM users WHERE name IN (?, ?)`, seen, unseen)
	})

	now := time.Now().UTC().Truncate(time.Second)
	ssoAuth, ssoUsed := now.Add(-72*time.Hour), now.Add(-24*time.Hour)
	siteSignIn, siteRotated := now.Add(-48*time.Hour), now.Add(-2*time.Hour)
	if err := db.Exec(`
INSERT INTO sessions (user_id, client_id, session_token, refresh_token, auth_time, last_used_at, created_at, expires_at)
SELECT id, '', ?, ?, ?, ?, ?, ? FROM users WHERE name = ?`,
		"at-sso-"+seen, "rt-sso-"+seen, ssoAuth, ssoUsed, ssoAuth, now.Add(time.Hour), seen).Error; err != nil {
		t.Fatalf("seed sso session: %v", err)
	}
	if err := db.Exec(`
INSERT INTO sessions (user_id, client_id, session_token, refresh_token, rotated_at, created_at, expires_at)
SELECT id, 'kungal', ?, ?, ?, ?, ? FROM users WHERE name = ?`,
		"at-site-"+seen, "rt-site-"+seen, siteRotated, siteSignIn, now.Add(time.Hour), seen).Error; err != nil {
		t.Fatalf("seed site session: %v", err)
	}

	if err := authModel.AddUserActivityColumns(db); err != nil {
		t.Fatalf("first run: %v", err)
	}

	type row struct {
		LastLoginAt  *time.Time
		LastActiveAt *time.Time
	}
	read := func(name string) row {
		var r row
		if err := db.Raw(`SELECT last_login_at, last_active_at FROM users WHERE name = ?`, name).Scan(&r).Error; err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return r
	}

	got := read(seen)
	if got.LastLoginAt == nil || !got.LastLoginAt.Equal(siteSignIn) {
		t.Fatalf("last_login_at = %v, want the newer sign-in %v", got.LastLoginAt, siteSignIn)
	}
	if got.LastActiveAt == nil || !got.LastActiveAt.Equal(siteRotated) {
		t.Fatalf("last_active_at = %v, want the latest rotation %v", got.LastActiveAt, siteRotated)
	}
	if r := read(unseen); r.LastLoginAt != nil || r.LastActiveAt != nil {
		t.Fatalf("an account with no session got %+v, want NULLs", r)
	}

	if err := db.Exec(`UPDATE users SET last_login_at = NULL WHERE name = ?`, seen).Error; err != nil {
		t.Fatalf("clear: %v", err)
	}
	if err := authModel.AddUserActivityColumns(db); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if r := read(seen); r.LastLoginAt != nil {
		t.Fatalf("second run backfilled again: %v", r.LastLoginAt)
	}
}
