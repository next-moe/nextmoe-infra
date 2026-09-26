package migrate

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	suitelock "api/internal/platform/community/dbtest"
	"api/internal/platform/community/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Integration test against a real Postgres (the catalog service_test.go
// convention): a missing database skips the whole package. Schema comes from migrate.Run — the exact production
// migration — so these probes assert the storage-level invariants of doc 11 §3
// (4/5/7/12/13) directly against the shipped schema. `go test -p 1` (pinned in
// test.yml) serializes packages that share the CI database.

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("community/migrate")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		dbtest.SkipMainf("community/migrate", "cannot connect to test database: %v", err)
	}
	// Serialize against the sibling community test packages (service/handler)
	// that share this database — otherwise parallel `go test` TRUNCATEs race.
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	// First migration provisions the schema; running it again here is the
	// idempotency probe (a second AutoMigrate + IF NOT EXISTS raw section must
	// be a no-op).
	if err := Run(db); err != nil {
		release()
		dbtest.SkipMainf("community/migrate", "community migration failed: %v", err)
	}
	if err := Run(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "community migration is NOT idempotent: %v\n", err)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

// cleanTables truncates every table so ordering-independent probes start fresh.
func cleanTables(t *testing.T) {
	t.Helper()
	for _, table := range []string{
		"community_feed_seen", "community_activity_group", "community_activity",
		"community_user_follow", "community_write_request", "community_purge_archive", "community_notification", "community_event",
		"community_review_item", "community_flag", "community_trust",
		"community_board", "community_anchor_user", "community_thread_user", "community_reaction",
		"community_post", "community_thread",
	} {
		if err := testDB.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
}

// insertThread creates a thread, returning the row and the raw error so the
// invariant probes can inspect a unique violation. Every no-default meaningful-
// zero column is written explicitly (the GORM-trap discipline in action).
func insertThread(kind, anchorKind int16, anchorID string, status int16) (*model.CommunityThread, error) {
	th := &model.CommunityThread{
		Site:          "letmoe",
		Kind:          kind,
		AnchorKind:    anchorKind,
		AnchorID:      anchorID,
		ContentRating: model.ContentRatingAll,
		Status:        status,
		CreatedBy:     1,
	}
	return th, testDB.Create(th).Error
}

func mustThread(t *testing.T, kind, anchorKind int16, anchorID string, status int16) *model.CommunityThread {
	t.Helper()
	th, err := insertThread(kind, anchorKind, anchorID, status)
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	return th
}

func isDuplicate(err error) bool {
	return err != nil && strings.Contains(err.Error(), "duplicate key")
}

// --- invariant 4: one live comments thread per anchor ----------------------

func TestInvariant4_CommentsAnchorUnique(t *testing.T) {
	cleanTables(t)

	// First comments thread for the anchor: OK.
	mustThread(t, model.ThreadKindComments, model.AnchorKindSiteGame, "g100", model.ThreadStatusOpen)

	// Second live comments thread, same anchor: rejected by the partial unique.
	if _, err := insertThread(model.ThreadKindComments, model.AnchorKindSiteGame, "g100", model.ThreadStatusOpen); !isDuplicate(err) {
		t.Fatalf("second live comments thread for the same anchor should violate the partial unique, got: %v", err)
	}

	// topic (kind=0) is unconstrained: many per anchor.
	mustThread(t, model.ThreadKindTopic, model.AnchorKindBoard, "1", model.ThreadStatusOpen)
	mustThread(t, model.ThreadKindTopic, model.AnchorKindBoard, "1", model.ThreadStatusOpen)
}

// --- second tenant: site-scoped vs global comments anchor uniqueness --------

// TestSecondTenant_CommentsAnchorSiteScoped proves the split partial uniques at
// the storage layer: a SITE-LOCAL anchor id is unique only WITHIN a site (two
// tenants share the same local game id → two distinct threads), while a CATALOG
// anchor id is unique NETWORK-WIDE (one shared cross-site thread).
func TestSecondTenant_CommentsAnchorSiteScoped(t *testing.T) {
	cleanTables(t)

	comments := func(site string, anchorKind int16, anchorID string) error {
		return testDB.Create(&model.CommunityThread{
			Site: site, Kind: model.ThreadKindComments, AnchorKind: anchorKind, AnchorID: anchorID,
			ContentRating: model.ContentRatingAll, Status: model.ThreadStatusOpen, CreatedBy: 1,
		}).Error
	}

	// Site-local anchor (site_game "123"): letmoe and kungal each get their own
	// thread — the collision the hardening fixes.
	if err := comments("letmoe", model.AnchorKindSiteGame, "123"); err != nil {
		t.Fatalf("letmoe site_game 123: %v", err)
	}
	if err := comments("kungal", model.AnchorKindSiteGame, "123"); err != nil {
		t.Fatalf("kungal must be able to open its own site_game 123 thread: %v", err)
	}
	// But a SECOND live thread for the same (site, anchor) still violates.
	if err := comments("letmoe", model.AnchorKindSiteGame, "123"); !isDuplicate(err) {
		t.Fatalf("second live thread for (letmoe, site_game 123) must violate the site unique, got: %v", err)
	}

	// Catalog anchor (catalog_work "w9"): network-global — the first site wins the
	// single shared thread, a second site cannot open a duplicate.
	if err := comments("letmoe", model.AnchorKindCatalogWork, "w9"); err != nil {
		t.Fatalf("letmoe catalog_work w9: %v", err)
	}
	if err := comments("kungal", model.AnchorKindCatalogWork, "w9"); !isDuplicate(err) {
		t.Fatalf("catalog_work w9 is shared cross-site — a second site's thread must violate the global unique, got: %v", err)
	}
}

// TestSecondTenant_LegacyIndexDroppedOnUpgrade simulates the PROD upgrade path:
// the pre-second-tenant global unique already exists, and a migration run must
// DROP it (a CREATE IF NOT EXISTS on the split pair alone would leave it in
// place, ruling 5). Re-running Run is a no-op — the whole section stays
// idempotent.
func TestSecondTenant_LegacyIndexDroppedOnUpgrade(t *testing.T) {
	// Start clean: sibling probes leave two tenants sharing a site-local anchor id,
	// which the legacy GLOBAL unique cannot tolerate at CREATE time (that is the
	// very collision this migration fixes).
	cleanTables(t)
	// Recreate the legacy index by hand, as it exists on the live database today.
	if err := testDB.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_community_thread_anchor_comments
		    ON community_thread(anchor_kind, anchor_id)
		    WHERE kind = 1 AND status <> 3`).Error; err != nil {
		t.Fatalf("recreate legacy index: %v", err)
	}
	if rawIndexDef(t, "uq_community_thread_anchor_comments") == "" {
		t.Fatal("legacy index should exist after manual recreate")
	}
	// A migration run drops it and (re)creates the split pair; a second run is a
	// no-op.
	for i := range 2 {
		if err := Run(testDB); err != nil {
			t.Fatalf("migrate run %d: %v", i, err)
		}
	}
	if def := rawIndexDef(t, "uq_community_thread_anchor_comments"); def != "" {
		t.Fatalf("legacy index must be dropped by the migration, still present:\n  %s", def)
	}
	if rawIndexDef(t, "uq_community_thread_anchor_comments_site") == "" ||
		rawIndexDef(t, "uq_community_thread_anchor_comments_global") == "" {
		t.Fatal("both split partial uniques must exist after the migration")
	}
}

// --- invariant 13: tombstone drops out of the comments unique (rebuild) ----

func TestInvariant4_TombstoneRebuild(t *testing.T) {
	cleanTables(t)

	first := mustThread(t, model.ThreadKindComments, model.AnchorKindSiteResource, "r7", model.ThreadStatusOpen)

	// Tombstone the first thread (invariant 13: status, never gorm.DeletedAt).
	if err := testDB.Model(first).Update("status", model.ThreadStatusDeleted).Error; err != nil {
		t.Fatalf("tombstone update: %v", err)
	}

	// A fresh comments thread for the same anchor is now allowed.
	if _, err := insertThread(model.ThreadKindComments, model.AnchorKindSiteResource, "r7", model.ThreadStatusOpen); err != nil {
		t.Fatalf("comments thread should be re-creatable after the prior one is tombstoned, got: %v", err)
	}
}

// --- invariant 5: (thread_id, post_number) unique --------------------------

func TestInvariant5_PostNumberUnique(t *testing.T) {
	cleanTables(t)
	th := mustThread(t, model.ThreadKindTopic, model.AnchorKindBoard, "1", model.ThreadStatusOpen)

	post := func(n int32) error {
		return testDB.Create(&model.CommunityPost{
			ThreadID: th.ID, PostNumber: n, AuthorID: 1,
			ContentRaw: "hi", ContentHTML: "<p>hi</p>", SanitizerVersion: 1,
			ContentRating: model.ContentRatingAll, Status: model.PostStatusVisible,
		}).Error
	}

	if err := post(1); err != nil {
		t.Fatalf("first post: %v", err)
	}
	if err := post(1); !isDuplicate(err) {
		t.Fatalf("duplicate (thread_id, post_number) should be rejected, got: %v", err)
	}
	if err := post(2); err != nil {
		t.Fatalf("distinct post_number should be accepted: %v", err)
	}
}

// --- board UNIQUE(site, slug) ----------------------------------------------

func TestBoardSiteSlugUnique(t *testing.T) {
	cleanTables(t)
	slug := "general"
	board := func(site string) error {
		return testDB.Create(&model.CommunityBoard{
			Site: site, Name: "General", Slug: slug, Format: model.BoardFormatDiscussion,
		}).Error
	}
	if err := board("letmoe"); err != nil {
		t.Fatalf("first board: %v", err)
	}
	if err := board("letmoe"); !isDuplicate(err) {
		t.Fatalf("duplicate (site, slug) should be rejected, got: %v", err)
	}
	if err := board("nextmanga"); err != nil {
		t.Fatalf("same slug on a different site should be accepted: %v", err)
	}
}

// --- flag UNIQUE(post_id, flagger_id) --------------------------------------

func TestFlagPostFlaggerUnique(t *testing.T) {
	cleanTables(t)
	flag := func(flagger int64) error {
		return testDB.Create(&model.CommunityFlag{
			PostID: 1, FlaggerID: flagger, Weight: 1.0, Status: model.FlagStatusPending,
		}).Error
	}
	if err := flag(10); err != nil {
		t.Fatalf("first flag: %v", err)
	}
	if err := flag(10); !isDuplicate(err) {
		t.Fatalf("one user flags a post once — duplicate should be rejected, got: %v", err)
	}
	if err := flag(11); err != nil {
		t.Fatalf("a different flagger should be accepted: %v", err)
	}
}

// --- invariant 7: tenant-first index column order (composite-index-trap
// regression). indexdef is asserted verbatim on the ordered column list. ------

func TestIndexColumnOrder(t *testing.T) {
	cases := []struct{ name, wantCols string }{
		// The two tenant/anchor-first access indexes (invariant 7).
		{"idx_community_thread_site_list", "(site, kind, last_posted_at DESC)"},
		{"idx_community_thread_anchor", "(anchor_kind, anchor_id, kind)"},
		// The GORM-tag composites (priority discipline): if a priority were
		// dropped, the column order here would silently reshuffle.
		{"idx_community_post_thread_root", "(thread_id, root_post_id, post_number)"},
		{"uq_community_post_thread_number", "(thread_id, post_number)"},
		// The listing sorts and the site-wide post feed. Each keeps the
		// (site, kind) prefix so switching sort changes only the trailing key;
		// the feed's key is created_at, never id.
		{"idx_community_thread_site_created", "(site, kind, created_at DESC)"},
		{"idx_community_thread_site_posts", "(site, kind, posts_count DESC)"},
		{"idx_community_post_created", "(created_at DESC, id DESC)"},
		// The unread reads come in by user; the primary key leads with thread_id.
		{"idx_community_thread_user_user", "(user_id)"},
		{"idx_community_board_parent", "(parent_id)"},
		{"idx_community_thread_board", "(site, anchor_id, last_posted_at DESC) WHERE ((kind = 0) AND (anchor_kind = 0))"},
		{"idx_community_thread_pinned", "(site, kind, pinned_at DESC) WHERE (pin_scope > 0)"},
		{"uq_community_anchor_user", "(site, user_id, anchor_kind, anchor_id)"},
		{"idx_community_anchor_user_anchor", "(anchor_kind, anchor_id, site)"},
		{"idx_community_notification_inbox", "(site, user_id, seq DESC)"},
		{"idx_community_notification_feed", "(site, seq)"},
		{"uq_community_user_follow", "(follower_id, followee_id)"},
		{"idx_community_user_follow_followee", "(followee_id, id DESC)"},
		{"idx_community_user_follow_follower", "(follower_id, id DESC)"},
		{"uq_community_activity_key", "(site, key)"},
		{"idx_community_activity_site_id", "(site, id)"},
		{"idx_community_activity_member", "(site, actor_id, verb, object_kind, bucket_date, occurred_at DESC, id DESC)"},
		{"idx_community_activity_notified", "(site, actor_id, occurred_at DESC, id DESC) WHERE ((removed_at IS NULL) AND (notified_at IS NOT NULL))"},
		{"idx_community_activity_removed", "(removed_at) WHERE (removed_at IS NOT NULL)"},
		{"uq_community_activity_group", "(site, actor_id, verb, object_kind, bucket_date)"},
		{"idx_community_activity_group_all", "(actor_id, latest_all_at DESC, id DESC) WHERE (count_all > 0)"},
		{"idx_community_activity_group_sfw", "(actor_id, latest_sfw_at DESC, id DESC) WHERE (count_sfw > 0)"},
		{"idx_community_notification_fold_key", "(site, fold_key) WHERE ((read_at IS NULL) AND (fold_key IS NOT NULL))"},
	}
	for _, c := range cases {
		def := indexDef(t, c.name)
		if !strings.Contains(def, c.wantCols) {
			t.Errorf("index %s: want column list %q in\n  %s", c.name, c.wantCols, def)
		}
	}

	fold := indexDef(t, "uq_community_notification_fold")
	for _, frag := range []string{"(site, user_id, fold_key)", "read_at IS NULL", "fold_key IS NOT NULL"} {
		if !strings.Contains(fold, frag) {
			t.Errorf("fold unique missing %q in\n  %s", frag, fold)
		}
	}
	if !strings.Contains(fold, "UNIQUE INDEX") {
		t.Errorf("fold index is not unique:\n  %s", fold)
	}
	pending := indexDef(t, "idx_community_event_pending")
	for _, frag := range []string{"(attempt_after, id)", "processed_at IS NULL"} {
		if !strings.Contains(pending, frag) {
			t.Errorf("pending-event index missing %q in\n  %s", frag, pending)
		}
	}
	unread := indexDef(t, "idx_community_notification_unread")
	if !strings.Contains(unread, "(site, user_id)") || !strings.Contains(unread, "read_at IS NULL") {
		t.Errorf("unread index:\n  %s", unread)
	}
	threadUnread := indexDef(t, "idx_community_notification_thread_unread")
	if !strings.Contains(threadUnread, "(thread_id, user_id)") || !strings.Contains(threadUnread, "read_at IS NULL") {
		t.Errorf("thread unread index:\n  %s", threadUnread)
	}
	readIdx := indexDef(t, "idx_community_notification_read")
	if !strings.Contains(readIdx, "(read_at)") || !strings.Contains(readIdx, "read_at IS NOT NULL") {
		t.Errorf("read index:\n  %s", readIdx)
	}
	processed := indexDef(t, "idx_community_event_processed")
	if !strings.Contains(processed, "(processed_at)") || !strings.Contains(processed, "processed_at IS NOT NULL") {
		t.Errorf("processed-event index:\n  %s", processed)
	}
	var seq int
	if err := testDB.Raw(`SELECT COUNT(*) FROM pg_class WHERE relkind = 'S' AND relname = 'community_notification_seq'`).Scan(&seq).Error; err != nil {
		t.Fatalf("read sequence: %v", err)
	}
	if seq != 1 {
		t.Fatalf("community_notification_seq must exist, count=%d", seq)
	}

	// The two comments partial uniques carry their columns AND predicate: the
	// site-scoped one leads with site and filters to the site-local anchor kinds;
	// the global one omits site and filters to the catalog anchor kinds.
	site := indexDef(t, "uq_community_thread_anchor_comments_site")
	for _, frag := range []string{"(site, anchor_kind, anchor_id)", "kind = 1", "status <> 3", "anchor_kind = ANY (ARRAY[1, 2])"} {
		if !strings.Contains(site, frag) {
			t.Errorf("site partial unique missing %q in\n  %s", frag, site)
		}
	}
	global := indexDef(t, "uq_community_thread_anchor_comments_global")
	for _, frag := range []string{"(anchor_kind, anchor_id)", "kind = 1", "status <> 3", "anchor_kind = ANY (ARRAY[3, 4])"} {
		if !strings.Contains(global, frag) {
			t.Errorf("global partial unique missing %q in\n  %s", frag, global)
		}
	}
}

// TestSecondTenant_CommentsAnchorIndexSplit is the migration-state probe of the
// second-tenant hardening (step 01 deliverable A / probe 5): the pre-second-tenant
// single-keyspace unique is GONE and exactly the two split partial uniques exist.
// The old index staying alive would keep clamping site-local anchors globally
// (two tenants sharing a local id would collide), so its absence is load-bearing.
func TestSecondTenant_CommentsAnchorIndexSplit(t *testing.T) {
	if def := rawIndexDef(t, "uq_community_thread_anchor_comments"); def != "" {
		t.Errorf("legacy global comments anchor unique must be dropped, still present:\n  %s", def)
	}
	// The site-scoped unique: (site, anchor_kind, anchor_id), site-local kinds only.
	site := indexDef(t, "uq_community_thread_anchor_comments_site")
	if !strings.Contains(site, "UNIQUE INDEX") {
		t.Errorf("site index is not unique:\n  %s", site)
	}
	for _, frag := range []string{"(site, anchor_kind, anchor_id)", "WHERE", "kind = 1", "status <> 3", "anchor_kind = ANY (ARRAY[1, 2])"} {
		if !strings.Contains(site, frag) {
			t.Errorf("site index indexdef missing %q in\n  %s", frag, site)
		}
	}
	// The global unique: (anchor_kind, anchor_id) with NO site, catalog kinds only.
	global := indexDef(t, "uq_community_thread_anchor_comments_global")
	if !strings.Contains(global, "UNIQUE INDEX") {
		t.Errorf("global index is not unique:\n  %s", global)
	}
	if strings.Contains(global, "(site,") {
		t.Errorf("global index must NOT lead with site (catalog anchors are network-global):\n  %s", global)
	}
	for _, frag := range []string{"(anchor_kind, anchor_id)", "WHERE", "kind = 1", "status <> 3", "anchor_kind = ANY (ARRAY[3, 4])"} {
		if !strings.Contains(global, frag) {
			t.Errorf("global index indexdef missing %q in\n  %s", frag, global)
		}
	}
}

func indexDef(t *testing.T, name string) string {
	t.Helper()
	def := rawIndexDef(t, name)
	if def == "" {
		t.Fatalf("index %s does not exist", name)
	}
	return def
}

// rawIndexDef returns an index's definition, or "" when the index does not exist
// — the non-fatal form used to assert an index was DROPPED.
func rawIndexDef(t *testing.T, name string) string {
	t.Helper()
	var def string
	if err := testDB.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = ?`, name,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read indexdef %s: %v", name, err)
	}
	return def
}

