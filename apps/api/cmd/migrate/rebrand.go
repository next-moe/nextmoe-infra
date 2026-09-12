package main

import (
	"log/slog"

	siteModel "api/internal/platform/site/model"

	"gorm.io/gorm"
)

const (
	legacyOPSiteDomain = "oauth.kungal.com"
	opSiteDomain       = "account.nextmoe.com"
	opSiteName         = "NextMoe·未萌 账号"
	adminSiteDomain    = "admin.nextmoe.dev"
	adminSiteName      = "NextMoe·未萌 管理台"
)

// rebrandNextMoeSites is the NextMoe identity cutover for the sites table.
//
// The OP row with domain oauth.kungal.com is UPDATEd in place to
// account.nextmoe.com / NextMoe·未萌 账号 so the row id is preserved — site_id
// is baked into live JWT claims, and a new row would mint a different id.
// A re-run with no oauth.kungal.com row is a no-op (0 rows). This must run
// before seed insert-if-missing on the new domain: otherwise a production
// cutover would insert a second OP row and leave existing tokens pointing at
// the old id.
//
// admin.nextmoe.dev is a new first-party site (the admin console as an OAuth
// RP). Insert-if-absent by domain so a re-run does not duplicate. The
// nextmoe-admin OAuth client is not created here — it is minted through the
// console at cutover so the secret never sits in source.
//
// Sessions are not touched. The logout purge is a manual runbook step;
// migrate is re-run on every deploy and must not DELETE FROM sessions.
func rebrandNextMoeSites(db *gorm.DB) error {
	// Once the canonical row exists, a main-built seed (a rollback, or the
	// stale GHCR migrate image the dev compose runs) re-creates the legacy
	// oauth.kungal.com row, and the rename below then dies on idx_sites_domain
	// (SQLSTATE 23505) on every subsequent run — before seedInitialData, so
	// roles/backfills were silently skipped too. The re-seeded row is a fresh
	// id carrying no tokens: delete it instead of renaming onto the conflict.
	var canonical int64
	if err := db.Model(&siteModel.Site{}).
		Where("domain = ?", opSiteDomain).
		Count(&canonical).Error; err != nil {
		return err
	}
	if canonical > 0 {
		res := db.Exec(`DELETE FROM sites WHERE domain = ?`, legacyOPSiteDomain)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			slog.Info("deleted re-seeded legacy OP site row",
				"domain", legacyOPSiteDomain,
				"rows", res.RowsAffected)
		}
	} else {
		res := db.Exec(`
			UPDATE sites
			SET domain = ?, name = ?, description = ?
			WHERE domain = ?
		`, opSiteDomain, opSiteName, opSiteName, legacyOPSiteDomain)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected > 0 {
			slog.Info("rebranded OP site row in place",
				"from", legacyOPSiteDomain,
				"to", opSiteDomain,
				"rows", res.RowsAffected)
		}
	}

	var existing siteModel.Site
	err := db.Where("domain = ?", adminSiteDomain).First(&existing).Error
	if err == nil {
		return nil
	}
	if err != gorm.ErrRecordNotFound {
		return err
	}

	admin := siteModel.Site{
		Name:        adminSiteName,
		Domain:      adminSiteDomain,
		Description: adminSiteName,
	}
	if err := db.Create(&admin).Error; err != nil {
		slog.Error("failed to create admin site", "domain", adminSiteDomain, "error", err)
		return err
	}
	slog.Info("Created site", "domain", adminSiteDomain)
	return nil
}
