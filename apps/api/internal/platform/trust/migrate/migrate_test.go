package migrate

import (
	"fmt"
	"os"
	"strings"
	"testing"

	suitelock "api/internal/platform/trust/dbtest"
	"api/internal/platform/trust/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Integration test against a real Postgres (the community/catalog convention):
// a missing database skips the whole package.
// Schema comes from migrate.Run — the exact production migration — so
// these probes assert the storage-level invariants of doc 18 §3 directly
// against the shipped schema. `go test -p 1` (pinned in test.yml) serializes
// packages that share the CI database.

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("trust/migrate")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("trust/migrate", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	// First migration provisions the schema; running it again is the
	// idempotency probe (E1) — a second AutoMigrate + IF NOT EXISTS raw
	// section + seed upsert must be a no-op.
	if err := Run(db); err != nil {
		release()
		dbtest.SkipMainf("trust/migrate", "trust migration failed: %v", err)
	}
	if err := Run(db); err != nil {
		release()
		fmt.Fprintf(os.Stderr, "trust migration is NOT idempotent: %v\n", err)
		os.Exit(1)
	}

	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

// TestSeedIdempotent asserts the six global reasons exist exactly once each
// after two migrations (E1).
func TestSeedIdempotent(t *testing.T) {
	var count int64
	if err := testDB.Raw(`SELECT count(*) FROM trust_report_reason WHERE site IS NULL`).Scan(&count).Error; err != nil {
		t.Fatalf("count reasons: %v", err)
	}
	if count != int64(len(SeedReasons)) {
		t.Fatalf("global reason count = %d, want %d (seed not idempotent?)", count, len(SeedReasons))
	}
	for _, r := range SeedReasons {
		var sev int16
		if err := testDB.Raw(`SELECT severity FROM trust_report_reason WHERE key = ? AND site IS NULL`, r.Key).Scan(&sev).Error; err != nil {
			t.Fatalf("read reason %s: %v", r.Key, err)
		}
		if sev != r.Severity {
			t.Errorf("reason %s severity = %d, want %d", r.Key, sev, r.Severity)
		}
	}
}

// TestPartialUniqueIndexes asserts the invariant-bearing partial uniques carry
// both their columns and their predicates verbatim (invariants 4/5).
func TestPartialUniqueIndexes(t *testing.T) {
	cases := []struct {
		name  string
		frags []string
	}{
		{"uq_trust_review_item_open", []string{"(site, subject_kind, subject_id)", "status = ANY", "0, 1"}},
		{"uq_trust_report_subject_reporter", []string{"(site, subject_kind, subject_id, reporter_id)", "reporter_id IS NOT NULL"}},
		{"uq_trust_report_reason_key_global", []string{"(key)", "site IS NULL"}},
		{"uq_trust_report_reason_key_site", []string{"(key, site)", "site IS NOT NULL"}},
		{"idx_trust_review_item_site_status_priority", []string{"(site, status, priority DESC)"}},
		// Scan worker claim + observation indexes (step 03).
		{"idx_trust_scan_result_status_id", []string{"(status, id)"}},
		{"idx_trust_scan_result_site_kind_created", []string{"(site, subject_kind, created_at)"}},
	}
	for _, c := range cases {
		def := indexDef(t, c.name)
		for _, frag := range c.frags {
			if !strings.Contains(def, frag) {
				t.Errorf("index %s missing %q in\n  %s", c.name, frag, def)
			}
		}
	}
}

func indexDef(t *testing.T, name string) string {
	t.Helper()
	var def string
	if err := testDB.Raw(
		`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = ?`, name,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read indexdef %s: %v", name, err)
	}
	if def == "" {
		t.Fatalf("index %s does not exist", name)
	}
	return def
}

// TestColumnDiscipline spot-checks that intent columns carry NO DB default (the
// GORM zero-value trap, 章程 ruling 4) while bookkeeping columns keep theirs.
func TestColumnDiscipline(t *testing.T) {
	noDefault := []struct{ table, column string }{
		{"trust_report", "weight"}, {"trust_report", "status"},
		{"trust_review_item", "priority"}, {"trust_review_item", "source"}, {"trust_review_item", "status"},
		{"trust_report_reason", "severity"}, {"trust_disposition", "action"},
		// Scan intent columns (step 03): status/mode zeros are meaningful.
		{"trust_scan_result", "status"}, {"trust_scan_result", "mode"},
		{"trust_scan_result", "site"}, {"trust_scan_result", "content_text"},
		// degraded_reason is nullable with no default on purpose: NULL means "not
		// recorded", and a default would hand every pre-existing drained row a
		// reason nobody ever observed.
		{"trust_scan_result", "degraded_reason"},
	}
	for _, c := range noDefault {
		if def := columnDefault(t, c.table, c.column); def != "" {
			t.Errorf("%s.%s must have NO default (intent column), got %q", c.table, c.column, def)
		}
	}
	withDefault := []struct{ table, column, want string }{
		{"trust_disposition", "callback_attempts", "0"},
		{"trust_report_reason", "is_deprecated", "false"},
		// Scan bookkeeping columns (step 03): channel '' and created_at now().
		{"trust_scan_result", "channel", "''"},
		{"trust_scan_result", "created_at", "now()"},
		// scan_attempts is a counter, not an intent: 0 IS the initial truth, so the
		// DDL default is what lets the column be added to a populated table without
		// the NOT NULL failure that took trust_term.purpose down.
		{"trust_scan_result", "scan_attempts", "0"},
	}
	for _, c := range withDefault {
		if def := columnDefault(t, c.table, c.column); !strings.Contains(def, c.want) {
			t.Errorf("%s.%s default = %q, want to contain %q", c.table, c.column, def, c.want)
		}
	}
}

func columnDefault(t *testing.T, table, column string) string {
	t.Helper()
	var def *string
	if err := testDB.Raw(
		`SELECT column_default FROM information_schema.columns
		   WHERE table_schema='public' AND table_name=? AND column_name=?`,
		table, column,
	).Scan(&def).Error; err != nil {
		t.Fatalf("read default %s.%s: %v", table, column, err)
	}
	if def == nil {
		return ""
	}
	return *def
}

// TestClassifyImportedTermPurpose pins the backfill that protects the
// compliance lexicon from the precision pruner. Getting this wrong is not a
// cosmetic bug: an unclassified compliance term is judged by an abuse
// classifier that never asks its question, scores ~0% precision forever, and is
// retired by the first pruning run that includes it.
func TestClassifyImportedTermPurpose(t *testing.T) {
	db := testDB
	if err := db.Exec("TRUNCATE trust_term RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}

	note := func(s string) *string { return &s }
	seed := []model.TrustTerm{
		{TermNorm: "political-a", Kind: model.TermKindSuspect, Note: note("反动词库.txt")},
		{TermNorm: "political-b", Kind: model.TermKindSuspect, Note: note("GFW补充词库.txt")},
		{TermNorm: "spam-a", Kind: model.TermKindSuspect, Note: note("广告类型.txt")},
		{TermNorm: "mixed-a", Kind: model.TermKindSuspect, Note: note("零时-Tencent.txt")},
		{TermNorm: "handmade", Kind: model.TermKindSuspect},
	}
	for i := range seed {
		if err := db.Create(&seed[i]).Error; err != nil {
			t.Fatalf("seed %s: %v", seed[i].TermNorm, err)
		}
	}

	// Re-running the migration is what performs the backfill; it must also be
	// idempotent, since it runs on every single deploy.
	for pass := 1; pass <= 2; pass++ {
		if err := Run(db); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		for _, tc := range []struct {
			term string
			want int16
		}{
			{"political-a", model.TermPurposeCompliance},
			{"political-b", model.TermPurposeCompliance},
			// Ad/spam lexicons ARE abuse — they must stay prunable.
			{"spam-a", model.TermPurposeAbuse},
			// The big mixed lexicons stay prunable on purpose: every measured
			// false positive so far came from them, and freezing them as
			// compliance would make that noise permanent.
			{"mixed-a", model.TermPurposeAbuse},
			// A hand-made term has no import provenance to classify by.
			{"handmade", model.TermPurposeAbuse},
		} {
			var got model.TrustTerm
			if err := db.Where("term_norm = ?", tc.term).Take(&got).Error; err != nil {
				t.Fatalf("pass %d: reload %s: %v", pass, tc.term, err)
			}
			if got.Purpose != tc.want {
				t.Errorf("pass %d: %s purpose = %d, want %d", pass, tc.term, got.Purpose, tc.want)
			}
		}
	}
}

// TestPurposeColumnOnPopulatedTable reproduces the PRODUCTION upgrade path: an
// existing trust_term with rows but no purpose column, then this migration on
// top of it. AutoMigrate emits `ADD COLUMN … NOT NULL` with no default, which
// Postgres refuses on a populated table — and since the trust service waits on
// migrate-trust (compose depends_on service_completed_successfully), that
// failure would keep trust from starting at all. A column addition would have
// been an outage, so the populated case gets its own test; running the
// migration only against the empty database it provisions proves nothing here.
func TestPurposeColumnOnPopulatedTable(t *testing.T) {
	db := testDB
	if err := db.Exec("ALTER TABLE trust_term DROP COLUMN IF EXISTS purpose").Error; err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if err := db.Exec("TRUNCATE trust_term RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO trust_term (term_norm, kind, note, is_deprecated)
		 VALUES ('pre-a', 0, '反动词库.txt', false), ('pre-b', 0, NULL, false)`).Error; err != nil {
		t.Fatalf("seed pre-existing rows: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("migration failed on a populated table: %v", err)
	}

	// Pre-existing rows land on the meaningful zero, and the provenance backfill
	// still reclassifies the compliance one.
	for _, tc := range []struct {
		term string
		want int16
	}{
		{"pre-a", model.TermPurposeCompliance},
		{"pre-b", model.TermPurposeAbuse},
	} {
		// Read the scalar rather than the model: this test drops and re-adds a
		// column, and a SELECT * through the prepared-statement cache trips
		// "cached plan must not change result type" — an artifact of the DDL
		// churn here, not of the migration (production never drops the column).
		var got int16
		if err := db.Raw("SELECT purpose FROM trust_term WHERE term_norm = ?", tc.term).
			Scan(&got).Error; err != nil {
			t.Fatalf("reload %s: %v", tc.term, err)
		}
		if got != tc.want {
			t.Errorf("%s purpose = %d, want %d", tc.term, got, tc.want)
		}
	}

	// The DDL default must NOT survive: purpose is an intent column whose zero is
	// meaningful (章程 ruling 4), and a lingering default is exactly the GORM
	// zero-value trap — it would silently supply 0 for a caller that meant to
	// write compliance and passed the field through a partial update.
	var colDefault *string
	if err := db.Raw(`SELECT column_default FROM information_schema.columns
	                   WHERE table_name = 'trust_term' AND column_name = 'purpose'`).
		Scan(&colDefault).Error; err != nil {
		t.Fatalf("read column_default: %v", err)
	}
	if colDefault != nil {
		t.Fatalf("purpose kept a DDL default (%q); the zero-value trap is open", *colDefault)
	}
}

// TestScanAttemptsBackfillOnPopulatedTable pins both halves of adding the retry
// counter to a table that already holds production rows.
//
// The first half is that it can be added at all: trust_term.purpose was added
// NOT NULL with no default and failed 23502 on 46k rows, which — because trust
// will not start until migrate-trust exits 0 — is an outage, not a bad migration.
// scan_attempts carries a DDL default for exactly that reason.
//
// The second is that the counter starts out honest. Rows that already reached a
// terminal state were attempted once, so leaving them at 0 would both misreport
// history and hand the rows drained during the truncation fault a full three
// retries instead of the two they have left.
func TestScanAttemptsBackfillOnPopulatedTable(t *testing.T) {
	db := testDB
	if err := db.Exec("ALTER TABLE trust_scan_result DROP COLUMN IF EXISTS scan_attempts").Error; err != nil {
		t.Fatalf("drop column: %v", err)
	}
	if err := db.Exec("TRUNCATE trust_scan_result RESTART IDENTITY CASCADE").Error; err != nil {
		t.Fatalf("truncate: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO trust_scan_result (site, subject_kind, subject_id, content_text, status, mode)
		 VALUES ('kungal','post','p-scored','t',1,0),
		        ('kungal','post','p-degraded','t',2,0),
		        ('kungal','post','p-pending','t',0,0)`).Error; err != nil {
		t.Fatalf("seed pre-existing rows: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("migration failed on a populated table: %v", err)
	}

	for _, tc := range []struct {
		subject string
		want    int16
		why     string
	}{
		{"p-scored", 1, "a scored row was attempted once"},
		{"p-degraded", 1, "a drained row was attempted once and keeps two retries"},
		{"p-pending", 0, "an unattempted row must stay at zero"},
	} {
		// Scalar read, not the model: this test drops and re-adds a column, and a
		// SELECT * through the prepared-statement cache trips "cached plan must not
		// change result type" (production never drops the column).
		var got int16
		if err := db.Raw("SELECT scan_attempts FROM trust_scan_result WHERE subject_id = ?", tc.subject).
			Scan(&got).Error; err != nil {
			t.Fatalf("reload %s: %v", tc.subject, err)
		}
		if got != tc.want {
			t.Errorf("%s scan_attempts = %d, want %d — %s", tc.subject, got, tc.want, tc.why)
		}
	}
}

// TestSubjectAuthorBackfillOnPopulatedTable replays the production upgrade: review
// items that predate subject_author_id, with the author known only from their
// subject's scan rows.
func TestSubjectAuthorBackfillOnPopulatedTable(t *testing.T) {
	db := testDB
	if err := db.Exec("ALTER TABLE trust_review_item DROP COLUMN IF EXISTS subject_author_id").Error; err != nil {
		t.Fatalf("drop column: %v", err)
	}
	for _, table := range []string{"trust_review_item", "trust_scan_result"} {
		if err := db.Exec("TRUNCATE " + table + " RESTART IDENTITY CASCADE").Error; err != nil {
			t.Fatalf("truncate %s: %v", table, err)
		}
	}
	if err := db.Exec(
		`INSERT INTO trust_review_item (site, subject_kind, subject_id, source, priority, status, created_at)
		 VALUES ('kungal','forum_topic','scanned',1,1,3,now()),
		        ('kungal','forum_topic','late-author',0,1,2,now()),
		        ('kungal','forum_topic','unscanned',0,1,0,now()),
		        ('kungal','forum_topic','other-site',0,1,0,now())`).Error; err != nil {
		t.Fatalf("seed items: %v", err)
	}
	if err := db.Exec(
		`INSERT INTO trust_scan_result (site, subject_kind, subject_id, author_id, content_text, status, mode)
		 VALUES ('kungal','forum_topic','scanned',101,'t',1,0),
		        ('kungal','forum_topic','scanned',102,'t',1,0),
		        ('kungal','forum_topic','late-author',NULL,'t',1,0),
		        ('kungal','forum_topic','late-author',201,'t',1,0),
		        ('moyu','forum_topic','other-site',301,'t',1,0)`).Error; err != nil {
		t.Fatalf("seed scans: %v", err)
	}

	if err := Run(db); err != nil {
		t.Fatalf("migration failed on a populated table: %v", err)
	}

	author := func(subject string) *int64 {
		t.Helper()
		var got *int64
		if err := db.Raw("SELECT subject_author_id FROM trust_review_item WHERE subject_id = ?", subject).
			Scan(&got).Error; err != nil {
			t.Fatalf("reload %s: %v", subject, err)
		}
		return got
	}
	for _, tc := range []struct {
		subject string
		want    int64
		why     string
	}{
		{"scanned", 101, "the earliest scan names the author"},
		{"late-author", 201, "a scan without an author is skipped, not adopted as NULL"},
		{"unscanned", 0, "no scan, no author"},
		{"other-site", 0, "another site's scan of the same id is a different subject"},
	} {
		got := author(tc.subject)
		switch {
		case tc.want == 0 && got != nil:
			t.Errorf("%s subject_author_id = %d, want NULL — %s", tc.subject, *got, tc.why)
		case tc.want != 0 && (got == nil || *got != tc.want):
			t.Errorf("%s subject_author_id = %v, want %d — %s", tc.subject, got, tc.want, tc.why)
		}
	}

	if err := db.Exec("UPDATE trust_review_item SET subject_author_id = 999 WHERE subject_id = 'scanned'").Error; err != nil {
		t.Fatalf("set author: %v", err)
	}
	if err := Run(db); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if got := author("scanned"); got == nil || *got != 999 {
		t.Errorf("a re-run overwrote an author already set: got %v, want 999", got)
	}
}
