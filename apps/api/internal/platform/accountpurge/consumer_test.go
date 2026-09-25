package accountpurge

import (
	"context"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	authRepo "api/internal/platform/auth/repository"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("accountpurge")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("accountpurge", "cannot connect to test database: %v", err)
	}
	if err := db.AutoMigrate(&Cursor{}); err != nil {
		dbtest.SkipMainf("accountpurge", "migrate: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

type fakeFeed struct{ users []authRepo.DeletedUser }

func (f *fakeFeed) ListDeletedAfter(_ context.Context, at time.Time, id uint, limit int) ([]authRepo.DeletedUser, error) {
	var out []authRepo.DeletedUser
	for _, u := range f.users {
		if u.AnonymizedAt.After(at) || (u.AnonymizedAt.Equal(at) && u.ID > id) {
			out = append(out, u)
		}
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

func feedOf(n int) *fakeFeed {
	base := time.Date(2026, 9, 25, 3, 0, 0, 123456000, time.UTC)
	f := &fakeFeed{}
	for i := range n {
		f.users = append(f.users, authRepo.DeletedUser{
			ID: uint(1000 - i), AnonymizedAt: base.Add(time.Duration(i/2) * time.Microsecond),
		})
	}
	sort.Slice(f.users, func(a, b int) bool {
		ua, ub := f.users[a], f.users[b]
		return ua.AnonymizedAt.Before(ub.AnonymizedAt) || (ua.AnonymizedAt.Equal(ub.AnonymizedAt) && ua.ID < ub.ID)
	})
	return f
}

func reset(t *testing.T) {
	t.Helper()
	if err := testDB.Exec("TRUNCATE account_purge_cursor").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
}

func TestEveryAccountIsPurgedOnceAcrossPagesAndRuns(t *testing.T) {
	reset(t)
	feed := feedOf(pageSize*2 + 5)
	seen := map[int64]int{}
	c := &Consumer{Name: "catalog", Feed: feed, DB: testDB, Purge: func(_ context.Context, uid int64) error {
		seen[uid]++
		return nil
	}}

	n, err := c.Run(context.Background())
	if err != nil || n != len(feed.users) {
		t.Fatalf("first run: purged %d, err %v; want %d", n, err, len(feed.users))
	}
	for _, u := range feed.users {
		if seen[int64(u.ID)] != 1 {
			t.Fatalf("user %d purged %d times", u.ID, seen[int64(u.ID)])
		}
	}

	n, err = c.Run(context.Background())
	if err != nil || n != 0 {
		t.Fatalf("a rerun must purge nothing, purged %d, err %v", n, err)
	}

	late := authRepo.DeletedUser{ID: 7, AnonymizedAt: feed.users[len(feed.users)-1].AnonymizedAt.Add(time.Second)}
	feed.users = append(feed.users, late)
	n, err = c.Run(context.Background())
	if err != nil || n != 1 || seen[7] != 1 {
		t.Fatalf("a later deletion must be picked up once, purged %d, seen %d, err %v", n, seen[7], err)
	}
}

func TestAFailedPurgeHoldsTheCursorForTheNextRun(t *testing.T) {
	reset(t)
	feed := feedOf(3)
	failing := int64(feed.users[1].ID)
	var calls []int64
	broken := true
	c := &Consumer{Name: "community", Feed: feed, DB: testDB, Purge: func(_ context.Context, uid int64) error {
		calls = append(calls, uid)
		if uid == failing && broken {
			return errors.New("database went away")
		}
		return nil
	}}

	n, err := c.Run(context.Background())
	if err == nil || n != 1 {
		t.Fatalf("first run: purged %d, err %v; want 1 and an error", n, err)
	}
	broken = false
	n, err = c.Run(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("second run: purged %d, err %v; want 2", n, err)
	}
	want := []int64{int64(feed.users[0].ID), failing, failing, int64(feed.users[2].ID)}
	if len(calls) != len(want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls %v, want %v", calls, want)
		}
	}
}

func TestTheCursorNeverMovesBackAndIsPerConsumer(t *testing.T) {
	reset(t)
	c := &Consumer{Name: "catalog", DB: testDB}
	other := &Consumer{Name: "community", DB: testDB}
	ctx := context.Background()
	later := time.Date(2026, 9, 25, 4, 0, 0, 0, time.UTC)

	if err := c.advance(ctx, later, 5); err != nil {
		t.Fatal(err)
	}
	if err := c.advance(ctx, later.Add(-time.Minute), 9); err != nil {
		t.Fatal(err)
	}
	at, id, err := c.position(ctx)
	if err != nil || !at.Equal(later) || id != 5 {
		t.Fatalf("an older position overwrote the cursor: %v %d %v", at, id, err)
	}
	at, id, err = other.position(ctx)
	if err != nil || !at.Equal(time.Unix(0, 0)) || id != 0 {
		t.Fatalf("a fresh consumer must start at the beginning, got %v %d %v", at, id, err)
	}
}