// --- column-name audit (acronym-column-naming-trap regression) -------------

func TestColumnAudit(t *testing.T) {
	want := map[string][]string{
		"community_thread": {
			"id", "site", "kind", "anchor_kind", "anchor_id", "title",
			"header_image_hashes", "content_rating", "status", "fb_status",
			"fb_response", "fb_responder_id", "fb_responded_at", "merged_into_id",
			"answer_post_id", "pin_scope", "pinned_at", "pinned_until",
			"posts_count", "participants_count",
			"highest_post_number", "last_posted_at", "created_by", "created_at",
			"updated_at",
		},
		"community_post": {
			"id", "thread_id", "post_number", "root_post_id", "reply_to_post_id",
			"target_user_id", "author_id", "content_raw", "content_html",
			"sanitizer_version", "content_rating", "status", "edited_at",
			"edited_by_moderator", "created_at",
		},
		"community_reaction": {"post_id", "user_id", "kind", "created_at"},
		"community_thread_user": {
			"thread_id", "user_id", "site", "last_read_post_number", "notification_level",
			"last_visited_at",
		},
		"community_anchor_user": {
			"id", "site", "user_id", "anchor_kind", "anchor_id", "notification_level",
			"created_at", "updated_at",
		},
		"community_board": {
			"id", "site", "parent_id", "slug", "name", "description", "icon", "color",
			"position", "format", "status", "content_rating", "topic_min_trust_level",
			"reply_min_trust_level", "topic_template", "created_at", "updated_at",
		},
		"community_trust": {
			"user_id", "level", "topics_entered", "posts_read", "read_time_s",
			"days_visited", "likes_given", "likes_received", "flags_agreed",
			"flags_disagreed", "first_posts_held_remaining", "granted_boost",
			"updated_at",
		},
		"community_flag": {
			"id", "post_id", "flagger_id", "reason", "note", "weight", "status",
			"created_at",
		},
		"community_review_item": {
			"id", "site", "post_id", "source", "status", "decided_by",
			"decided_at", "created_at", "trust_review_item_id", "forward_attempts",
		},
		"community_event": {
			"id", "site", "kind", "thread_id", "post_id", "actor_id", "target_user_id",
			"mention_user_ids", "activity_id", "attempts", "attempt_after", "processed_at", "created_at",
		},
		"community_notification": {
			"id", "site", "user_id", "kind", "thread_id", "anchor_kind", "anchor_id",
			"post_id", "post_number", "first_post_number", "since_at", "actor_id",
			"actor_count", "item_count", "fold_key", "activity_id", "read_at", "seq", "created_at",
			"updated_at",
		},
		"community_purge_archive": {
			"id", "site", "author_id", "step", "data", "restored_at", "created_at",
		},
		"community_write_request": {
			"id", "site", "idempotency_key", "request_hash", "post_id", "created_at",
		},
		"community_user_follow": {
			"id", "follower_id", "followee_id", "origin_site", "created_at", "imported_at", "notify_level",
		},
		"community_activity": {
			"id", "site", "key", "actor_id", "verb", "object_kind", "object_label", "title",
			"excerpt", "url", "cover_image_hash", "work_id", "content_limit", "notify",
			"occurred_at", "bucket_date", "revision", "notified_at", "removed_at",
			"created_at", "updated_at",
		},
		"community_activity_group": {
			"id", "site", "actor_id", "verb", "object_kind", "bucket_date", "object_label",
			"count_all", "latest_all_id", "latest_all_at", "count_sfw", "latest_sfw_id",
			"latest_sfw_at", "created_at", "updated_at",
		},
		"community_feed_seen": {"user_id", "seen_at", "updated_at"},
	}
	for table, cols := range want {
		got := columnNames(t, table)
		sort.Strings(cols)
		if strings.Join(got, ",") != strings.Join(cols, ",") {
			t.Errorf("table %s columns mismatch\n  want: %v\n  got:  %v", table, cols, got)
		}
	}
}

