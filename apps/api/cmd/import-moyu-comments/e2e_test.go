package main

import (
	"os"
	"strconv"
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

const (
	e2eSite      = "moyu_import_e2e"
	e2eSrcSchema = "moyu_import_e2e_src"
)

var (
	testDB *gorm.DB
	srcDB  *gorm.DB
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("cmd/import-moyu-comments")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("cmd/import-moyu-comments", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	quit := func(msg string, err error) {
		release()
		dbtest.SkipMainf("cmd/import-moyu-comments", "%s: %v", msg, err)
	}
	if err := migrate.Run(db); err != nil {
		quit("community migration failed", err)
	}
	if err := db.Exec("CREATE SCHEMA IF NOT EXISTS " + e2eSrcSchema).Error; err != nil {
		quit("create source schema failed", err)
	}
	src, err := gorm.Open(postgres.Open(dsn+" search_path="+e2eSrcSchema),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		quit("connect source schema failed", err)
	}
	if err := createSourceTables(src); err != nil {
		quit("create source scratch tables failed", err)
	}
	testDB, srcDB = db, src

	code := m.Run()

	_ = db.Exec("DROP SCHEMA IF EXISTS " + e2eSrcSchema + " CASCADE").Error
	release()
	os.Exit(code)
}

func createSourceTables(db *gorm.DB) error {
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS patch_comment (
			id int PRIMARY KEY, content varchar NOT NULL, edit text NOT NULL DEFAULT '',
			parent_id int, user_id int NOT NULL, galgame_id int NOT NULL, resource_id int,
			like_count int NOT NULL DEFAULT 0, status int NOT NULL DEFAULT 0,
			created timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now())`,
		`CREATE TABLE IF NOT EXISTS user_patch_comment_like_relation (
			id serial PRIMARY KEY, user_id int NOT NULL, comment_id int NOT NULL,
			created timestamptz NOT NULL DEFAULT now(), updated timestamptz NOT NULL DEFAULT now())`,
	} {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func resetE2E(t *testing.T) {
	t.Helper()
	for _, stmt := range []string{
		"TRUNCATE patch_comment",
		"TRUNCATE user_patch_comment_like_relation",
		"DROP TABLE IF EXISTS patch_comment_community_map",
	} {
		if err := srcDB.Exec(stmt).Error; err != nil {
			t.Fatalf("reset source (%s): %v", stmt, err)
		}
	}
	if err := testDB.Exec(
		"DELETE FROM community_post WHERE thread_id IN (SELECT id FROM community_thread WHERE site = ?)", e2eSite,
	).Error; err != nil {
		t.Fatalf("reset posts: %v", err)
	}
	for _, stmt := range []string{
		"DELETE FROM community_thread WHERE site = ?",
	} {
		if err := testDB.Exec(stmt, e2eSite).Error; err != nil {
			t.Fatalf("reset target: %v", err)
		}
	}
	if err := testDB.Exec("DELETE FROM community_trust WHERE user_id BETWEEN 90000 AND 90099").Error; err != nil {
		t.Fatalf("reset trust: %v", err)
	}
}

func insertComment(t *testing.T, id, game int, resource *int, user int, parent *int, body, edit string, created time.Time) {
	t.Helper()
	if err := srcDB.Exec(
		`INSERT INTO patch_comment (id, content, edit, parent_id, user_id, galgame_id, resource_id, created, updated)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, body, edit, parent, user, game, resource, created, created,
	).Error; err != nil {
		t.Fatalf("insert comment %d: %v", id, err)
	}
}

func TestImportMoyuComments(t *testing.T) {
	resetE2E(t)

	base := time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)
	resource := 77
	edited := base.Add(2 * time.Hour).Format(time.RFC3339)

	// Game wall: a root and a reply.
	insertComment(t, 1, 500, nil, 90001, nil, "root on the game", "", base)
	one := 1
	insertComment(t, 2, 500, nil, 90002, &one, "reply on the game", edited, base.Add(time.Minute))
	// The SAME game, but a resource under it: a second wall, not more of the first.
	insertComment(t, 3, 500, &resource, 90003, nil, "on the resource", "", base.Add(2*time.Minute))
	// A second game, edited in moyu's older format: Date.now() milliseconds.
	editedMillis := base.Add(3 * time.Hour)
	insertComment(t, 4, 501, nil, 90001, nil, "another game",
		strconv.FormatInt(editedMillis.UnixMilli(), 10), base.Add(3*time.Minute))

	if err := srcDB.Exec(
		`INSERT INTO user_patch_comment_like_relation (user_id, comment_id, created) VALUES (?, ?, ?), (?, ?, ?)`,
		90002, 1, base, 90003, 1, base,
	).Error; err != nil {
		t.Fatalf("insert likes: %v", err)
	}

	dry, err := run(srcDB, testDB, e2eSite, false)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.ThreadsCreated != 3 || dry.PostsInserted != 4 {
		t.Fatalf("dry run: want 3 threads / 4 posts, got %d / %d", dry.ThreadsCreated, dry.PostsInserted)
	}
	if dry.GameWalls != 2 || dry.ResourceWalls != 1 {
		t.Fatalf("dry run: want 2 game walls and 1 resource wall, got %d / %d", dry.GameWalls, dry.ResourceWalls)
	}
	// The run we read before applying must not report every like as lost just
	// because no post ids exist yet.
	if dry.LikesToInsert != 2 || dry.LikesOrphaned != 0 {
		t.Fatalf("dry run: want 2 likes to insert and 0 orphans, got %d / %d", dry.LikesToInsert, dry.LikesOrphaned)
	}
	var wrote int64
	if err := testDB.Raw("SELECT count(*) FROM community_thread WHERE site = ?", e2eSite).Scan(&wrote).Error; err != nil {
		t.Fatalf("count after dry run: %v", err)
	}
	if wrote != 0 {
		t.Fatalf("a dry run must write nothing, found %d threads", wrote)
	}

	rep, err := run(srcDB, testDB, e2eSite, true)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.ThreadsCreated != 3 || rep.PostsInserted != 4 || rep.LedgerRows != 4 {
		t.Fatalf("apply: want 3/4/4, got %d/%d/%d", rep.ThreadsCreated, rep.PostsInserted, rep.LedgerRows)
	}
	if rep.LikesInserted != 2 {
		t.Fatalf("apply: want 2 likes, got %d", rep.LikesInserted)
	}

	// The two walls of game 500 are separate threads, on the anchor kinds that
	// name them, with moyu's own bare ids.
	var anchors []struct {
		AnchorKind int16  `gorm:"column:anchor_kind"`
		AnchorID   string `gorm:"column:anchor_id"`
		Posts      int32  `gorm:"column:posts_count"`
	}
	if err := testDB.Raw(
		"SELECT anchor_kind, anchor_id, posts_count FROM community_thread WHERE site = ? ORDER BY anchor_kind, anchor_id",
		e2eSite).Scan(&anchors).Error; err != nil {
		t.Fatalf("read anchors: %v", err)
	}
	want := []struct {
		kind  int16
		id    string
		posts int32
	}{
		{model.AnchorKindSiteGame, "500", 2},
		{model.AnchorKindSiteGame, "501", 1},
		{model.AnchorKindSiteResource, "77", 1},
	}
	if len(anchors) != len(want) {
		t.Fatalf("want %d threads, got %d (%+v)", len(want), len(anchors), anchors)
	}
	for i, w := range want {
		if anchors[i].AnchorKind != w.kind || anchors[i].AnchorID != w.id || anchors[i].Posts != w.posts {
			t.Fatalf("thread[%d]: want %d/%q/%d, got %d/%q/%d", i,
				w.kind, w.id, w.posts, anchors[i].AnchorKind, anchors[i].AnchorID, anchors[i].Posts)
		}
	}

	// The reply points at the root through the NEW ids, and addresses its author.
	var reply struct {
		RootPostID    *int64 `gorm:"column:root_post_id"`
		ReplyToPostID *int64 `gorm:"column:reply_to_post_id"`
		TargetUserID  *int64 `gorm:"column:target_user_id"`
		PostNumber    int32  `gorm:"column:post_number"`
		EditedAt      *time.Time
	}
	var rootID, replyID int64
	if err := srcDB.Raw("SELECT post_id FROM patch_comment_community_map WHERE old_comment_id = 1").Scan(&rootID).Error; err != nil {
		t.Fatalf("ledger root: %v", err)
	}
	if err := srcDB.Raw("SELECT post_id FROM patch_comment_community_map WHERE old_comment_id = 2").Scan(&replyID).Error; err != nil {
		t.Fatalf("ledger reply: %v", err)
	}
	if err := testDB.Raw(
		"SELECT root_post_id, reply_to_post_id, target_user_id, post_number, edited_at FROM community_post WHERE id = ?",
		replyID).Scan(&reply).Error; err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply.RootPostID == nil || *reply.RootPostID != rootID {
		t.Fatalf("reply root must be the imported root %d, got %v", rootID, reply.RootPostID)
	}
	if reply.ReplyToPostID == nil || *reply.ReplyToPostID != rootID {
		t.Fatalf("reply target must be the imported root %d, got %v", rootID, reply.ReplyToPostID)
	}
	if reply.TargetUserID == nil || *reply.TargetUserID != 90001 {
		t.Fatalf("reply must address the parent author, got %v", reply.TargetUserID)
	}
	if reply.PostNumber != 2 {
		t.Fatalf("reply must be post 2 of its wall, got %d", reply.PostNumber)
	}
	if reply.EditedAt == nil {
		t.Fatal("an RFC3339 `edit` must become edited_at")
	}

	var millisEdited *time.Time
	if err := testDB.Raw(`
		SELECT p.edited_at FROM community_post p
		  JOIN community_thread t ON t.id = p.thread_id
		 WHERE t.site = ? AND t.anchor_id = '501'`, e2eSite).Scan(&millisEdited).Error; err != nil {
		t.Fatalf("read millis-edited post: %v", err)
	}
	if millisEdited == nil || !millisEdited.Equal(editedMillis) {
		t.Fatalf("an epoch-millis `edit` must become edited_at %v, got %v", editedMillis, millisEdited)
	}

	// Likes land as reactions on the root, and the trust counters that describe
	// those rows move with them.
	var likes int64
	if err := testDB.Raw("SELECT count(*) FROM community_reaction WHERE post_id = ?", rootID).Scan(&likes).Error; err != nil {
		t.Fatalf("count reactions: %v", err)
	}
	if likes != 2 {
		t.Fatalf("want 2 reactions on the root, got %d", likes)
	}
	var received *int32
	if err := testDB.Raw("SELECT likes_received FROM community_trust WHERE user_id = 90001").Scan(&received).Error; err != nil {
		t.Fatalf("read trust: %v", err)
	}
	if received == nil || *received != 2 {
		t.Fatalf("the root's author must have 2 likes_received, got %v", received)
	}

	// Idempotency: the same run again writes nothing new.
	again, err := run(srcDB, testDB, e2eSite, true)
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if again.ThreadsCreated != 0 || again.PostsInserted != 0 || again.LikesInserted != 0 {
		t.Fatalf("a re-run must be a no-op, got %d threads / %d posts / %d likes",
			again.ThreadsCreated, again.PostsInserted, again.LikesInserted)
	}
	if again.PostsExisting != 4 || again.LikesExisting != 2 {
		t.Fatalf("a re-run must recognise everything it wrote, got %d posts / %d likes",
			again.PostsExisting, again.LikesExisting)
	}
	var receivedAgain *int32
	if err := testDB.Raw("SELECT likes_received FROM community_trust WHERE user_id = 90001").Scan(&receivedAgain).Error; err != nil {
		t.Fatalf("read trust after re-run: %v", err)
	}
	if receivedAgain == nil || *receivedAgain != 2 {
		t.Fatalf("a re-run must not double-count likes_received, got %v", receivedAgain)
	}
}
