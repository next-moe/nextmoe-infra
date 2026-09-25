package main

import (
	"context"
	"os"
	"testing"
	"time"

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
		dbtest.SkipMain("cmd/import-follows")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/import-follows", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("cmd/import-follows", "community migration failed: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS user_follow_relation (
			id serial PRIMARY KEY, follower_id int, following_id int);
		CREATE TABLE IF NOT EXISTS follow (
			id bigserial PRIMARY KEY, follower_id bigint, followee_id bigint, created timestamptz)`).Error; err != nil {
		release()
		dbtest.SkipMainf("cmd/import-follows", "create source tables: %v", err)
	}
	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func clean(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"community_user_follow", "user_follow_relation", "follow",
		"community_event", "community_notification",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

func mustRun(t *testing.T, opt options) report {
	t.Helper()
	rep, err := run(context.Background(), testDB, testDB, opt)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return rep
}

func countFollows(t *testing.T) int64 {
	t.Helper()
	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestImportDryRunWritesNothing(t *testing.T) {
	clean(t)
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 2), (1, 3)`).Error; err != nil {
		t.Fatalf("seed moyu: %v", err)
	}
	rep := mustRun(t, options{Source: "moyu", Batch: 1000})
	if rep.Applied || rep.Inserted != 0 || rep.ToInsert != 2 || rep.SourceEdges != 2 {
		t.Fatalf("dry run report: %+v", rep)
	}
	if countFollows(t) != 0 {
		t.Fatal("dry run wrote rows")
	}
}

func TestImportMoyuApplyThenRerunIsNoop(t *testing.T) {
	clean(t)
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 2), (3, 4), (5, 6)`).Error; err != nil {
		t.Fatalf("seed moyu: %v", err)
	}
	rep := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	if !rep.Applied || rep.Inserted != 3 || rep.ToInsert != 3 || rep.AlreadyPresent != 0 || rep.Raced != 0 {
		t.Fatalf("apply report: %+v", rep)
	}
	var rows []model.CommunityUserFollow
	if err := testDB.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	want := [][2]int64{{1, 2}, {3, 4}, {5, 6}}
	for i, row := range rows {
		if row.FollowerID != want[i][0] || row.FolloweeID != want[i][1] {
			t.Fatalf("id order row %d: %+v, want %v", i, row, want[i])
		}
		if row.OriginSite != "moyu" {
			t.Fatalf("origin=%q", row.OriginSite)
		}
		if row.CreatedAt != nil {
			t.Fatal("moyu created_at must be null")
		}
		if row.ImportedAt == nil {
			t.Fatal("imported_at must be set")
		}
	}

	again := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	if again.Inserted != 0 || again.AlreadyPresent != 3 || again.ToInsert != 0 {
		t.Fatalf("rerun: %+v", again)
	}
	if countFollows(t) != 3 {
		t.Fatal("rerun changed row count")
	}
}

func TestImportLetmoeKeepsCreatedAt(t *testing.T) {
	clean(t)
	created := time.Date(2024, 6, 1, 8, 0, 0, 0, time.UTC)
	if err := testDB.Exec(`INSERT INTO follow (follower_id, followee_id, created) VALUES (1, 2, ?)`, created).Error; err != nil {
		t.Fatalf("seed letmoe: %v", err)
	}
	rep := mustRun(t, options{Source: "letmoe", Apply: true, Batch: 1000})
	if rep.Inserted != 1 {
		t.Fatalf("insert: %+v", rep)
	}
	var row model.CommunityUserFollow
	if err := testDB.Take(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.OriginSite != "letmoe" || row.CreatedAt == nil || !row.CreatedAt.UTC().Equal(created) {
		t.Fatalf("created_at=%v origin=%q", row.CreatedAt, row.OriginSite)
	}
	if row.ImportedAt == nil {
		t.Fatal("imported_at must be set")
	}
}

func TestImportSkipsSelfFollows(t *testing.T) {
	clean(t)
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 1), (1, 2)`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	rep := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	if rep.SelfSkipped != 1 || rep.ToInsert != 1 || rep.Inserted != 1 || rep.SourceEdges != 2 {
		t.Fatalf("report: %+v", rep)
	}
	var n int64
	if err := testDB.Model(&model.CommunityUserFollow{}).Where("follower_id = followee_id").Count(&n).Error; err != nil {
		t.Fatalf("count self: %v", err)
	}
	if n != 0 {
		t.Fatal("a self-follow was imported")
	}
	if countFollows(t) != 1 {
		t.Fatal("want the one non-self edge")
	}
}

