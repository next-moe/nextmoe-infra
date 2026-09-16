package service

import (
	"context"
	"os"
	"sync"
	"testing"

	suitelock "api/internal/platform/community/dbtest"
	"api/internal/platform/community/migrate"
	"api/internal/platform/community/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("community/service")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("community/service", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("community/service", "community migration failed: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"community_notification", "community_event",
		"community_review_item", "community_flag", "community_reaction",
		"community_anchor_user", "community_thread_user", "community_board", "community_trust",
		"community_post", "community_thread",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

type recordingSink struct {
	mu     sync.Mutex
	events []Event
}

func (r *recordingSink) Emit(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
}

func (r *recordingSink) count(kind string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, e := range r.events {
		if e.Kind == kind {
			n++
		}
	}
	return n
}

func seedTrust(t *testing.T, userID int64, level int16, holds int32) {
	t.Helper()
	if err := testDB.Exec(
		`INSERT INTO community_trust (user_id, level, first_posts_held_remaining) VALUES (?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET level = EXCLUDED.level,
		   first_posts_held_remaining = EXCLUDED.first_posts_held_remaining`,
		userID, level, holds,
	).Error; err != nil {
		t.Fatalf("seed trust: %v", err)
	}
}

func getThread(t *testing.T, id int64) *model.CommunityThread {
	t.Helper()
	var th model.CommunityThread
	if err := testDB.First(&th, id).Error; err != nil {
		t.Fatalf("reload thread %d: %v", id, err)
	}
	return &th
}

func getTrust(t *testing.T, userID int64) *model.CommunityTrust {
	t.Helper()
	var tr model.CommunityTrust
	if err := testDB.Where("user_id = ?", userID).First(&tr).Error; err != nil {
		t.Fatalf("get trust %d: %v", userID, err)
	}
	return &tr
}

func setLikes(t *testing.T, userID int64, given, received int32) {
	t.Helper()
	if err := testDB.Exec(
		`UPDATE community_trust SET likes_given = ?, likes_received = ? WHERE user_id = ?`,
		given, received, userID,
	).Error; err != nil {
		t.Fatalf("set likes: %v", err)
	}
}

func getPost(t *testing.T, id int64) *model.CommunityPost {
	t.Helper()
	var p model.CommunityPost
	if err := testDB.First(&p, id).Error; err != nil {
		t.Fatalf("reload post %d: %v", id, err)
	}
	return &p
}

func openTopic(t *testing.T, ts *ThreadService, site string, author int64, anchorID, body string) *model.CommunityThread {
	t.Helper()
	seedTrust(t, author, model.TrustLevelBasic, 0)
	th, _, err := ts.OpenTopic(context.Background(), OpenTopicParams{
		Site: site, AuthorID: author, BoardID: testBoard(t, site, anchorID),
		Title: "t", ContentRating: model.ContentRatingAll, BodyRaw: body,
	})
	if err != nil {
		t.Fatalf("open topic: %v", err)
	}
	return th
}

func testBoard(t *testing.T, site, slug string) int64 {
	t.Helper()
	id, err := suitelock.Board(testDB, site, slug)
	if err != nil {
		t.Fatalf("board %s/%s: %v", site, slug, err)
	}
	return id
}
