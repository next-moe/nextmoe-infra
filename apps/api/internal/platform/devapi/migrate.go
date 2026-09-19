package devapi

import (
	"fmt"

	"gorm.io/gorm"
)

// Models returns the developer-platform models for AutoMigrate registration in
// cmd/migrate (the developer-platform tables in kun_galgame_infra).
func Models() []any {
	return []any{&DeveloperAPIKey{}, &DeveloperAPIUsage{}, &PolicyOverride{}}
}

// DropScopeApplications removes devapi_scope_applications, retired on
// 2026-08-25 together with the grant-only scope machinery: /v1/news stopped
// asking for news:read, so nothing is applied for any more and no code reads the
// table. Three production rows are discarded with it — one approved application
// (user 2) and two still pending (users 12525 and 115587, the second filed on
// 2026-08-26 while this retirement was already in flight) — because approval no
// longer grants anything and a pending application has nothing left to be
// decided. Keys
// minted under the old rule keep the literal "news:read" in their scopes jsonb;
// that string is inert history and is deliberately not cleaned out.
//
// Idempotent (IF EXISTS), which matters here: production runs the main-database
// migration on every deploy since 85ea4ab, so this statement re-runs forever.
func DropScopeApplications(db *gorm.DB) error {
	if err := db.Exec(`DROP TABLE IF EXISTS devapi_scope_applications`).Error; err != nil {
		return fmt.Errorf("devapi migrate: drop devapi_scope_applications: %w", err)
	}
	return nil
}

// AddOAuthClientDevColumns adds the developer-platform columns to an existing
// oauth_clients table using raw SQL (裁定 8). A fresh database is a no-op here:
// the later AutoMigrate call creates the table with every column already
// present. For an existing table, the NOT NULL intent columns are added WITH a
// temporary DEFAULT so existing rows backfill, then the DEFAULT is dropped so
// the GORM zero-value INSERT trap can't reintroduce a silent default. Two
// columns keep their DEFAULT because the Go model never writes them explicitly
// — dev_nsfw_allowed (the field is gone) and store_settlement_eligible (GORM
// omits a zero-valued field that has a default) — see the notes at those
// statements.
// owner_user_id is nullable (third-party app owner; NULL for first-party site
// clients) and gets a plain index.
//
// Idempotent — ADD COLUMN IF NOT EXISTS + a no-op DROP/SET DEFAULT on re-run, so
// a second migration is a zero-change pass. cmd/migrate MUST call this BEFORE
// AutoMigrate(oauth_clients) so AutoMigrate never tries to add a NOT NULL column
// to a populated table (which Postgres rejects). Column types match GORM's
// mapping for the model fields (Go int → bigint, string size:20 → varchar(20)),
// so AutoMigrate reconciles to a no-op afterward.
func AddOAuthClientDevColumns(db *gorm.DB) error {
	if !db.Migrator().HasTable("oauth_clients") {
		return nil
	}

	stmts := []string{
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS owner_user_id bigint`,
		`CREATE INDEX IF NOT EXISTS idx_oauth_clients_owner_user_id ON oauth_clients (owner_user_id)`,

		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_enabled boolean NOT NULL DEFAULT false`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_enabled DROP DEFAULT`,
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_tier varchar(20) NOT NULL DEFAULT 'free'`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_tier DROP DEFAULT`,
		// 2026-08-25, the NSFW capability is retired: any key may ask for
		// nsfw=true. The column stays (dropping it needs a separate outage-class
		// migration and the historical grants are still readable), but its
		// DEFAULT is restored instead of dropped — OAuthClient no longer carries
		// DevNSFWAllowed, so every INSERT now omits the column and a NOT NULL
		// column with no default would reject it.
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_nsfw_allowed boolean NOT NULL DEFAULT false`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_nsfw_allowed SET DEFAULT false`,
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_rate_per_min bigint NOT NULL DEFAULT 0`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_rate_per_min DROP DEFAULT`,
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_quota_daily bigint NOT NULL DEFAULT 0`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_quota_daily DROP DEFAULT`,

		// 2026-08-18, the app-approval flow. Every row that already exists was
		// created when creation was unconditionally self-service, so the
		// temporary DEFAULT backfills them all to 'approved' — anything else
		// would retroactively suspend live third-party applications. The
		// DEFAULT is then dropped, and rows inserted by the OAuth console
		// (which does not know these columns) land '' — read as approved by
		// appAwaitsReview, deliberately fail-open for first-party clients.
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_review_status varchar(20) NOT NULL DEFAULT 'approved'`,
		`ALTER TABLE oauth_clients ALTER COLUMN dev_review_status DROP DEFAULT`,
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_review_note text`,

		// 2026-08-29, the application lifecycle. dev_archived_at is nullable and
		// every existing row backfills to NULL — nothing was archived before the
		// column existed, and reading "created before the feature" as "deleted"
		// would empty every developer's application list.
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS dev_archived_at timestamptz`,

		// 2026-08-29, the settlement roster. NOT NULL DEFAULT false and the
		// DEFAULT is KEPT (unlike the dev_* columns above): false is Go's zero
		// value, so GORM omits the column from every INSERT and a NOT NULL
		// column with no default would reject the row. Existing rows backfill to
		// false on purpose — nine applications already hold store:read, and
		// backfilling them to true would silently enrol whoever happened to tick
		// the box before the roster existed.
		`ALTER TABLE oauth_clients ADD COLUMN IF NOT EXISTS store_settlement_eligible boolean NOT NULL DEFAULT false`,
		// 2026-09-19: every application created from now on joins the roster
		// (operator ruling); the ones already on file keep what they have, and
		// an operator can still take any of them off. With a true default GORM
		// omits a false field from the INSERT, so no code path can create an
		// application off the roster — they all switch it off afterwards.
		`ALTER TABLE oauth_clients ALTER COLUMN store_settlement_eligible SET DEFAULT true`,
	}
	for _, s := range stmts {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("devapi migrate %q: %w", s, err)
		}
	}
	return nil
}

