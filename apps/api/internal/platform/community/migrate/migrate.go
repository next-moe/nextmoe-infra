// Package migrate owns the kun_community schema migration: the AutoMigrate
// table list plus the idempotent raw-SQL section for the indexes AutoMigrate
// cannot express (partial-unique and DESC-ordered composites). It is called by
// cmd/migrate-community and by the migrate integration test (which provisions
// its test database with the exact production schema).
package migrate

import (
	"fmt"

	"api/internal/platform/accountpurge"
	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

// Run applies the full community schema (tables + raw SQL). Idempotent: safe to
// run on every deploy and repeatedly against the same database. It seeds
// nothing; the only rows it writes are the legacy-anchor boards of boardsSQL.
func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// community_thread first: community_post's thread_id FK and the thread
		// self-reference (merged_into_id) both point at it.
		&model.CommunityThread{},
		&model.CommunityPost{},
		&model.CommunityReaction{},
		&model.CommunityThreadUser{},
		&model.CommunityAnchorUser{},
		&model.CommunityEvent{},
		&model.CommunityNotification{},
		&model.CommunityBoard{},
		&model.CommunityTrust{},
		&model.CommunityFlag{},
		&model.CommunityReviewItem{},
		&model.CommunityPurgeArchive{},
		&model.CommunityWriteRequest{},
		// 2026-09-25: how far this service has read the deleted-accounts list
		// (oauth doc 17). A new table with no rows; the first run starts from
		// the beginning of the list.
		&accountpurge.Cursor{},
	); err != nil {
		return fmt.Errorf("community automigrate: %w", err)
	}
	return rawSQL(db)
}