func columnNames(t *testing.T, table string) []string {
	t.Helper()
	var names []string
	if err := testDB.Raw(
		`SELECT column_name FROM information_schema.columns
		   WHERE table_schema = 'public' AND table_name = ? ORDER BY column_name`, table,
	).Scan(&names).Error; err != nil {
		t.Fatalf("read columns of %s: %v", table, err)
	}
	if len(names) == 0 {
		t.Fatalf("table %s has no columns (missing?)", table)
	}
	return names
}

// --- column-type spot check (types alignment with doc 11 §4) ---------------

func TestColumnTypes(t *testing.T) {
	cases := []struct{ table, column, want string }{
		{"community_post", "post_number", "integer"},         // int, not bigint
		{"community_thread", "kind", "smallint"},             // smallint enum
		{"community_thread", "header_image_hashes", "jsonb"}, // explicit jsonb
		{"community_flag", "weight", "real"},                 // real, NOT numeric
		{"community_thread", "posts_count", "integer"},       // int counter
		{"community_trust", "user_id", "bigint"},             // global user id
	}
	for _, c := range cases {
		var got string
		if err := testDB.Raw(
			`SELECT data_type FROM information_schema.columns
			   WHERE table_schema='public' AND table_name=? AND column_name=?`,
			c.table, c.column,
		).Scan(&got).Error; err != nil {
			t.Fatalf("read type %s.%s: %v", c.table, c.column, err)
		}
		if got != c.want {
			t.Errorf("%s.%s: want %s, got %s", c.table, c.column, c.want, got)
		}
	}
}

