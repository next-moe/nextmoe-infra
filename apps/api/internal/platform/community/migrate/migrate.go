// Package migrate owns the kun_community schema migration: the AutoMigrate
// table list plus the idempotent raw-SQL section for the indexes AutoMigrate
// cannot express (partial-unique and DESC-ordered composites). It is called by
// cmd/migrate-community and by the migrate integration test (which provisions
// its test database with the exact production schema).
package migrate

import (
	"fmt"

	"api/internal/platform/community/model"

	"gorm.io/gorm"
)

// Run applies the full community schema (tables + raw SQL). Idempotent: safe to
// run on every deploy and repeatedly against the same database. This step has
// NO seeds — the per-site default board is planted at letmoe cut-over time.
func Run(db *gorm.DB) error {
	if err := db.AutoMigrate(
		// community_thread first: community_post's thread_id FK and the thread
		// self-reference (merged_into_id) both point at it.
		&model.CommunityThread{},
		&model.CommunityPost{},
		&model.CommunityReaction{},
		&model.CommunityThreadUser{},
		&model.CommunityBoard{},
		&model.CommunityTrust{},
		&model.CommunityFlag{},
		&model.CommunityReviewItem{},
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
	for _, ix := range []struct{ name, stmt string }{
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
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}
	return nil
}
