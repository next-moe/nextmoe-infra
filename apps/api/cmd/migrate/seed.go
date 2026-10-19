package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"os"
	"strings"

	siteModel "api/internal/platform/site/model"

	"gorm.io/gorm"
)

// seedInitialData creates initial required data
func seedInitialData(db *gorm.DB, env string) error {
	// Sites — mirrors the production deployment (snapshot 2026-05). Idempotent:
	// the per-domain WHERE-First check skips rows that already exist, so re-
	// running migrate after admin manually edits site metadata via UI does
	// NOT clobber their changes.
	//
	// OAuth clients are NOT seeded here — they carry per-environment
	// secrets that have no business sitting in source. Admin creates each
	// client manually through the OAuth admin UI after sites are seeded;
	// the UI's "create client" path generates a fresh secret and shows
	// it once (consumer-side `.env` then pastes it in).
	//
	// Exception: the local developer-portal SSO client. The portal is
	// SSO-only. refresh-dev-db restores the prod snapshot, whose portal
	// client only accepts https://developer.nextmoe.dev/auth/callback, so
	// GET /oauth/authorize for `devportal-dev` answers 无效的客户端.
	// The secret is the public `dev-secret-<client_id>` contract. Development
	// only — production already has its own client.
	//
	// To add a new first-party site:
	//   1. Append to this slice (and update the prod DB by re-running migrate).
	//   2. Have admin create the OAuth client via UI on the new site.
	//   3. Optionally add the domain to `firstPartyDomains` below so the
	//      auto_consent backfill catches it (or admin toggles in the UI).
	sites := []siteModel.Site{
		{Name: opSiteName, Domain: opSiteDomain, Description: opSiteName},
		{Name: adminSiteName, Domain: adminSiteDomain, Description: adminSiteName},
		{Name: editorSiteName, Domain: editorSiteDomain, Description: editorSiteName},
		{Name: "鲲 Galgame 论坛", Domain: "www.kungal.com", Description: "鲲 Galgame 论坛"},
		{Name: "鲲 Galgame 补丁", Domain: "www.moyu.moe", Description: "鲲 Galgame 补丁"},
		{Name: "鲲 Galgame AI", Domain: "ai.kungal.com", Description: "鲲 Galgame AI"},
		{Name: "鲲 Galgame 表情包", Domain: "sticker.kungal.com", Description: "鲲 Galgame 表情包"},
	}

	for _, s := range sites {
		var existing siteModel.Site
		if err := db.Where("domain = ?", s.Domain).First(&existing).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				if err := db.Create(&s).Error; err != nil {
					slog.Error("failed to create site", "domain", s.Domain, "error", err)
					return err
				}
				slog.Info("Created site", "domain", s.Domain)
			}
		}
	}

	// Create default roles
	defaultRoles := []siteModel.Role{
		{Name: "user", Description: "Regular user"},
		// editor（编辑者）— granted by approving an editor application: the
		// holder's catalog edits merge without review, within a daily quota.
		// No review power over anyone else's proposals. The catalog
		// permissions behind it live in catalog/perm.
		{Name: "editor", Description: "编辑者 — catalog edits merge without review, within a daily quota"},
		// creator（创作者）— trusted publisher tier between user and moderator.
		// Flat RBAC: granted ALONGSIDE user, gates publish-trust capabilities
		// (direct galgame publish incl. without a VNDB id) without any moderation
		// power. Auto-granted on contribution threshold + admin-grantable.
		// See docs/auth/01-creator-role-design.md.
		{Name: "creator", Description: "创作者 — trusted creator (direct galgame publish)"},
		{Name: "moderator", Description: "Content moderator"},
		{Name: "admin", Description: "Administrator"},
		// ren（莲）— elevated operator above admin. Flat RBAC has no
		// hierarchy, so ren is a SEPARATE role granted ALONGSIDE admin to a
		// tiny set of fully-trusted owners. It gates the genuinely dangerous
		// OAuth-admin capabilities that ordinary admins should not hold:
		//   - granting the image:upload scope to a client
		//   - flipping a client to auto_consent (silent first-party authorize)
		//   - seeing user email / IP in the admin user list & detail
		// Enforcement lives at the handlers (site_handler, admin_handler) via
		// the site/perm permission bundles (oauth.* capabilities).
		{Name: "ren", Description: "莲 — elevated operator above admin"},
	}

	for _, role := range defaultRoles {
		var existing siteModel.Role
		if err := db.First(&existing, "name = ?", role.Name).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				if err := db.Create(&role).Error; err != nil {
					slog.Error("failed to create role", "role", role.Name, "error", err)
					return err
				}
				slog.Info("Created role", "name", role.Name)
			}
		}
	}

	// Backfill: any existing OAuth client whose grants is exactly
	// ["authorization_code"] gets refresh_token added too. Without this,
	// every pre-existing client breaks the moment a refresh attempt hits
	// the grant-allowlist check we just added (15 min after any login).
	//
	// Idempotent: the JSONB equality check skips clients that already
	// have refresh_token or some other grants config. Doesn't touch
	// clients with explicit single-grant configurations other than the
	// historical default.
	res := db.Exec(`
		UPDATE oauth_clients
		SET grants = '["authorization_code","refresh_token"]'::jsonb
		WHERE grants::jsonb = '["authorization_code"]'::jsonb
	`)
	if res.Error != nil {
		// Don't fail migration — log and continue. Existing deployments
		// can recover via a manual UPDATE if needed.
		slog.Warn("failed to backfill oauth_clients.grants", "error", res.Error)
	} else if res.RowsAffected > 0 {
		slog.Info("Backfilled oauth_clients.grants with refresh_token", "rows", res.RowsAffected)
	}

	// Backfill: flip auto_consent=true for first-party clients so the
	// unified-registration redirect chain skips the consent UI on
	// kungal / moyu / wiki / ai / sticker / admin.nextmoe.dev. The column
	// itself is added by GORM AutoMigrate from siteModel.OAuthClient.AutoConsent;
	// this step only seeds the values.
	//
	// Targeted by the parent Site.Domain (resolved by JOIN), NOT by
	// client_id, so freshly-created clients on these domains in any
	// environment (dev/staging/prod) get the right default without
	// hardcoding env-specific UUIDs. Idempotent: WHERE auto_consent =
	// false ensures re-runs after admin manually toggles a row don't
	// undo their choice.
	//
	// First-party = "owned by the OAuth platform itself" = same team
	// can audit/respond to security incidents. Adding new domains here
	// means committing to keeping them secure end-to-end. Policy:
	// docs/integration/oauth/05-registration.md §auto_consent.
	firstPartyDomains := []string{
		"www.kungal.com",
		"www.moyu.moe",
		"ai.kungal.com",
		"sticker.kungal.com",
		adminSiteDomain,
		editorSiteDomain,
	}
	res = db.Exec(`
		UPDATE oauth_clients
		SET auto_consent = true
		WHERE auto_consent = false
		  AND site_id IN (SELECT id FROM sites WHERE domain IN ?)
	`, firstPartyDomains)
	if res.Error != nil {
		slog.Warn("failed to backfill oauth_clients.auto_consent", "error", res.Error)
	} else if res.RowsAffected > 0 {
		slog.Info("Backfilled oauth_clients.auto_consent for first-party clients", "rows", res.RowsAffected)
	}

	// Backfill: moemoepoint awarder allow-list. The service-to-service
	// POST /users/:id/moemoepoint (mint) endpoint is gated on
	// oauth_clients.moemoepoint_awarder (column added by GORM AutoMigrate from
	// siteModel.OAuthClient, default false = fail-closed). The only legitimate
	// minters are the forum + patch backends; flip them on by parent
	// Site.Domain so freshly-created clients on these domains in any
	// environment get it without hardcoding per-env client UUIDs. Idempotent:
	// WHERE moemoepoint_awarder = false skips re-runs / manual toggles.
	//
	// New content sites are deliberately NOT added here. A client that only
	// READS the balance (e.g. to seed its own local economy) must stay
	// fail-closed — minting into the shared wallet would stamp its provenance
	// onto every user's ledger. Add a domain only when that site legitimately
	// awards into the shared currency. Policy:
	// docs/integration/oauth/06-moemoepoint.md §awarder allow-list.
	awarderDomains := []string{
		"www.kungal.com",
		"www.moyu.moe",
	}
	res = db.Exec(`
		UPDATE oauth_clients
		SET moemoepoint_awarder = true
		WHERE moemoepoint_awarder = false
		  AND site_id IN (SELECT id FROM sites WHERE domain IN ?)
	`, awarderDomains)
	if res.Error != nil {
		slog.Warn("failed to backfill oauth_clients.moemoepoint_awarder", "error", res.Error)
	} else if res.RowsAffected > 0 {
		slog.Info("Backfilled oauth_clients.moemoepoint_awarder for awarder clients", "rows", res.RowsAffected)
	}

	// Backfill: moyu's community tenant. community_site (GORM AutoMigrate from
	// siteModel.OAuthClient) overrides catalog_site as the community service's
	// tenant, and moyu is the site that needs it: its client files catalog
	// claims as `kungal` and cannot stop, which put its comment walls in the
	// forum's tenant — where a purge from moyu's admin panel scrubbed the
	// user's forum comments, the feeds answered the forum's rows, and the
	// notification badge counted the forum's unread. Set by parent Site.Domain
	// like the awarder list above, so a freshly created client on that domain
	// in any environment lands in the right tenant rather than waiting for a
	// hand-run UPDATE nobody remembers. Idempotent, and a value already set by
	// hand is left alone.
	//
	// It matters that this runs BEFORE the first write: the tenant is stamped
	// into every thread row, so a site that posts under the wrong one has to be
	// migrated rather than re-bound.
	res = db.Exec(`
		UPDATE oauth_clients
		SET community_site = 'moyu'
		WHERE COALESCE(community_site, '') = ''
		  AND site_id IN (SELECT id FROM sites WHERE domain = 'www.moyu.moe')
	`)
	if res.Error != nil {
		slog.Warn("failed to backfill oauth_clients.community_site", "error", res.Error)
	} else if res.RowsAffected > 0 {
		slog.Info("Backfilled oauth_clients.community_site for moyu", "rows", res.RowsAffected)
	}

	if env == "development" {
		if err := seedDevPortalClient(db); err != nil {
			return err
		}
	}

	if err := seedEditorClient(db, os.Getenv("KUN_EDITOR_PUBLIC_URL")); err != nil {
		return err
	}

	return nil
}

