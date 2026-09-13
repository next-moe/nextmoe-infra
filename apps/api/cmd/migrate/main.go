// cmd/migrate — the single schema-migration entry point for every database this
// module owns.
//
//	migrate              # the platform database (cfg.Database): auth, sites, devapi…
//	migrate -drop        # …dropping every table first (DANGEROUS)
//	migrate <domain>     # one domain database — catalog | community | trust | ai | news
//
// The per-domain targets used to be five near-identical cmd/migrate-* binaries
// shipped as five separate images; they are table-driven in domain.go now.
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"api/internal/infrastructure/database"
	"api/pkg/config"
	"api/pkg/logger"

	// Import all models
	jobsModel "api/internal/jobs/model"
	authModel "api/internal/platform/auth/model"
	"api/internal/platform/devapi"
	"api/internal/platform/permissions"
	"api/internal/platform/settings"
	siteModel "api/internal/platform/site/model"
	storeModel "api/internal/platform/store/model"

	"gorm.io/gorm"
)

func main() {
	target, args := splitTarget(os.Args[1:])

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger.Init(cfg.Server.Env)

	if target != "" {
		if err := runDomain(target, cfg); err != nil {
			slog.Error("migration failed", "target", target, "error", err)
			os.Exit(1)
		}
		return
	}

	runPlatform(cfg, args)
}

// splitTarget takes the optional domain subcommand off the front of the
// arguments. It has to happen before any flag parsing: flag.Parse stops at the
// first non-flag argument, so `migrate catalog -drop` would parse no flags at
// all and silently ignore -drop.
func splitTarget(args []string) (string, []string) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args[0], args[1:]
	}
	return "", args
}

func runPlatform(cfg *config.Config, args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	dropTables := fs.Bool("drop", false, "Drop all tables before migration (DANGEROUS)")
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	// Connect to database
	db, err := database.NewPostgresDB(cfg.Database)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	gormDB := db.DB()

	// Drop tables if requested
	if *dropTables {
		slog.Warn("Dropping all tables...")
		if err := dropAllTables(gormDB); err != nil {
			slog.Error("failed to drop tables", "error", err)
			os.Exit(1)
		}
		slog.Info("All tables dropped")
	}

	// Run migrations
	slog.Info("Running migrations...")

	// Developer-platform columns on the EXISTING oauth_clients table must be
	// added via raw SQL (NOT NULL backfilled + DROP DEFAULT) BEFORE AutoMigrate,
	// so AutoMigrate never tries to add a NOT NULL column to a populated table.
	// Idempotent. See devapi.AddOAuthClientDevColumns / 裁定 8.
	if err := devapi.AddOAuthClientDevColumns(gormDB); err != nil {
		slog.Error("failed to add developer-platform columns", "error", err)
		os.Exit(1)
	}

	// developer_api_keys.nsfw_allowed outlives the NSFW capability that owned
	// it (retired 2026-08-25) and is no longer on the model, so it needs its
	// DEFAULT back before the first key is minted without the column.
	// Idempotent. See devapi.RestoreKeyNSFWDefault.
	if err := devapi.RestoreKeyNSFWDefault(gormDB); err != nil {
		slog.Error("failed to restore the developer key nsfw_allowed default", "error", err)
		os.Exit(1)
	}

	// developer_api_usage.path (the matched route pattern) + the 4→5 column
	// rebuild of idx_usage_day, for the same reason and one more: AutoMigrate
	// never alters an index that already exists, so the widened unique key has
	// to be dropped in raw SQL here or the first two route patterns of a face
	// collide. Idempotent. See devapi.AddUsagePathColumn.
	if err := devapi.AddUsagePathColumn(gormDB); err != nil {
		slog.Error("failed to add the developer usage path column", "error", err)
		os.Exit(1)
	}

	// role_permission_overrides.effect, for the same reason: the overlay gained
	// its deny half on 2026-08-04 and the column is NOT NULL, so a table that
	// already holds (necessarily grant) rows must be backfilled in raw SQL
	// before AutoMigrate sees it. Idempotent.
	if err := permissions.AddOverrideEffectColumn(gormDB); err != nil {
		slog.Error("failed to add the permission-overlay effect column", "error", err)
		os.Exit(1)
	}

	// Get all models to migrate
	models := getAllModels()

	if err := gormDB.AutoMigrate(models...); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	slog.Info("Migrations completed successfully")

	// Retire the dead authz structures the permission-first migration left
	// behind (zombie permissions/role_permissions tables + user_site_data.role
	// column). Runs after AutoMigrate so it isn't undone by a recreate;
	// idempotent, so re-running migrate is a safe no-op.
	if err := dropRetiredAuthzStructures(gormDB); err != nil {
		slog.Error("failed to drop retired authz structures", "error", err)
		os.Exit(1)
	}

	// Retire the dead moderation-skeleton tables (the moderation skeleton
	// service was removed; Trust & Safety is the dedicated kun_trust DB).
	// Idempotent DROP IF EXISTS, so a re-run is a no-op.
	if err := dropRetiredModerationTables(gormDB); err != nil {
		slog.Error("failed to drop retired moderation tables", "error", err)
		os.Exit(1)
	}

	// Retire devapi_scope_applications: /v1/news stopped asking for news:read on
	// 2026-08-25, so nothing is applied for. Runs after AutoMigrate for the same
	// reason as the two drops above — the model is gone, but a stale binary that
	// still carried it would otherwise recreate the table underneath us.
	// See devapi.DropScopeApplications for what the discarded rows were.
	if err := devapi.DropScopeApplications(gormDB); err != nil {
		slog.Error("failed to drop the retired devapi scope applications table", "error", err)
		os.Exit(1)
	}

	// At most one PENDING creator application per user (GORM can't express a
	// partial unique index). Backstops the service-layer "one pending" guard.
	if err := gormDB.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS uq_creator_app_pending
		ON creator_applications (user_id) WHERE status = 'pending'
	`).Error; err != nil {
		slog.Error("failed to create creator_applications partial unique index", "error", err)
		os.Exit(1)
	}

	// Case-insensitive email lookups. Login / forgot-password / existence checks
	// query LOWER(email) (email is case-insensitive in practice); this functional
	// index keeps those lookups index-backed. NON-unique on purpose: legacy data
	// has a few case-variant duplicate emails, so a unique LOWER(email) index
	// can't be created until those are deduped.
	if err := gormDB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_users_email_lower ON users (LOWER(email))
	`).Error; err != nil {
		slog.Error("failed to create users LOWER(email) index", "error", err)
		os.Exit(1)
	}

	// In-place OP domain rename + admin site insert. Must run before seed:
	// seed insert-if-missing keys on domain, so renaming first is what stops
	// a production cutover from inserting a duplicate OP row.
	if err := rebrandNextMoeSites(gormDB); err != nil {
		slog.Error("failed to rebrand NextMoe sites", "error", err)
		os.Exit(1)
	}

	// Create initial data if needed
	if err := seedInitialData(gormDB, cfg.Server.Env); err != nil {
		slog.Error("failed to seed initial data", "error", err)
		os.Exit(1)
	}

	slog.Info("Database setup completed")
}

