package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRegistrationCounts_Integration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run (scripts/ephemeral-test-db.sh create <slug>)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewUserRepository(db)
	ctx := context.Background()
	const tz = "Asia/Shanghai"

	// Seed our own row: the queries read users.created_at, and an ephemeral
	// test DB has no ambient registrations to count.
	tag := time.Now().UnixNano() % 1e8
	seeded := &model.User{
		Name:  fmt.Sprintf("fedstat-%08d", tag),
		Email: fmt.Sprintf("fedstat-%d@test.local", tag),
	}
	if err := db.Create(seeded).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	defer db.Exec("DELETE FROM users WHERE id = ?", seeded.ID)

	byDay, err := repo.RegistrationCountsByDay(ctx, time.Now().AddDate(0, 0, -14), tz)
	if err != nil {
		t.Fatalf("RegistrationCountsByDay: %v", err)
	}
	if len(byDay) == 0 {
		t.Fatal("RegistrationCountsByDay returned no days despite a user created now")
	}
	t.Logf("byDay: %d days, e.g. %v", len(byDay), byDay)

	loc := time.FixedZone(tz, 8*3600)
	now := time.Now().In(loc)
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	byHour, err := repo.RegistrationCountsByHour(ctx, dayStart, dayStart.AddDate(0, 0, 1), tz)
	if err != nil {
		t.Fatalf("RegistrationCountsByHour: %v", err)
	}
	for h := range byHour {
		if h < 0 || h > 23 {
			t.Fatalf("hour out of range: %d", h)
		}
	}
	t.Logf("byHour today: %d buckets", len(byHour))

	total, err := repo.CountAll(ctx)
	if err != nil {
		t.Fatalf("CountAll: %v", err)
	}
	if total == 0 {
		t.Fatal("CountAll returned 0 despite a seeded user")
	}
	t.Logf("CountAll: %d", total)
}