// --- search: the trigram extension and its two indexes ----------------------

func TestTrigramSearchIndexes(t *testing.T) {
	var installed string
	if err := testDB.Raw(`SELECT extversion FROM pg_extension WHERE extname = 'pg_trgm'`).Scan(&installed).Error; err != nil {
		t.Fatalf("read pg_trgm: %v", err)
	}
	if installed == "" {
		t.Fatal("pg_trgm must be installed by the migration: ILIKE search has no other index on stock Postgres")
	}
	for _, name := range []string{"idx_community_post_content_trgm", "idx_community_thread_title_trgm"} {
		def := indexDef(t, name)
		if !strings.Contains(def, "USING gin") || !strings.Contains(def, "gin_trgm_ops") {
			t.Errorf("index %s must be a trigram GIN index, got\n  %s", name, def)
		}
	}
}

func TestLegacyBoardAnchorsBecomeBoards(t *testing.T) {
	cleanTables(t)
	topic := func(site, anchor string) *model.CommunityThread {
		t.Helper()
		th := &model.CommunityThread{
			Site: site, Kind: model.ThreadKindTopic, AnchorKind: model.AnchorKindBoard, AnchorID: anchor,
			ContentRating: model.ContentRatingAll, Status: model.ThreadStatusOpen, CreatedBy: 1,
		}
		if err := testDB.Create(th).Error; err != nil {
			t.Fatalf("seed topic %s/%s: %v", site, anchor, err)
		}
		return th
	}
	a := topic("letmoe", "main")
	b := topic("letmoe", "main")
	staging := topic("letmoe-staging", "main")
	numeric := topic("letmoe", "77")
	wall := mustThread(t, model.ThreadKindComments, model.AnchorKindSiteGame, "main", model.ThreadStatusOpen)
	feedback := mustThread(t, model.ThreadKindFeedback, model.AnchorKindBoard, "reports", model.ThreadStatusOpen)
	feedbackOnMain := mustThread(t, model.ThreadKindFeedback, model.AnchorKindBoard, "main", model.ThreadStatusOpen)

	for i := range 2 {
		if err := Run(testDB); err != nil {
			t.Fatalf("migrate run %d: %v", i, err)
		}
	}

	var boards []model.CommunityBoard
	if err := testDB.Order("site").Find(&boards).Error; err != nil {
		t.Fatalf("read boards: %v", err)
	}
	if len(boards) != 2 || boards[0].Site != "letmoe" || boards[1].Site != "letmoe-staging" ||
		boards[0].Slug != "main" || boards[0].Name != "main" {
		t.Fatalf("want one main board per site, got %+v", boards)
	}
	anchorOf := func(id int64) string {
		t.Helper()
		var th model.CommunityThread
		if err := testDB.First(&th, id).Error; err != nil {
			t.Fatalf("reload %d: %v", id, err)
		}
		return th.AnchorID
	}
	for _, c := range []struct {
		id   int64
		want string
	}{
		{a.ID, model.BoardAnchorID(boards[0].ID)},
		{b.ID, model.BoardAnchorID(boards[0].ID)},
		{staging.ID, model.BoardAnchorID(boards[1].ID)},
		{numeric.ID, "77"},
		{wall.ID, "main"},
		{feedback.ID, "reports"},
		{feedbackOnMain.ID, "main"},
	} {
		if got := anchorOf(c.id); got != c.want {
			t.Errorf("thread %d: want anchor %q, got %q", c.id, c.want, got)
		}
	}

	var nullable string
	if err := testDB.Raw(`SELECT is_nullable FROM information_schema.columns
		WHERE table_name = 'community_board' AND column_name = 'slug'`).Scan(&nullable).Error; err != nil {
		t.Fatalf("read slug nullability: %v", err)
	}
	if nullable != "NO" {
		t.Fatalf("community_board.slug must be NOT NULL, is_nullable=%s", nullable)
	}
}