// seedEditorClient creates the editor platform's OAuth client. It is a public
// PKCE client — its BFF holds the tokens in httpOnly cookies and there is no
// secret to keep, so unlike every other first-party client it can be seeded
// instead of minted through the console. The stored secret is a random hash
// nobody ever sees. Create-only: a console edit to the row is never undone by
// a later deploy. Skipped where KUN_EDITOR_PUBLIC_URL is unset.
func seedEditorClient(db *gorm.DB, publicURL string) error {
	publicURL = strings.TrimRight(strings.TrimSpace(publicURL), "/")
	if publicURL == "" {
		slog.Info("KUN_EDITOR_PUBLIC_URL unset; not seeding the nextmoe-editor client")
		return nil
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return err
	}
	redirects, _ := json.Marshal([]string{publicURL + "/auth/callback"})
	res := db.Exec(`
		INSERT INTO oauth_clients
		  (id, site_id, name, secret, redirect_uris, grants, is_public, auto_consent,
		   refresh_token_ttl_seconds, allowed_scopes, catalog_site, union_site_roles,
		   store_settlement_eligible,
		   dev_enabled, dev_tier, dev_rate_per_min, dev_quota_daily, dev_review_status)
		VALUES
		  ('nextmoe-editor', (SELECT id FROM sites WHERE domain = ?), ?, ?,
		   ?::jsonb, '["authorization_code","refresh_token"]'::jsonb, true, true,
		   7776000, '["openid","profile","catalog:read","catalog:edit"]'::jsonb, 'editor', true,
		   false,
		   false, '', 0, 0, '')
		ON CONFLICT (id) DO NOTHING
	`, editorSiteDomain, editorSiteName,
		siteModel.HashOAuthClientSecret(hex.EncodeToString(raw)), string(redirects))
	if res.Error != nil {
		slog.Error("failed to seed oauth_clients.nextmoe-editor", "error", res.Error)
		return res.Error
	}
	if res.RowsAffected > 0 {
		slog.Info("Seeded oauth_clients.nextmoe-editor", "redirect", publicURL+"/auth/callback")
	}
	return nil
}