// rawSQL is the post-AutoMigrate section: the indexes AutoMigrate cannot
// express (partial predicate / DESC sort). Every statement is idempotent
// (CREATE INDEX IF NOT EXISTS), so this section reruns freely.
func rawSQL(db *gorm.DB) error {
	// Retire the single-keyspace comments-anchor unique BEFORE (re)creating the
	// site-scoped split pair below. CREATE INDEX IF NOT EXISTS never redefines an
	// EXISTING index, so the pre-second-tenant index — a global unique on
	// (anchor_kind, anchor_id) — must be dropped by name first, otherwise it would
	// keep clamping site-local anchors globally and two tenants sharing a local
	// game/resource id would collide (gorm-composite-index-priority-trap family,
	// ruling 5). DROP INDEX IF EXISTS is idempotent: a no-op on a fresh database
	// and on every rerun after the first.
	if err := db.Exec(`DROP INDEX IF EXISTS uq_community_thread_anchor_comments`).Error; err != nil {
		return fmt.Errorf("drop legacy comments anchor unique: %w", err)
	}
	// Wave 07: retire the blanket newcomer hold. AutoMigrate never LOWERS an
	// existing column default, so dropping the model's `default:2` tag alone would
	// leave the DDL default at 2 and every fresh trust row would silently keep
	// holding two posts (the gorm-default-tag-zero-value-trap, inverted). Both
	// statements are idempotent and must ship together with the model change:
	// the ALTER fixes future rows, the UPDATE releases the ones already carrying a
	// budget (391 users in production at cutover).
	if err := db.Exec(
		`ALTER TABLE community_trust ALTER COLUMN first_posts_held_remaining SET DEFAULT 0`).Error; err != nil {
		return fmt.Errorf("drop newcomer hold default: %w", err)
	}
	if err := db.Exec(
		`UPDATE community_trust SET first_posts_held_remaining = 0, updated_at = now()
		 WHERE first_posts_held_remaining > 0`).Error; err != nil {
		return fmt.Errorf("release outstanding newcomer holds: %w", err)
	}
	// Search is ILIKE '%q%' over the markdown source and thread titles, which no
	// btree can serve. pg_trgm is the only CJK-capable option on a stock
	// Postgres (zhparser/pg_jieba are not installed and to_tsvector has no
	// Chinese tokenizer), and it is a trusted extension, so this needs no
	// superuser. It accelerates patterns of three characters or more; a two
	// character query — very common in Chinese — extracts no full trigram and
	// falls back to a scan, which the corpus size still absorbs (11k posts /
	// 4.5 MB at the time of writing). A real search engine is the scale
	// trigger, not the starting point.
	if err := db.Exec(`CREATE EXTENSION IF NOT EXISTS pg_trgm`).Error; err != nil {
		return fmt.Errorf("create pg_trgm: %w", err)
	}
	// seq is assigned only by the dispatcher (insert and every fold update);
	// AutoMigrate cannot express a sequence that is not a column default.
	if err := db.Exec(`CREATE SEQUENCE IF NOT EXISTS community_notification_seq`).Error; err != nil {
		return fmt.Errorf("create community_notification_seq: %w", err)
	}
	for _, ix := range []struct{ name, stmt string }{
		{"idx_community_post_content_trgm", `
			CREATE INDEX IF NOT EXISTS idx_community_post_content_trgm
			    ON community_post USING gin (content_raw gin_trgm_ops)`},
		{"idx_community_thread_title_trgm", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_title_trgm
			    ON community_thread USING gin (title gin_trgm_ops)`},
		// Invariant 4 (site anchors): at most ONE live comments thread per
		// (site, anchor). Site-local anchors (anchor_kind 1=site_game,
		// 2=site_resource) carry a tenant-local id, so identity INCLUDES the site
		// — two tenants can mint the same local id and must resolve to distinct
		// threads. Partial — deleted threads (status=3) drop out so a fresh
		// comments thread can be opened for the same anchor (tombstone-rebuild).
		{"uq_community_thread_anchor_comments_site", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_community_thread_anchor_comments_site
			    ON community_thread(site, anchor_kind, anchor_id)
			    WHERE kind = 1 AND status <> 3 AND anchor_kind IN (1, 2)`},
		// Invariant 4 (catalog anchors): at most ONE live comments thread per
		// catalog anchor NETWORK-WIDE. Catalog anchors (anchor_kind 3=catalog_work,
		// 4=catalog_person) carry a network-global id and share one cross-site
		// conversation by design (invariant 1), so the unique deliberately omits
		// site. topic/feedback (kind 0/2) and board (anchor_kind 0) stay
		// unconstrained.
		{"uq_community_thread_anchor_comments_global", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_community_thread_anchor_comments_global
			    ON community_thread(anchor_kind, anchor_id)
			    WHERE kind = 1 AND status <> 3 AND anchor_kind IN (3, 4)`},
		// Invariant 7 (tenant-first): the in-site thread list, ordered by
		// recency. site leads; last_posted_at DESC serves the newest-first read.
		{"idx_community_thread_site_list", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_site_list
			    ON community_thread(site, kind, last_posted_at DESC)`},
		// Invariant 7 (anchor dimension): per-anchor aggregation across sites
		// (the NextMoe aggregate read). Both the (site, …) and (anchor, …)
		// dimensions are queryable.
		{"idx_community_thread_anchor", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_anchor
			    ON community_thread(anchor_kind, anchor_id, kind)`},
		// The two sorts the site thread list offers besides activity. Both carry
		// the same (site, kind) prefix as the activity index so a sort switch
		// changes only the trailing key.
		{"idx_community_thread_site_created", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_site_created
			    ON community_thread(site, kind, created_at DESC)`},
		{"idx_community_thread_site_posts", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_site_posts
			    ON community_thread(site, kind, posts_count DESC)`},
		// The site-wide newest-posts feed. It orders by created_at, not id: the
		// kungal import gave historical comments fresh ids, so id order is import
		// order (measured corr(id, created_at) = 0.72 at the time of writing).
		{"idx_community_post_created", `
			CREATE INDEX IF NOT EXISTS idx_community_post_created
			    ON community_post(created_at DESC, id DESC)`},
		// User footprint (NextMoe profile): a user's posts newest-first. DESC
		// sort, so it lives here rather than a struct tag.
		{"idx_community_post_author", `
			CREATE INDEX IF NOT EXISTS idx_community_post_author
			    ON community_post(author_id, created_at DESC)`},
		// The sub-board lookups and the self-FK check on a board delete; a site's
		// boards are already reached through uq_community_board_site_slug.
		{"idx_community_board_parent", `
			CREATE INDEX IF NOT EXISTS idx_community_board_parent
			    ON community_board(parent_id)`},
		// One board's topics by activity, and the per-board stats aggregate.
		// Partial: topics are a sliver of the table next to the comment walls.
		{"idx_community_thread_board", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_board
			    ON community_thread(site, anchor_id, last_posted_at DESC)
			    WHERE kind = 0 AND anchor_kind = 0`},
		// The pinned set a listing puts on top; only pinned rows are indexed.
		{"idx_community_thread_pinned", `
			CREATE INDEX IF NOT EXISTS idx_community_thread_pinned
			    ON community_thread(site, kind, pinned_at DESC)
			    WHERE pin_scope > 0`},
		// Who watches this anchor, per delivery site. The table is new, so the
		// index is created over no rows.
		{"idx_community_anchor_user_anchor", `
			CREATE INDEX IF NOT EXISTS idx_community_anchor_user_anchor
			    ON community_anchor_user(anchor_kind, anchor_id, site)`},
		// One unread folded row per (site, user, fold_key). Marking it read
		// drops it out so the next activity starts a new row; a NULL fold_key
		// (replied / mentioned / thread_created / answer_accepted) never folds.
		{"uq_community_notification_fold", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_community_notification_fold
			    ON community_notification(site, user_id, fold_key)
			    WHERE read_at IS NULL AND fold_key IS NOT NULL`},
		{"idx_community_notification_inbox", `
			CREATE INDEX IF NOT EXISTS idx_community_notification_inbox
			    ON community_notification(site, user_id, seq DESC)`},
		{"idx_community_notification_unread", `
			CREATE INDEX IF NOT EXISTS idx_community_notification_unread
			    ON community_notification(site, user_id)
			    WHERE read_at IS NULL`},
		{"idx_community_notification_feed", `
			CREATE INDEX IF NOT EXISTS idx_community_notification_feed
			    ON community_notification(site, seq)`},
		{"idx_community_notification_thread_unread", `
			CREATE INDEX IF NOT EXISTS idx_community_notification_thread_unread
			    ON community_notification(thread_id, user_id)
			    WHERE read_at IS NULL`},
		{"idx_community_notification_read", `
			CREATE INDEX IF NOT EXISTS idx_community_notification_read
			    ON community_notification(read_at)
			    WHERE read_at IS NOT NULL`},
		{"idx_community_event_pending", `
			CREATE INDEX IF NOT EXISTS idx_community_event_pending
			    ON community_event(attempt_after, id)
			    WHERE processed_at IS NULL`},
		{"idx_community_event_processed", `
			CREATE INDEX IF NOT EXISTS idx_community_event_processed
			    ON community_event(processed_at)
			    WHERE processed_at IS NOT NULL`},
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}
	// community_thread_user.site is the site of the user's latest interaction.
	// Production held 4,909 rows on 2026-09-16 (moyu 4,880, kungal 22, letmoe 7)
	// and no catalog-anchored thread, so every existing row belongs to its
	// thread's site. The column stays nullable because the one-off
	// cmd/import-moyu-comments inserts rows without it, and a reader falls back to
	// the thread's site when it is NULL.
	if err := db.Exec(`
		UPDATE community_thread_user AS tu
		   SET site = t.site
		  FROM community_thread AS t
		 WHERE tu.thread_id = t.id AND tu.site IS NULL`).Error; err != nil {
		return fmt.Errorf("backfill community_thread_user.site: %w", err)
	}
	return boardsSQL(db)
}

// boardsSQL (wave 15) turns community_board, which no face had ever written,
// into the home of every topic.
//
// slug becomes the board's URL key and is required. AutoMigrate never turns a
// nullable column NOT NULL, so the model tag alone would leave it nullable. The
// table held 0 rows in production and locally when this shipped (2026-09-16),
// so there is nothing to backfill first.
//
// Topics opened before boards existed name their board with a string the site
// minted: "main", on letmoe's 2 topics and letmoe-staging's 1 in production —
// the only board anchors anywhere. A board anchor is now a community_board id,
// so each such string becomes a board of that slug (named after the slug until
// the site renames it) and its threads move onto the id. Both statements match
// only topics on non-numeric board anchors, and none are left after the UPDATE,
// so a rerun changes nothing.
func boardsSQL(db *gorm.DB) error {
	for _, st := range []struct{ name, stmt string }{
		{"require board slug", `ALTER TABLE community_board ALTER COLUMN slug SET NOT NULL`},
		{"create boards for legacy anchors", `
			INSERT INTO community_board
			       (site, slug, name, position, format, status, content_rating,
			        topic_min_trust_level, reply_min_trust_level, created_at, updated_at)
			SELECT DISTINCT site, anchor_id, anchor_id, 0, 0, 0, 0, 0, 0, now(), now()
			  FROM community_thread
			 WHERE kind = 0 AND anchor_kind = 0 AND anchor_id !~ '^[0-9]+$'
			ON CONFLICT (site, slug) DO NOTHING`},
		{"re-anchor legacy topics onto board ids", `
			UPDATE community_thread AS t
			   SET anchor_id = b.id::text
			  FROM community_board AS b
			 WHERE t.kind = 0 AND t.anchor_kind = 0 AND t.anchor_id !~ '^[0-9]+$'
			   AND b.site = t.site AND b.slug = t.anchor_id`},
	} {
		if err := db.Exec(st.stmt).Error; err != nil {
			return fmt.Errorf("%s: %w", st.name, err)
		}
	}
	return nil
}