func TestImportLeavesExistingEdgesAlone(t *testing.T) {
	clean(t)
	nativeAt := time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC)
	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at, imported_at)
		VALUES (1, 2, 'kungal', ?, NULL)`, nativeAt).Error; err != nil {
		t.Fatalf("seed native: %v", err)
	}
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 2), (3, 4)`).Error; err != nil {
		t.Fatalf("seed moyu: %v", err)
	}
	rep := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	if rep.AlreadyPresent != 1 || rep.Inserted != 1 || rep.ToInsert != 1 {
		t.Fatalf("report: %+v", rep)
	}
	var native model.CommunityUserFollow
	if err := testDB.Where("follower_id = ? AND followee_id = ?", 1, 2).Take(&native).Error; err != nil {
		t.Fatalf("reload native: %v", err)
	}
	if native.OriginSite != "kungal" || native.ImportedAt != nil || native.CreatedAt == nil || !native.CreatedAt.UTC().Equal(nativeAt) {
		t.Fatalf("native edge was rewritten: %+v", native)
	}
}

func TestImportPruneStale(t *testing.T) {
	clean(t)
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 2), (1, 3)`).Error; err != nil {
		t.Fatalf("seed moyu: %v", err)
	}
	if rep := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000}); rep.Inserted != 2 {
		t.Fatalf("first apply: %+v", rep)
	}
	nativeAt := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)
	importedAt := time.Date(2024, 2, 2, 0, 0, 0, 0, time.UTC)
	if err := testDB.Exec(`
		INSERT INTO community_user_follow (follower_id, followee_id, origin_site, created_at, imported_at)
		VALUES (10, 11, 'kungal', ?, NULL), (12, 13, 'letmoe', ?, ?)`,
		nativeAt, importedAt, importedAt).Error; err != nil {
		t.Fatalf("seed bystanders: %v", err)
	}
	if err := testDB.Exec(`DELETE FROM user_follow_relation WHERE follower_id = 1 AND following_id = 3`).Error; err != nil {
		t.Fatalf("remove source pair: %v", err)
	}

	dry := mustRun(t, options{Source: "moyu", PruneStale: true, Batch: 1000})
	if dry.Stale != 1 || dry.Pruned != 0 || dry.Applied {
		t.Fatalf("dry prune: %+v", dry)
	}
	if countFollows(t) != 4 {
		t.Fatal("dry run deleted rows")
	}

	noPrune := mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	if noPrune.Stale != 1 || noPrune.Pruned != 0 {
		t.Fatalf("apply without prune: %+v", noPrune)
	}
	if countFollows(t) != 4 {
		t.Fatal("apply without prune deleted rows")
	}

	pruned := mustRun(t, options{Source: "moyu", Apply: true, PruneStale: true, Batch: 1000})
	if pruned.Stale != 1 || pruned.Pruned != 1 {
		t.Fatalf("prune: %+v", pruned)
	}
	var leftover []model.CommunityUserFollow
	if err := testDB.Order("follower_id, followee_id").Find(&leftover).Error; err != nil {
		t.Fatalf("leftover: %v", err)
	}
	if len(leftover) != 3 {
		t.Fatalf("want 3 leftover rows, got %+v", leftover)
	}
	got := map[[2]int64]string{}
	for _, r := range leftover {
		got[[2]int64{r.FollowerID, r.FolloweeID}] = r.OriginSite
	}
	if got[[2]int64{1, 2}] != "moyu" || got[[2]int64{10, 11}] != "kungal" || got[[2]int64{12, 13}] != "letmoe" {
		t.Fatalf("leftover origins: %v", got)
	}
	if _, ok := got[[2]int64{1, 3}]; ok {
		t.Fatal("stale moyu pair survived prune")
	}
}

func TestImportEnqueuesNothing(t *testing.T) {
	clean(t)
	if err := testDB.Exec(`INSERT INTO user_follow_relation (follower_id, following_id) VALUES (1, 2)`).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	mustRun(t, options{Source: "moyu", Apply: true, Batch: 1000})
	var events, notifs int64
	if err := testDB.Raw(`SELECT count(*) FROM community_event`).Scan(&events).Error; err != nil {
		t.Fatalf("events: %v", err)
	}
	if err := testDB.Raw(`SELECT count(*) FROM community_notification`).Scan(&notifs).Error; err != nil {
		t.Fatalf("notifications: %v", err)
	}
	if events != 0 || notifs != 0 {
		t.Fatalf("importer wrote events=%d notifications=%d", events, notifs)
	}
}
