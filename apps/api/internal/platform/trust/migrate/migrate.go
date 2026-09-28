// Package migrate owns the kun_trust schema migration: the AutoMigrate table
// list, the idempotent raw-SQL section for the indexes AutoMigrate cannot
// express (partial-unique + DESC-ordered composites), and the global report-
// reason seed. It is called by cmd/migrate-trust and by the trust integration
// tests (which provision their database with the exact production schema).
package migrate

import (
	"fmt"

	"api/internal/platform/trust/model"

	"gorm.io/gorm"
)

// Run applies the full trust schema (tables + raw SQL) and seeds the six global
// report reasons. Idempotent: safe to run on every deploy and repeatedly
// against the same database.
func Run(db *gorm.DB) error {
	if err := preAutoMigrate(db); err != nil {
		return err
	}
	if err := db.AutoMigrate(
		// Reason before report (report.reason_id FK); review_item before both
		// report (review_item_id, no FK) and disposition (review_item_id FK).
		&model.TrustReportReason{},
		&model.TrustSubjectKind{},
		&model.TrustReviewItem{},
		&model.TrustReport{},
		&model.TrustDisposition{},
		&model.TrustAuditLog{},
		// AI shadow-scoring pipeline (step 03); no FK — subject is referenced by
		// (site, subject_kind, subject_id) like every other trust table. Step 05
		// adds the nullable tier0_matched jsonb column (AutoMigrate adds it).
		&model.TrustScanResult{},
		// Tier0 deterministic word list (step 05); no FK — a per-site (or global)
		// registry of normalized substrings.
		&model.TrustTerm{},
		// Per-site moderation posture (step 07 M0); no FK — site is a bare tenant
		// string here as everywhere else in this schema. Every column is a
		// nullable override, so an absent row = today's platform-wide behaviour
		// and creating the table changes nothing.
		&model.TrustSitePolicy{},
	); err != nil {
		return fmt.Errorf("trust automigrate: %w", err)
	}
	if err := rawSQL(db); err != nil {
		return err
	}
	if err := seedReasons(db); err != nil {
		return err
	}
	if err := classifyImportedTermPurpose(db); err != nil {
		return err
	}
	if err := backfillGatewayFlagged(db); err != nil {
		return err
	}
	if err := backfillScanAttempts(db); err != nil {
		return err
	}
	return backfillSubjectAuthor(db)
}

// backfillSubjectAuthor gives review items opened before subject_author_id
// existed the author their subject's scans recorded; until then the author
// reached trust only on scan rows, so every author's moderation history would
// have started empty. 448 of production's 513 items had a matching scan when
// this was written; the rest (report- and forward-only subjects) stay NULL until
// a new signal names their author.
//
// The earliest scan wins, the first-signal rule the live paths apply. Only NULL
// rows are written, so the per-deploy re-run picks up items whose subject has
// been scanned since and never overrides an author a signal already set.
func backfillSubjectAuthor(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE trust_review_item AS i
		   SET subject_author_id = s.author_id
		  FROM (SELECT DISTINCT ON (site, subject_kind, subject_id)
		               site, subject_kind, subject_id, author_id
		          FROM trust_scan_result
		         WHERE author_id IS NOT NULL
		         ORDER BY site, subject_kind, subject_id, id) AS s
		 WHERE i.subject_author_id IS NULL
		   AND i.site = s.site
		   AND i.subject_kind = s.subject_kind
		   AND i.subject_id = s.subject_id`).Error; err != nil {
		return fmt.Errorf("backfill subject_author_id: %w", err)
	}
	return nil
}

// backfillScanAttempts makes the new counter honest about the past. Every row
// that already reached a terminal state was attempted exactly once, so leaving
// them at the DDL default of 0 would claim the pipeline had never touched them.
//
// It also decides what happens to the rows drained during the 2026-07-22..08-07
// truncation fault: they land on 1, so the worker gives them two more attempts
// rather than the full three. That is the correct trade — they are genuinely
// unjudged content that deserves a retry, but they were already tried once and
// the counter should say so. Pending rows are left at 0: they have not been
// attempted, and the default is already the truth for them.
func backfillScanAttempts(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE trust_scan_result
		   SET scan_attempts = 1
		 WHERE scan_attempts = 0 AND status <> ?`, model.ScanStatusPending).Error; err != nil {
		return fmt.Errorf("backfill scan_attempts: %w", err)
	}
	return nil
}