func TestThreadUserSiteBackfill(t *testing.T) {
	cleanTables(t)
	th := mustThread(t, model.ThreadKindTopic, model.AnchorKindBoard, "1", model.ThreadStatusOpen)
	if err := testDB.Exec(`
		INSERT INTO community_thread_user (thread_id, user_id, last_read_post_number, notification_level)
		VALUES (?, 1, 0, 1)`, th.ID).Error; err != nil {
		t.Fatalf("insert null-site row: %v", err)
	}
	if err := testDB.Exec(`
		INSERT INTO community_thread_user (thread_id, user_id, last_read_post_number, notification_level, site)
		VALUES (?, 2, 0, 1, 'kungal')`, th.ID).Error; err != nil {
		t.Fatalf("insert set-site row: %v", err)
	}
	for i := range 2 {
		if err := Run(testDB); err != nil {
			t.Fatalf("migrate run %d: %v", i, err)
		}
	}
	got := map[int64]*string{}
	var rows []model.CommunityThreadUser
	if err := testDB.Order("user_id").Find(&rows).Error; err != nil {
		t.Fatalf("read thread_user: %v", err)
	}
	for i := range rows {
		got[rows[i].UserID] = rows[i].Site
	}
	if got[1] == nil || *got[1] != th.Site {
		t.Fatalf("null site must backfill from the thread, got %v (thread %q)", got[1], th.Site)
	}
	if got[2] == nil || *got[2] != "kungal" {
		t.Fatalf("an already-set site must be kept, got %v", got[2])
	}
}