// getAllModels returns all models to be migrated
func getAllModels() []any {
	return []any{
		// Auth models
		&authModel.User{},
		&authModel.Session{},
		&authModel.OAuthAccount{},
		&authModel.UserFollow{},
		&authModel.UserSiteData{},
		&authModel.UserSiteRole{},
		&authModel.UserMigration{},
		&authModel.PasswordReset{},
		&authModel.AuthorizationCode{},
		&authModel.MoemoepointLog{},
		&authModel.CreatorApplication{},
		&authModel.SigningKey{},

		// Site models
		&siteModel.Site{},
		&siteModel.OAuthClient{},
		&siteModel.Role{},

		// Developer platform (NextMoe open API): API keys + usage rollup + the
		// platform policy matrix (2026-08-18: one row per capability that departs
		// from the code default; no row = the default). The grant-only scope
		// applications added here on 2026-08-18 were dropped on 2026-08-25 — see
		// devapi.DropScopeApplications, called after AutoMigrate below.
		// The oauth_clients dev_* / dev_review_* columns are handled by
		// devapi.AddOAuthClientDevColumns above (raw SQL, pre-AutoMigrate).
		&devapi.DeveloperAPIKey{},
		&devapi.DeveloperAPIUsage{},
		&devapi.PolicyOverride{},

		// NOTE: artifact models (Artifact/Manifest) moved to the dedicated
		// kun_artifacts DB — migrated by cmd/artifact's AutoMigrate, not here.
		// See docs/artifact/02-storage-and-schema.md.

		// NOTE: the moderation skeleton (moderation_jobs / moderation_results)
		// was retired with the moderation skeleton service; the dead tables
		// are dropped by
		// dropRetiredModerationTables below. Trust & Safety lives in the
		// dedicated kun_trust DB (`migrate trust`).

		// Permission overlay + its audit trail (docs/auth/04 §7). The overlay is
		// ALLOW-ONLY on top of the compiled-in bundles: these tables can widen a
		// role's permissions but never cut below the code floor.
		&permissions.RolePermissionOverride{},
		&permissions.PermissionAuditLog{},

		// Runtime configuration overrides on top of code defaults, and their
		// audit trail (2026-09-02). Brand-new tables, no existing rows to convert.
		&settings.SettingOverride{},
		&settings.SettingAuditLog{},

		// DLsite distribution face (/v1/store, wave 02 of the store track,
		// 2026-08-25). Four brand-new tables, no pre-existing rows to convert:
		// the two link tables map (calling site, product|campaign) → the short
		// link minted for it, store_campaigns is the coupon-campaign window
		// (rows inserted by hand — there is no admin face yet), and
		// store_link_daily_stats caches the redirector's JST-day click buckets
		// so the portal and settlement read this database instead of fanning out.
		&storeModel.PurchaseLink{},
		&storeModel.CouponLink{},
		&storeModel.Campaign{},
		&storeModel.LinkDailyStat{},

		// store_price_quotes (2026-09-01). Brand-new table, no rows to convert:
		// the catalog service's price fetch writes observed storefront quotes
		// and the /v2/store/prices face reads them.
		&storeModel.PriceQuote{},

		// Job registry observability
		&jobsModel.JobRun{},
	}
}

