package alert

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	suitelock "api/internal/platform/telemetry/dbtest"
	"api/internal/platform/telemetry/migrate"
	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/store"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	testDB *gorm.DB
	st     *store.Store
)

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		os.Exit(m.Run())
		return
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("telemetry/alert", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)
	if err := migrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("telemetry/alert", "telemetry migration failed: %v", err)
	}
	testDB = db
	st = store.New(db)
	if err := st.EnsurePartitions(context.Background(), evalNow()); err != nil {
		release()
		dbtest.SkipMainf("telemetry/alert", "ensure partitions: %v", err)
	}
	code := m.Run()
	release()
	os.Exit(code)
}

func requireDB(t *testing.T) {
	t.Helper()
	if st == nil {
		dbtest.Skip(t)
	}
}

func evalNow() time.Time {
	return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
}

func truncateAlert(t *testing.T) {
	t.Helper()
	if err := testDB.Exec(
		`TRUNCATE telemetry_alert, telemetry_alert_channel, telemetry_issue_daily, telemetry_crash, telemetry_issue, telemetry_symbol_file, telemetry_symbol_upload, telemetry_engine_symbol, telemetry_blob, telemetry_event, telemetry_session, telemetry_daily_metric, telemetry_app RESTART IDENTITY CASCADE`,
	).Error; err != nil {
		t.Fatal(err)
	}
}

func insertApp(t *testing.T, name string, enabled bool) int64 {
	t.Helper()
	if err := testDB.Exec(
		`INSERT INTO telemetry_app (service_name, display_name, ingest_key, enabled, in_app_prefixes, created_at, updated_at)
		VALUES (?, 'n', ?, ?, '[]', now(), now())`,
		name, name+"key01234567890123456789012", enabled,
	).Error; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := testDB.Raw(`SELECT id FROM telemetry_app WHERE service_name = ?`, name).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func insertMetric(t *testing.T, appID int64, version, env, day string, sessions, crashed, anr int) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO telemetry_daily_metric (
		app_id, service_version, environment, day, sessions, crashed_sessions, anr_sessions,
		unhandled_sessions, abnormal_sessions, exceptions, crash_java, crash_native, crash_anr,
		startup_count, jank_frames_over, jank_frames_total, updated_at)
		VALUES (?, ?, ?, ?::date, ?, ?, ?, 0, 0, 0, 0, 0, 0, 0, 0, 0, now())`,
		appID, version, env, day, sessions, crashed, anr).Error; err != nil {
		t.Fatal(err)
	}
}

func insertIssue(t *testing.T, appID int64, fp, kind, title, status string, regressed bool, created, lastSeen, firstVer, lastVer string) int64 {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO telemetry_issue (
		app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
		first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at)
		VALUES (?, ?, ?, ?, 'culprit', ?, '', ?, ?::date, ?::date, ?, ?, ?::timestamptz, now())`,
		appID, fp, kind, title, status, regressed, lastSeen, lastSeen, firstVer, lastVer, created).Error; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := testDB.Raw(`SELECT id FROM telemetry_issue WHERE fingerprint = ?`, fp).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func insertEvent(t *testing.T, appID int64, uid, version, env, name, day string, at time.Time, attrs string) {
	t.Helper()
	if attrs == "" {
		attrs = "{}"
	}
	if err := testDB.Exec(`INSERT INTO telemetry_event (
		record_uid, event_day, app_id, service_version, environment,
		event_name, severity, event_time, os_name, os_version, device_model,
		device_manufacturer, host_arch, sdk_version, attributes)
		VALUES (?, ?::date, ?, ?, ?, ?, 17, ?, '', '', '', '', '', '', ?::jsonb)`,
		uid, day, appID, version, env, name, at, attrs).Error; err != nil {
		t.Fatal(err)
	}
}

func insertCrash(t *testing.T, appID int64, uid, version, day, status, needs, env string) {
	t.Helper()
	if err := testDB.Exec(`INSERT INTO telemetry_crash (
		event_day, record_uid, app_id, service_version, event_name, kind, status, needs, attempts,
		exception_type, message, stack, frames, fingerprint)
		VALUES (?::date, ?, ?, ?, 'exception', 'exception', ?, ?, 0, '', '', '', '[]', '')`,
		day, uid, appID, version, status, needs).Error; err != nil {
		t.Fatal(err)
	}
	insertEvent(t, appID, uid, version, env, "exception", day, evalNow().Add(-25*time.Hour), "{}")
}