// backfillGatewayFlagged seeds the new gateway_flagged column from flagged for
// rows scored before the column existed. That copy is exactly right TODAY,
// because today flagged IS the gateway's verdict — trust stores the boolean the
// gateway returned and adds nothing of its own.
//
// It stops being right the moment step 07 M0-B lets a site re-derive flagged
// from score against its own threshold. This backfill must therefore be DELETED
// in the same commit that starts re-deriving, not left to run on: from then on
// it would copy trust's own re-derived verdict into the column whose entire job
// is to remember what the gateway said instead.
func backfillGatewayFlagged(db *gorm.DB) error {
	if err := db.Exec(`
		UPDATE trust_scan_result
		   SET gateway_flagged = flagged
		 WHERE gateway_flagged IS NULL AND flagged IS NOT NULL`).Error; err != nil {
		return fmt.Errorf("backfill gateway_flagged: %w", err)
	}
	return nil
}

// compliancePurposeSources are the imported lexicons whose terms exist for LEGAL
// / REGULATORY filtering rather than to catch abuse. They are listed by their
// import note (the source filename cmd/import-trust-terms records) because that
// is the only provenance the rows carry.
//
// The distinction is not editorial fussiness, it is what keeps the precision
// pruner honest: the abuse classifier never judges political speech as abuse, so
// it scores every term here at ~0% precision no matter how well the term works.
// Without this list, one -drop-unevidenced run would silently empty the entire
// compliance lexicon while producing a report that looked fully evidence-based.
//
// The two large mixed lexicons (零时-Tencent / 网易前端过滤敏感词库) are
// deliberately NOT here: they are majority abuse/noise — every measured false
// positive found so far came from them — and marking them compliance wholesale
// would freeze that noise in place permanently. They stay prunable; the pruner's
// review bucket is where anything questionable in them surfaces for a human.
var compliancePurposeSources = []string{
	"GFW补充词库.txt",
	"反动词库.txt",
	"政治类型.txt",
	"贪腐词库.txt",
	"民生词库.txt",
	"新思想启蒙.txt",
	"暴恐词库.txt",
	"涉枪涉爆.txt",
	"COVID-19词库.txt",
}

// classifyImportedTermPurpose backfills purpose=compliance for the lexicons
// above. Idempotent (a re-run rewrites the same rows to the same value) and
// scoped by note, so a term an operator later reclassifies by hand is only
// reverted if it still carries the original import note — which is the correct
// behaviour for a provenance-driven default.
func classifyImportedTermPurpose(db *gorm.DB) error {
	if err := db.Model(&model.TrustTerm{}).
		Where("note IN ? AND purpose <> ?", compliancePurposeSources, model.TermPurposeCompliance).
		Update("purpose", model.TermPurposeCompliance).Error; err != nil {
		return fmt.Errorf("classify imported term purpose: %w", err)
	}
	return nil
}

// preAutoMigrate runs BEFORE AutoMigrate, for the one thing AutoMigrate cannot
// do: add a NOT NULL column to a table that already has rows.
//
// Postgres rejects `ADD COLUMN … NOT NULL` without a default on a populated
// table, and trust_term carries 46k+ rows in production. AutoMigrate emits
// exactly that statement, so without this the migration fails — and because the
// trust service waits on migrate-trust (compose depends_on
// service_completed_successfully), a failed migration means trust never starts.
// A schema addition would have become an outage.
//
// The column is therefore added here WITH a default (so existing rows are
// backfilled) and the default is then DROPPED, which is what keeps 章程 ruling 4
// satisfied: purpose is an intent column whose zero is meaningful, so no DDL
// default may survive to silently supply it on future inserts (the GORM
// zero-value default trap). The DO block makes it a no-op on a fresh database
// where the table does not exist yet.
func preAutoMigrate(db *gorm.DB) error {
	const addPurpose = `
		DO $$
		BEGIN
		    IF to_regclass('trust_term') IS NOT NULL THEN
		        ALTER TABLE trust_term
		            ADD COLUMN IF NOT EXISTS purpose smallint NOT NULL DEFAULT 0;
		        ALTER TABLE trust_term ALTER COLUMN purpose DROP DEFAULT;
		    END IF;
		END $$;`
	if err := db.Exec(addPurpose).Error; err != nil {
		return fmt.Errorf("pre-migrate trust_term.purpose: %w", err)
	}
	return nil
}