// RestoreKeyNSFWDefault re-adds a DEFAULT to developer_api_keys.nsfw_allowed,
// retired with the NSFW capability on 2026-08-25. The column is kept — a DROP
// COLUMN is outage-class and the historical grants are still worth reading —
// but DeveloperAPIKey no longer has the field, so every minted key now INSERTs
// without it and the NOT NULL column would reject the row. Guarded on the
// column: a database created after this change never has it, because
// AutoMigrate builds developer_api_keys from the model.
//
// Idempotent, and cmd/migrate calls it BEFORE AutoMigrate. Migrate FIRST, then
// deploy: the new binary cannot mint a key against a column that still demands
// an explicit value.
func RestoreKeyNSFWDefault(db *gorm.DB) error {
	if !db.Migrator().HasTable("developer_api_keys") || !db.Migrator().HasColumn("developer_api_keys", "nsfw_allowed") {
		return nil
	}
	s := `ALTER TABLE developer_api_keys ALTER COLUMN nsfw_allowed SET DEFAULT false`
	if err := db.Exec(s).Error; err != nil {
		return fmt.Errorf("devapi migrate %q: %w", s, err)
	}
	return nil
}

// AddUsagePathColumn widens developer_api_usage from one row per
// (client, key, face, day) to one row per (client, key, face, day, ROUTE
// PATTERN), so "which public endpoints do third-party keys actually call" is
// answerable — before this the table only knew face=catalog|galgame|…, which
// makes any deprecation decision blind.
//
// Existing rows are backfilled to the empty string rather than dropped or
// guessed: they were recorded when the process did not know the route, and
// empty is the only honest value for "counted before per-path telemetry
// existed". Every reader aggregates with SUM()/GROUP BY over columns that are
// not path, so their numbers are unchanged by the split; only row COUNTS grow.
//
// The unique index has to be dropped here rather than just extended in the GORM
// tag: AutoMigrate creates an index when the name is absent and otherwise
// leaves it alone, so the 4-column idx_usage_day would survive untouched and
// the first two route patterns of one face and day would collide on 23505 —
// which fails the whole batched INSERT, not one row.
//
// Idempotent: ADD COLUMN IF NOT EXISTS, and the drop is guarded on the index
// not already covering path, so a re-run does not churn the index. cmd/migrate
// MUST call this BEFORE AutoMigrate — the column is NOT NULL and the table is
// populated, and AutoMigrate then recreates idx_usage_day in its 5-column shape.
//
// Migrate FIRST, then deploy: code that writes path against a table without the
// column fails every flush.
func AddUsagePathColumn(db *gorm.DB) error {
	if !db.Migrator().HasTable("developer_api_usage") {
		return nil
	}
	for _, s := range []string{
		`ALTER TABLE developer_api_usage ADD COLUMN IF NOT EXISTS path text NOT NULL DEFAULT ''`,
		`ALTER TABLE developer_api_usage ALTER COLUMN path DROP DEFAULT`,
	} {
		if err := db.Exec(s).Error; err != nil {
			return fmt.Errorf("devapi migrate %q: %w", s, err)
		}
	}
	var stale bool
	if err := db.Raw(`SELECT EXISTS (
		SELECT 1 FROM pg_indexes
		WHERE schemaname = current_schema() AND tablename = 'developer_api_usage'
		  AND indexname = 'idx_usage_day' AND indexdef NOT LIKE '%path%')`).Scan(&stale).Error; err != nil {
		return fmt.Errorf("devapi migrate: inspect idx_usage_day: %w", err)
	}
	if stale {
		if err := db.Exec(`DROP INDEX idx_usage_day`).Error; err != nil {
			return fmt.Errorf("devapi migrate: drop the pre-path idx_usage_day: %w", err)
		}
	}
	return nil
}