func TestMigrateCreatesUserFollow(t *testing.T) {
	cleanTables(t)

	var tables int
	if err := testDB.Raw(`SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = 'community_user_follow'`).Scan(&tables).Error; err != nil {
		t.Fatalf("table exists: %v", err)
	}
	if tables != 1 {
		t.Fatal("community_user_follow must exist")
	}
	for _, col := range []string{"created_at", "imported_at"} {
		var nullable string
		if err := testDB.Raw(`SELECT is_nullable FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'community_user_follow' AND column_name = ?`, col).
			Scan(&nullable).Error; err != nil {
			t.Fatalf("nullability %s: %v", col, err)
		}
		if nullable != "YES" {
			t.Fatalf("%s must be nullable, is_nullable=%s", col, nullable)
		}
	}

	self := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site) VALUES (1, 1, 'kungal')`).Error
	if self == nil || !strings.Contains(self.Error(), "chk_community_user_follow_self") {
		t.Fatalf("self-follow must fail the check, got: %v", self)
	}
	if err := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site) VALUES (1, 2, 'kungal')`).Error; err != nil {
		t.Fatalf("first pair: %v", err)
	}
	dup := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site) VALUES (1, 2, 'moyu')`).Error
	if !isDuplicate(dup) {
		t.Fatalf("duplicate pair must fail the unique, got: %v", dup)
	}

	followee := indexDef(t, "idx_community_user_follow_followee")
	if !strings.Contains(followee, "(followee_id, id DESC)") {
		t.Fatalf("followee index:\n  %s", followee)
	}
	follower := indexDef(t, "idx_community_user_follow_follower")
	if !strings.Contains(follower, "(follower_id, id DESC)") {
		t.Fatalf("follower index:\n  %s", follower)
	}
	uq := indexDef(t, "uq_community_user_follow")
	if !strings.Contains(uq, "(follower_id, followee_id)") {
		t.Fatalf("unique:\n  %s", uq)
	}

	if err := Run(testDB); err != nil {
		t.Fatalf("second migrate.Run: %v", err)
	}
}

func TestMigrateCreatesActivityTables(t *testing.T) {
	cleanTables(t)

	var gen struct {
		IsGenerated string `gorm:"column:is_generated"`
		Expression  string `gorm:"column:generation_expression"`
	}
	if err := testDB.Raw(`SELECT is_generated, generation_expression FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'community_activity' AND column_name = 'bucket_date'`).
		Scan(&gen).Error; err != nil {
		t.Fatalf("bucket_date: %v", err)
	}
	if gen.IsGenerated != "ALWAYS" || !strings.Contains(gen.Expression, "Asia/Shanghai") {
		t.Fatalf("bucket_date must be generated from occurred_at in Beijing time: %+v", gen)
	}
	if err := testDB.Exec(`INSERT INTO community_activity (site, key, actor_id, verb, object_kind, object_label,
		title, excerpt, url, content_limit, notify, occurred_at, revision, created_at, updated_at)
		VALUES ('kungal', 'k', 1, 0, 'topic', 'l', 't', '', 'https://x', 0, false,
		        '2026-09-25T16:30:00Z', 1, now(), now())`).Error; err != nil {
		t.Fatalf("insert activity: %v", err)
	}
	var day string
	testDB.Raw(`SELECT bucket_date::text FROM community_activity WHERE key = 'k'`).Scan(&day)
	if day != "2026-09-26" {
		t.Fatalf("16:30 UTC on the 25th is the 26th in Beijing, got %s", day)
	}
	dup := testDB.Exec(`INSERT INTO community_activity (site, key, actor_id, verb, object_kind, object_label,
		title, excerpt, url, content_limit, notify, occurred_at, revision, created_at, updated_at)
		VALUES ('kungal', 'k', 2, 0, 'topic', 'l', 't', '', 'https://x', 0, false, now(), 2, now(), now())`).Error
	if !isDuplicate(dup) {
		t.Fatalf("(site, key) must be unique, got: %v", dup)
	}

	if err := testDB.Exec(`INSERT INTO community_user_follow (follower_id, followee_id, origin_site) VALUES (1, 2, 'moyu')`).Error; err != nil {
		t.Fatalf("edge: %v", err)
	}
	var level int
	testDB.Raw(`SELECT notify_level FROM community_user_follow WHERE follower_id = 1`).Scan(&level)
	if level != 0 {
		t.Fatalf("an edge written without a level notifies on everything, got %d", level)
	}

	if err := Run(testDB); err != nil {
		t.Fatalf("second migrate.Run: %v", err)
	}
}