// rawSQL is the post-AutoMigrate section: the indexes AutoMigrate cannot
// express (partial predicate / DESC sort). Every statement is idempotent
// (CREATE INDEX IF NOT EXISTS), so this section reruns freely.
func rawSQL(db *gorm.DB) error {
	for _, ix := range []struct{ name, stmt string }{
		// Reason uniqueness split on NULL site (Postgres treats NULLs as
		// distinct, so a plain UNIQUE(key, site) would let duplicate global
		// rows through): one global row per key, and one per (key, site).
		{"uq_trust_report_reason_key_global", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_trust_report_reason_key_global
			    ON trust_report_reason(key) WHERE site IS NULL`},
		{"uq_trust_report_reason_key_site", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_trust_report_reason_key_site
			    ON trust_report_reason(key, site) WHERE site IS NOT NULL`},
		// Invariant 5: one report per (subject, reporter). Partial (P0
		// reporter_id is always present; the predicate is forward-compatible
		// with the P1 anonymous channel where reporter_id may be NULL).
		{"uq_trust_report_subject_reporter", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_trust_report_subject_reporter
			    ON trust_report(site, subject_kind, subject_id, reporter_id)
			    WHERE reporter_id IS NOT NULL`},
		// Rate-limit window count (章程 ruling 6): reports by a reporter in a
		// time window.
		{"idx_trust_report_reporter_created", `
			CREATE INDEX IF NOT EXISTS idx_trust_report_reporter_created
			    ON trust_report(reporter_id, created_at)`},
		// Invariant 4: at most ONE open item per subject. Partial — terminal
		// items (actioned/dismissed) drop out so a fresh item can open later.
		{"uq_trust_review_item_open", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_trust_review_item_open
			    ON trust_review_item(site, subject_kind, subject_id)
			    WHERE status IN (0, 1)`},
		// Invariant 11 (tenant-first): the per-site queue, highest priority
		// first. DESC sort, so it lives here rather than a struct tag.
		{"idx_trust_review_item_site_status_priority", `
			CREATE INDEX IF NOT EXISTS idx_trust_review_item_site_status_priority
			    ON trust_review_item(site, status, priority DESC)`},
		// An author's moderation history: the inbox filtered to one
		// subject_author_id.
		{"idx_trust_review_item_subject_author", `
			CREATE INDEX IF NOT EXISTS idx_trust_review_item_subject_author
			    ON trust_review_item(subject_author_id)`},
		// Dispatch worker claim: pending dispositions whose next_attempt_at is
		// due (章程 ruling 9).
		{"idx_trust_disposition_callback", `
			CREATE INDEX IF NOT EXISTS idx_trust_disposition_callback
			    ON trust_disposition(callback_status, next_attempt_at)`},
		// Scan scoring worker claim (step 03): pending rows, oldest first. The
		// (status, id) shape backs the FOR UPDATE SKIP LOCKED batch pick.
		{"idx_trust_scan_result_status_id", `
			CREATE INDEX IF NOT EXISTS idx_trust_scan_result_status_id
			    ON trust_scan_result(status, id)`},
		// Scan observation / calibration queries (step 03): per-site, per-kind,
		// newest first.
		{"idx_trust_scan_result_site_kind_created", `
			CREATE INDEX IF NOT EXISTS idx_trust_scan_result_site_kind_created
			    ON trust_scan_result(site, subject_kind, created_at)`},
		// Tier0 de-dup (step 05): one ACTIVE term per (site-or-global, norm).
		// COALESCE(site,'') folds the NULL (global) and non-NULL (per-site) cases
		// into one keyspace; the partial predicate lets a deprecated row keep the
		// same norm, so a retired term can be re-created (registry discipline).
		{"uq_trust_term_active", `
			CREATE UNIQUE INDEX IF NOT EXISTS uq_trust_term_active
			    ON trust_term(COALESCE(site, ''), term_norm)
			    WHERE is_deprecated = false`},
	} {
		if err := db.Exec(ix.stmt).Error; err != nil {
			return fmt.Errorf("create index %s: %w", ix.name, err)
		}
	}
	return nil
}

// globalReason is one seed row of the global reason taxonomy (章程 ruling 12).
type globalReason struct {
	Key      string
	NameCN   string
	Severity int16
}

// SeedReasons is the global base taxonomy (site NULL). Exported so tests can
// assert the exact seed set.
var SeedReasons = []globalReason{
	{"abuse", "辱骂/骚扰", 2},
	{"spam", "垃圾信息", 1},
	{"illegal", "违法内容", 3},
	{"rating_mislabel", "分级标注错误", 1},
	{"copyright", "版权侵权", 2},
	{"other", "其他", 1},
}

// seedReasons upserts the six global reasons, keyed on the partial unique
// (key) WHERE site IS NULL. Idempotent: a re-run keeps name/severity in sync
// without duplicating rows. Severity is written explicitly here (no GORM
// default tag on the column), sidestepping the zero-value default trap.
func seedReasons(db *gorm.DB) error {
	for _, r := range SeedReasons {
		if err := db.Exec(`
			INSERT INTO trust_report_reason (key, name_cn, site, severity, is_deprecated)
			VALUES (?, ?, NULL, ?, false)
			ON CONFLICT (key) WHERE site IS NULL
			DO UPDATE SET name_cn = EXCLUDED.name_cn, severity = EXCLUDED.severity
		`, r.Key, r.NameCN, r.Severity).Error; err != nil {
			return fmt.Errorf("seed reason %s: %w", r.Key, err)
		}
	}
	return nil
}
