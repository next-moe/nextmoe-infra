package repository

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	siteModel "api/internal/platform/site/model"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openActivityDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run (scripts/ephemeral-test-db.sh create <slug>)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&siteModel.Role{}, &model.User{}, &model.Session{}, &model.UserActiveHour{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedActivityUser(t *testing.T, db *gorm.DB, prefix string) *model.User {
	t.Helper()
	tag := time.Now().UnixNano() % 1e8
	u := &model.User{Name: fmt.Sprintf("%s-%08d", prefix, tag), Email: fmt.Sprintf("%s-%d@test.local", prefix, tag)}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		db.Exec("DELETE FROM user_active_hours WHERE user_id = ?", u.ID)
		db.Exec("DELETE FROM sessions WHERE user_id = ?", u.ID)
		db.Exec("DELETE FROM user_roles WHERE user_id = ?", u.ID)
		db.Exec("DELETE FROM users WHERE id = ?", u.ID)
	})
	return u
}

func TestSessionLifecycleRecordsActivity(t *testing.T) {
	db := openActivityDB(t)
	ctx := context.Background()
	sessions := NewSessionRepository(db)
	activity := NewActivityRepository(db)
	u := seedActivityUser(t, db, "act")

	s := &model.Session{UserID: u.ID, SessionToken: "at-" + u.Name, RefreshToken: "rt-" + u.Name, ExpiresAt: time.Now().Add(time.Hour)}
	if err := sessions.Create(ctx, s); err != nil {
		t.Fatalf("create session: %v", err)
	}

	var got model.User
	db.First(&got, u.ID)
	if got.LastLoginAt == nil || got.LastActiveAt == nil {
		t.Fatalf("sign-in not recorded: login=%v active=%v", got.LastLoginAt, got.LastActiveAt)
	}
	firstLogin := *got.LastLoginAt

	now := time.Now()
	won, err := sessions.RotateRefreshToken(ctx, s.ID, s.RefreshToken, "at2-"+u.Name, "rt2-"+u.Name, now, now.Add(time.Hour))
	if err != nil || !won {
		t.Fatalf("rotate: won=%v err=%v", won, err)
	}
	if err := sessions.TouchLastUsed(ctx, s.ID, time.Now()); err != nil {
		t.Fatalf("touch: %v", err)
	}

	var hours int64
	db.Model(&model.UserActiveHour{}).Where("user_id = ?", u.ID).Count(&hours)
	if hours != 1 {
		t.Fatalf("active hours = %d, want 1 (same hour must collapse)", hours)
	}
	db.First(&got, u.ID)
	if !got.LastLoginAt.Equal(firstLogin) {
		t.Fatalf("a refresh moved last_login_at: %v -> %v", firstLogin, *got.LastLoginAt)
	}

	tz := "Asia/Shanghai"
	loc := time.FixedZone(tz, 8*3600)
	today := time.Now().In(loc)
	dayStart := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, loc)
	byDay, err := activity.ActiveCountsByDay(ctx, dayStart, tz)
	if err != nil {
		t.Fatalf("by day: %v", err)
	}
	if byDay[dayStart.Format("2006-01-02")] < 1 {
		t.Fatalf("today's active count missing our user: %v", byDay)
	}
	byHour, err := activity.ActiveCountsByHour(ctx, dayStart, dayStart.AddDate(0, 0, 1), tz)
	if err != nil {
		t.Fatalf("by hour: %v", err)
	}
	if byHour[today.Hour()] < 1 {
		t.Fatalf("this hour's active count missing our user: %v", byHour)
	}
	n, err := activity.UniqueActive(ctx, dayStart, dayStart.AddDate(0, 0, 1))
	if err != nil || n < 1 {
		t.Fatalf("unique = %d err=%v", n, err)
	}
}

func TestPruneDropsOnlyOldActivity(t *testing.T) {
	db := openActivityDB(t)
	ctx := context.Background()
	u := seedActivityUser(t, db, "prune")
	old := time.Now().Add(-activeHourRetention - 24*time.Hour).Truncate(time.Hour)
	recent := time.Now().Truncate(time.Hour)
	db.Create(&[]model.UserActiveHour{{UserID: u.ID, Hour: old}, {UserID: u.ID, Hour: recent}})

	if err := NewActivityRepository(db).Prune(ctx); err != nil {
		t.Fatalf("prune: %v", err)
	}
	var left []model.UserActiveHour
	db.Where("user_id = ?", u.ID).Find(&left)
	if len(left) != 1 || !left[0].Hour.Equal(recent) {
		t.Fatalf("after prune = %+v, want only the recent hour", left)
	}
}

func TestFindAllPaginatedFiltersByRole(t *testing.T) {
	db := openActivityDB(t)
	ctx := context.Background()
	users := NewUserRepository(db)
	plain := seedActivityUser(t, db, "rplain")
	staff := seedActivityUser(t, db, "rstaff")

	role := siteModel.Role{Name: "moderator"}
	if err := db.Where("name = ?", role.Name).FirstOrCreate(&role).Error; err != nil {
		t.Fatalf("role: %v", err)
	}
	if err := db.Exec("INSERT INTO user_roles (user_id, role_id) VALUES (?, ?)", staff.ID, role.ID).Error; err != nil {
		t.Fatalf("grant: %v", err)
	}

	ids := func(role string) map[uint]bool {
		t.Helper()
		out := map[uint]bool{}
		got, _, err := users.FindAllPaginated(ctx, 1, 100, "rplain", nil, role, "created_at", true)
		if err != nil {
			t.Fatalf("list %q: %v", role, err)
		}
		more, _, err := users.FindAllPaginated(ctx, 1, 100, "rstaff", nil, role, "created_at", true)
		if err != nil {
			t.Fatalf("list %q: %v", role, err)
		}
		for _, u := range append(got, more...) {
			out[u.ID] = true
		}
		return out
	}

	if got := ids("user"); !got[plain.ID] || got[staff.ID] {
		t.Fatalf("role=user: %v (plain %d, staff %d)", got, plain.ID, staff.ID)
	}
	if got := ids("moderator"); got[plain.ID] || !got[staff.ID] {
		t.Fatalf("role=moderator: %v (plain %d, staff %d)", got, plain.ID, staff.ID)
	}
	if got := ids(""); !got[plain.ID] || !got[staff.ID] {
		t.Fatalf("no role filter: %v", got)
	}
}