func seedDevPortalClient(db *gorm.DB) error {
	secret := siteModel.HashOAuthClientSecret("dev-secret-devportal-dev")
	res := db.Exec(`
		INSERT INTO oauth_clients
		  (id, name, secret, redirect_uris, grants, is_public, auto_consent,
		   refresh_token_ttl_seconds, allowed_scopes,
		   image_enabled, catalog_site,
		   dev_enabled, dev_tier, dev_rate_per_min, dev_quota_daily,
		   dev_review_status)
		VALUES
		  ('devportal-dev', 'NextMoe 开发者平台 (dev)', ?,
		   '["http://127.0.0.1:9430/auth/callback"]'::jsonb,
		   '["authorization_code","refresh_token"]'::jsonb,
		   false, true, 7776000, '["openid","profile","email"]'::jsonb,
		   false, '',
		   false, '', 0, 0,
		   'approved')
		ON CONFLICT (id) DO UPDATE SET
		  secret = EXCLUDED.secret,
		  redirect_uris = EXCLUDED.redirect_uris,
		  grants = EXCLUDED.grants,
		  auto_consent = EXCLUDED.auto_consent,
		  allowed_scopes = EXCLUDED.allowed_scopes
	`, secret)
	if res.Error != nil {
		slog.Error("failed to seed oauth_clients.devportal-dev", "error", res.Error)
		return res.Error
	}
	if res.RowsAffected > 0 {
		slog.Info("Seeded oauth_clients.devportal-dev")
	}
	return nil
}