// dropAllTables drops all tables (for development only)
func dropAllTables(db *gorm.DB) error {
	models := getAllModels()
	// Reverse order to handle foreign keys
	for i := len(models) - 1; i >= 0; i-- {
		if err := db.Migrator().DropTable(models[i]); err != nil {
			// Ignore errors (table might not exist)
			slog.Debug("drop table skipped", "error", err)
		}
	}
	// Also drop the live user_roles join table (not a model, so not in the
	// loop above). The retired role_permissions join table dies with its
	// permissions model — dropRetiredAuthzStructures handles it on the normal
	// migrate path.
	if err := db.Migrator().DropTable("user_roles"); err != nil {
		slog.Debug("drop user_roles skipped", "error", err)
	}
	return nil
}

// dropRetiredAuthzStructures removes the dead structures left by the
// permission-first authorization migration (refs step 03): the zombie
// permissions / role_permissions tables (authorization is now permission-first,
// bundled in the per-domain perm packages — no permissions table) and the dead
// per-site user_site_data.role numeric column (never read by any live
// enforcement). Idempotent — each drop is guarded, so a re-run is a no-op.
func dropRetiredAuthzStructures(db *gorm.DB) error {
	m := db.Migrator()
	// Order matters: drop the join table before the main table it references.
	if m.HasTable("role_permissions") {
		if err := m.DropTable("role_permissions"); err != nil {
			return fmt.Errorf("drop role_permissions: %w", err)
		}
		slog.Info("dropped retired table role_permissions")
	}
	if m.HasTable("permissions") {
		if err := m.DropTable("permissions"); err != nil {
			return fmt.Errorf("drop permissions: %w", err)
		}
		slog.Info("dropped retired table permissions")
	}
	// The dead per-site numeric role column (pass the DB column name — the
	// struct field is gone).
	if m.HasColumn(&authModel.UserSiteData{}, "role") {
		if err := m.DropColumn(&authModel.UserSiteData{}, "role"); err != nil {
			return fmt.Errorf("drop user_site_data.role: %w", err)
		}
		slog.Info("dropped retired column user_site_data.role")
	}
	return nil
}

// dropRetiredModerationTables removes the dead moderation-skeleton tables left
// behind by the retired moderation skeleton service (the stub provider stack, doc 02
// §8 / doc 18 P0). Trust & Safety now owns its own kun_trust database
// (`migrate trust`). The two tables carried no data worth keeping (the stub
// always returned approved). Order: moderation_results references
// moderation_jobs, so drop the child first. Idempotent — guarded DROP IF
// EXISTS, so a re-run is a no-op.
func dropRetiredModerationTables(db *gorm.DB) error {
	if err := db.Exec(`DROP TABLE IF EXISTS moderation_results`).Error; err != nil {
		return fmt.Errorf("drop moderation_results: %w", err)
	}
	if err := db.Exec(`DROP TABLE IF EXISTS moderation_jobs`).Error; err != nil {
		return fmt.Errorf("drop moderation_jobs: %w", err)
	}
	return nil
}