func evaluate(t *testing.T) {
	t.Helper()
	if err := NewEvaluator(st).Evaluate(context.Background(), evalNow()); err != nil {
		t.Fatal(err)
	}
}

func alertsOf(t *testing.T, rule string) []model.Alert {
	t.Helper()
	var rows []model.Alert
	if err := testDB.Where("rule = ?", rule).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestRuleContractNew(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	now := evalNow()
	fireID := insertIssue(t, appID, "fp-new", model.KindContract, "shape", model.IssueOpen, false,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	insertIssue(t, appID, "fp-old", model.KindContract, "old", model.IssueOpen, false,
		now.Add(-8*24*time.Hour).Format(time.RFC3339), "2026-09-21", "1.0", "1.0")
	insertIssue(t, appID, "fp-ex", model.KindException, "ex", model.IssueOpen, false,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	evaluate(t)
	got := alertsOf(t, model.RuleContractNew)
	if len(got) != 1 || got[0].SubjectKey != fmt.Sprintf("issue:%d", fireID) {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleIssueNew(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	now := evalNow()
	fire := insertIssue(t, appID, "fp-java", model.KindJava, "J", model.IssueOpen, false,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	insertIssue(t, appID, "fp-old", model.KindJava, "old", model.IssueOpen, false,
		now.Add(-8*24*time.Hour).Format(time.RFC3339), "2026-09-21", "1.0", "1.0")
	insertIssue(t, appID, "fp-server", model.KindServer, "s", model.IssueOpen, false,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	evaluate(t)
	got := alertsOf(t, model.RuleIssueNew)
	if len(got) != 1 || got[0].SubjectKey != fmt.Sprintf("issue:%d", fire) {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleIssueRegressed(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	now := evalNow()
	fire := insertIssue(t, appID, "fp-reg", model.KindException, "R", model.IssueOpen, true,
		now.Add(-48*time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.2")
	insertIssue(t, appID, "fp-open", model.KindException, "O", model.IssueOpen, false,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	insertIssue(t, appID, "fp-res", model.KindException, "X", model.IssueResolved, true,
		now.Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	evaluate(t)
	got := alertsOf(t, model.RuleIssueRegressed)
	if len(got) != 1 || got[0].SubjectKey != fmt.Sprintf("issue:%d:2026-09-29", fire) {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleCrashRate(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	insertMetric(t, appID, "low-n", "direct", "2026-09-29", 50, 20, 0)
	insertMetric(t, appID, "below", "direct", "2026-09-29", 200, 2, 0)
	insertMetric(t, appID, "above", "direct", "2026-09-29", 200, 3, 0)
	evaluate(t)
	got := alertsOf(t, model.RuleCrashRate)
	if len(got) != 1 || got[0].SubjectKey != "crash_rate:above:2026-09-29" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleAnrRate(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	insertMetric(t, appID, "low-n", "direct", "2026-09-29", 50, 0, 20)
	insertMetric(t, appID, "below", "direct", "2026-09-29", 2000, 0, 9)
	insertMetric(t, appID, "above", "direct", "2026-09-29", 2000, 0, 10)
	evaluate(t)
	got := alertsOf(t, model.RuleAnrRate)
	if len(got) != 1 || got[0].SubjectKey != "anr_rate:above:2026-09-29" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleCrashRegression(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	pick := insertApp(t, "pick-app", true)
	insertMetric(t, pick, "0.9", "direct", "2026-09-09", 1, 0, 0)
	insertMetric(t, pick, "0.9", "direct", "2026-09-20", 200, 2, 0)
	insertMetric(t, pick, "1.0", "direct", "2026-09-24", 1, 0, 0)
	insertMetric(t, pick, "1.0", "direct", "2026-09-20", 200, 4, 0)
	insertMetric(t, pick, "2.0", "direct", "2026-09-28", 200, 10, 0)

	lowP := insertApp(t, "low-p", true)
	insertMetric(t, lowP, "p", "direct", "2026-09-10", 1, 0, 0)
	insertMetric(t, lowP, "p", "direct", "2026-09-20", 50, 25, 0)
	insertMetric(t, lowP, "v", "direct", "2026-09-28", 200, 100, 0)

	bound := insertApp(t, "bound-app", true)
	insertMetric(t, bound, "p", "direct", "2026-09-10", 1, 0, 0)
	insertMetric(t, bound, "p", "direct", "2026-09-20", 200, 10, 0)
	insertMetric(t, bound, "eq", "direct", "2026-09-28", 200, 20, 0)
	insertMetric(t, bound, "over", "direct", "2026-09-28", 200, 21, 0)

	evaluate(t)
	got := alertsOf(t, model.RuleCrashRegression)
	byKey := map[string]model.Alert{}
	for _, a := range got {
		byKey[a.SubjectKey] = a
	}
	sel, ok := byKey["crash_regression:2.0:2026-09-29"]
	if !ok {
		t.Fatalf("missing V=2.0: %+v", got)
	}
	var facts map[string]any
	if err := json.Unmarshal(sel.Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(facts["previous_version"]) != "1.0" {
		t.Fatalf("P=%v want 1.0 facts=%v", facts["previous_version"], facts)
	}
	if _, ok := byKey["crash_regression:v:2026-09-29"]; ok {
		t.Fatal("P below min_sessions should not fire")
	}
	if _, ok := byKey["crash_regression:eq:2026-09-29"]; ok {
		t.Fatal("equal to max(factor,delta) should not fire")
	}
	if _, ok := byKey["crash_regression:over:2026-09-29"]; !ok {
		t.Fatalf("over boundary missing: %+v", got)
	}
}

func TestRuleServerFaults(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	now := evalNow()
	day := "2026-09-29"
	for i := 0; i < 51; i++ {
		path := fmt.Sprintf("/p/%d", i%6)
		uid := fmt.Sprintf("%032d", i+1)
		attrs := fmt.Sprintf(`{"app.fault":"server","http.request.method":"GET","url.path":"%s","http.response.status_code":500}`, path)
		insertEvent(t, appID, uid, "1.0", "direct", "exception", day, now.Add(-10*time.Minute), attrs)
	}
	insertEvent(t, appID, fmt.Sprintf("%032d", 99), "1.0", "direct", "exception", day, now.Add(-2*time.Hour),
		`{"app.fault":"server","http.request.method":"GET","url.path":"/old","http.response.status_code":500}`)
	other := insertApp(t, "quiet-app", true)
	for i := 0; i < 50; i++ {
		uid := fmt.Sprintf("b%031d", i)
		insertEvent(t, other, uid, "1.0", "direct", "exception", day, now.Add(-10*time.Minute),
			`{"app.fault":"server","http.request.method":"GET","url.path":"/x","http.response.status_code":500}`)
	}
	evaluate(t)
	got := alertsOf(t, model.RuleServerFaults)
	if len(got) != 1 || got[0].AppID != appID {
		t.Fatalf("got=%+v", got)
	}
	var facts struct {
		Count int `json:"count"`
		Top   []struct {
			Path  string `json:"path"`
			Count int    `json:"count"`
		} `json:"top"`
	}
	if err := json.Unmarshal(got[0].Facts, &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Count != 51 {
		t.Fatalf("count=%d", facts.Count)
	}
	if len(facts.Top) != 5 {
		t.Fatalf("top=%d %+v", len(facts.Top), facts.Top)
	}
}

func TestRuleSymbolsMissing(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	for i := 0; i < 5; i++ {
		uid := fmt.Sprintf("d%031d", i)
		insertCrash(t, appID, uid, "1.0.0", "2026-09-28", model.CrashWaitingSymbols, "dart:aa", "direct")
	}
	insertCrash(t, appID, fmt.Sprintf("e%031d", 1), "1.0.0", "2026-09-28", model.CrashWaitingSymbols, "dart:aa", "dev")
	insertCrash(t, appID, fmt.Sprintf("t%031d", 1), "1.0.0", "2026-09-29", model.CrashWaitingSymbols, "dart:aa", "direct")
	for i := 0; i < 4; i++ {
		uid := fmt.Sprintf("n%031d", i)
		insertCrash(t, appID, uid, "2.0.0", "2026-09-28", model.CrashWaitingSymbols, "dart:bb", "direct")
	}
	evaluate(t)
	got := alertsOf(t, model.RuleSymbolsMissing)
	if len(got) != 1 || got[0].SubjectKey != "symbols_missing:1.0.0:dart:aa" {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleSilentApp(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	fire := insertApp(t, "silent-fire", true)
	quiet := insertApp(t, "silent-low", true)
	awake := insertApp(t, "silent-awake", true)
	for i := 1; i <= 7; i++ {
		day := evalNow().AddDate(0, 0, -i).Format("2006-01-02")
		insertMetric(t, fire, "1.0", "direct", day, 50, 0, 0)
		insertMetric(t, quiet, "1.0", "direct", day, 49, 0, 0)
		insertMetric(t, awake, "1.0", "direct", day, 50, 0, 0)
	}
	insertEvent(t, awake, fmt.Sprintf("%032d", 1), "1.0", "direct", "app.lifecycle", "2026-09-29",
		evalNow().Add(-time.Hour), "{}")
	evaluate(t)
	got := alertsOf(t, model.RuleSilentApp)
	if len(got) != 1 || got[0].AppID != fire {
		t.Fatalf("got=%+v", got)
	}
}

func TestRuleEngineFetchFailed(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	if err := testDB.Exec(`INSERT INTO telemetry_engine_symbol
		(engine_revision, variant, status, build_id, sha256, size, attempts, next_attempt_at, last_error, updated_at)
		VALUES ('rev-fail', 'android-arm64-release', 'failed', '', '', 0, 1, now(), 'not found', now()),
		       ('rev-ok', 'android-arm64-release', 'ready', '', '', 0, 0, now(), '', now())`).Error; err != nil {
		t.Fatal(err)
	}
	evaluate(t)
	got := alertsOf(t, model.RuleEngineFetchFailed)
	if len(got) != 1 || got[0].SubjectKey != "engine:rev-fail:android-arm64-release" {
		t.Fatalf("got=%+v", got)
	}
	if got[0].AppID != 0 {
		t.Fatalf("app_id=%d", got[0].AppID)
	}
}

func TestDedup(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "kungal-app", true)
	insertIssue(t, appID, "fp-c", model.KindContract, "t", model.IssueOpen, false,
		evalNow().Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	evaluate(t)
	evaluate(t)
	got := alertsOf(t, model.RuleContractNew)
	if len(got) != 1 {
		t.Fatalf("dedup n=%d", len(got))
	}
}

func TestDisabledAppSkipped(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	appID := insertApp(t, "off-app", false)
	insertIssue(t, appID, "fp-c", model.KindContract, "t", model.IssueOpen, false,
		evalNow().Add(-time.Hour).Format(time.RFC3339), "2026-09-29", "1.0", "1.0")
	insertMetric(t, appID, "1.0", "direct", "2026-09-29", 200, 50, 0)
	evaluate(t)
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_alert`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("alerts=%d", n)
	}
}

func TestAlertRetention(t *testing.T) {
	requireDB(t)
	truncateAlert(t)
	now := evalNow()
	if err := testDB.Exec(`INSERT INTO telemetry_alert
		(app_id, rule, subject_key, urgency, title, facts, status, attempts, last_error, created_at)
		VALUES (1, 'crash_rate', 'old', 'immediate', 't', '{}', 'sent', 0, '', ?),
		       (1, 'crash_rate', 'new', 'immediate', 't', '{}', 'sent', 0, '', ?)`,
		now.AddDate(0, 0, -401), now.AddDate(0, 0, -399)).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeExpired(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	var keys []string
	if err := testDB.Raw(`SELECT subject_key FROM telemetry_alert ORDER BY subject_key`).Scan(&keys).Error; err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "new" {
		t.Fatalf("keys=%v", keys)
	}
}
