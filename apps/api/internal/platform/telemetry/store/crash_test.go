package store

import (
	"context"
	"sync"
	"testing"
	"time"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/symbolicate"
	"api/internal/platform/telemetry/symbols"

	"gorm.io/gorm"
)

func TestCrashRowsQueuedOnWrite(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("session.start", sid(1), "1.0", "direct", ts, nil),
		rec("exception", sid(1), "1.0", "direct", ts.Add(time.Second), map[string]any{"exception.type": "X"}),
		rec("app.crash", sid(1), "1.0", "direct", ts.Add(2*time.Second), map[string]any{"app.exit.kind": "java"}),
		rec("exception", sid(1), "1.0", "direct", ts.Add(3*time.Second), map[string]any{"app.fault": "contract", "exception.type": "Y"}),
		rec("exception", sid(1), "1.0", "direct", ts.Add(4*time.Second), map[string]any{"app.fault": "server", "http.request.method": "GET"}),
		rec("app.crash", sid(1), "1.0", "direct", ts.Add(5*time.Second), map[string]any{"app.exit.kind": "weird"}),
	)
	var rows []struct {
		EventName string
		Kind      string
		Status    string
	}
	if err := testDB.Raw(`SELECT event_name, kind, status FROM telemetry_crash ORDER BY record_uid`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("crashes=%d %+v", len(rows), rows)
	}
	kinds := map[string]int{}
	for _, r := range rows {
		if r.Status != model.CrashPending {
			t.Errorf("status %s", r.Status)
		}
		kinds[r.Kind]++
	}
	if kinds[model.KindException] != 1 || kinds[model.KindJava] != 1 || kinds[model.KindContract] != 1 || kinds[model.KindServer] != 1 || kinds[model.KindNative] != 1 {
		t.Errorf("kinds %+v", kinds)
	}
	var starts int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_crash WHERE event_name = 'session.start'`).Scan(&starts).Error; err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatal("session.start queued")
	}
	r := rec("exception", sid(2), "1.0", "direct", ts, map[string]any{"exception.type": "Z"})
	write(t, 1, r)
	write(t, 1, r)
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_crash`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("after resend crashes=%d", n)
	}
}

func TestClaimSkipLocked(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("exception", sid(1), "1.0", "direct", ts, map[string]any{"exception.type": "A"}),
		rec("exception", sid(2), "1.0", "direct", ts.Add(time.Second), map[string]any{"exception.type": "B"}),
	)
	got := make(chan string, 2)
	var wg sync.WaitGroup
	claim := func() {
		defer wg.Done()
		err := testDB.Transaction(func(tx *gorm.DB) error {
			job, err := ClaimCrash(tx)
			if err != nil {
				return err
			}
			if job == nil {
				got <- ""
				return nil
			}
			got <- job.Crash.RecordUID
			time.Sleep(200 * time.Millisecond)
			return nil
		})
		if err != nil {
			t.Error(err)
		}
	}
	wg.Add(2)
	go claim()
	go claim()
	wg.Wait()
	close(got)
	seen := map[string]struct{}{}
	for uid := range got {
		if uid == "" {
			t.Fatal("missing claim")
		}
		if _, ok := seen[uid]; ok {
			t.Fatalf("duplicate claim %s", uid)
		}
		seen[uid] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("claimed %d", len(seen))
	}
}

func TestNoTimestampsOnCrash(t *testing.T) {
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_name = 'telemetry_crash' AND column_name IN ('created_at','updated_at','processed_at','received_at')`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("timestamp columns=%d", n)
	}
}

func TestCrashRetention(t *testing.T) {
	truncate(t)
	old := time.Date(2026, 8, 29, 0, 0, 0, 0, time.UTC)
	keep := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	if err := testDB.Exec(`INSERT INTO telemetry_crash (
		event_day, record_uid, app_id, service_version, event_name, kind, status, needs, attempts,
		exception_type, message, stack, frames, fingerprint)
		VALUES (?::date, ?, 1, '1.0', 'exception', 'exception', 'pending', '', 0, '', '', '', '[]', ''),
		       (?::date, ?, 1, '1.0', 'exception', 'exception', 'pending', '', 0, '', '', '', '[]', '')`,
		old.Format("2006-01-02"), sid(1), keep.Format("2006-01-02"), sid(2)).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeExpired(context.Background(), receiptNow()); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_crash`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("after purge crashes=%d", n)
	}
}

func finish(t *testing.T, job *ClaimedCrash, res CrashResult) {
	t.Helper()
	err := testDB.Transaction(func(tx *gorm.DB) error {
		return FinishCrash(tx, job, res)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func loadCrash(t *testing.T) ClaimedCrash {
	t.Helper()
	var c model.Crash
	if err := testDB.Order("record_uid").Take(&c).Error; err != nil {
		t.Fatal(err)
	}
	return ClaimedCrash{Crash: c, Attributes: map[string]any{}, Prefixes: nil}
}

func sampleResult(fp, title string) CrashResult {
	return CrashResult{
		Status:        model.CrashDone,
		ExceptionType: "StateError",
		Message:       "boom",
		Stack:         "stack",
		Frames:        []symbolicate.Frame{{Function: "appFn", File: "package:kungal/a.dart"}},
		Fingerprint:   fp,
		Title:         title,
		Culprit:       "appFn (package:kungal/a.dart)",
	}
}

func TestAssignCreatesIssueAndDaily(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1, rec("exception", sid(1), "1.2.0", "direct", ts, map[string]any{"exception.type": "StateError"}))
	job := loadCrash(t)
	finish(t, &job, sampleResult("fp-one", "StateError: boom"))
	var iss model.Issue
	if err := testDB.Take(&iss).Error; err != nil {
		t.Fatal(err)
	}
	if iss.Kind != model.KindException || iss.Title != "StateError: boom" || iss.Status != model.IssueOpen {
		t.Errorf("%+v", iss)
	}
	if iss.FirstVersion != "1.2.0" || iss.LastVersion != "1.2.0" {
		t.Errorf("versions %s %s", iss.FirstVersion, iss.LastVersion)
	}
	var d model.IssueDaily
	if err := testDB.Take(&d).Error; err != nil {
		t.Fatal(err)
	}
	if d.Events != 1 || d.Sessions != 1 || d.ServiceVersion != "1.2.0" {
		t.Errorf("%+v", d)
	}
}

func TestRegroupMovesAndRecounts(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1, rec("exception", sid(1), "1.0", "direct", ts, map[string]any{"exception.type": "A"}))
	job := loadCrash(t)
	finish(t, &job, sampleResult("fp-old", "old"))
	var oldID int64
	if err := testDB.Raw(`SELECT id FROM telemetry_issue`).Scan(&oldID).Error; err != nil {
		t.Fatal(err)
	}
	job = loadCrash(t)
	finish(t, &job, sampleResult("fp-new", "new"))
	var nOld int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_issue WHERE id = ?`, oldID).Scan(&nOld).Error; err != nil {
		t.Fatal(err)
	}
	if nOld != 0 {
		t.Fatal("old issue not deleted")
	}
	var dailies []model.IssueDaily
	if err := testDB.Find(&dailies).Error; err != nil {
		t.Fatal(err)
	}
	if len(dailies) != 1 || dailies[0].Events != 1 || dailies[0].Sessions != 1 {
		t.Fatalf("%+v", dailies)
	}
	var nCrash int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_crash WHERE issue_id = ?`, dailies[0].IssueID).Scan(&nCrash).Error; err != nil {
		t.Fatal(err)
	}
	if nCrash != 1 {
		t.Fatalf("crashes on new issue %d", nCrash)
	}
}

func TestResolvedIssueRegresses(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1, rec("exception", sid(1), "1.0", "direct", ts, map[string]any{"exception.type": "A"}))
	job := loadCrash(t)
	finish(t, &job, sampleResult("fp-r", "t"))
	if _, err := st.UpdateIssue(context.Background(), issueID(t), model.IssueResolved, strPtr("1.0")); err != nil {
		t.Fatal(err)
	}
	write(t, 1, rec("exception", sid(2), "1.1", "direct", ts.Add(time.Second), map[string]any{"exception.type": "A"}))
	var c model.Crash
	if err := testDB.Where("session_id = ?", sid(2)).Take(&c).Error; err != nil {
		t.Fatal(err)
	}
	finish(t, &ClaimedCrash{Crash: c}, sampleResult("fp-r", "t"))
	var iss model.Issue
	if err := testDB.Take(&iss).Error; err != nil {
		t.Fatal(err)
	}
	if iss.Status != model.IssueOpen || !iss.Regressed {
		t.Errorf("status=%s regressed=%v", iss.Status, iss.Regressed)
	}
}

func TestIgnoredStaysIgnored(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, 1, rec("exception", sid(1), "1.0", "direct", ts, map[string]any{"exception.type": "A"}))
	job := loadCrash(t)
	finish(t, &job, sampleResult("fp-i", "t"))
	if _, err := st.UpdateIssue(context.Background(), issueID(t), model.IssueIgnored, nil); err != nil {
		t.Fatal(err)
	}
	write(t, 1, rec("exception", sid(2), "1.0", "direct", ts.Add(time.Second), map[string]any{"exception.type": "A"}))
	var c model.Crash
	if err := testDB.Where("session_id = ?", sid(2)).Take(&c).Error; err != nil {
		t.Fatal(err)
	}
	finish(t, &ClaimedCrash{Crash: c}, sampleResult("fp-i", "t"))
	var iss model.Issue
	if err := testDB.Take(&iss).Error; err != nil {
		t.Fatal(err)
	}
	if iss.Status != model.IssueIgnored || iss.Regressed {
		t.Errorf("status=%s regressed=%v", iss.Status, iss.Regressed)
	}
}

func TestFirstLastVersion(t *testing.T) {
	truncate(t)
	d1 := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	d2 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1, rec("exception", sid(1), "1.0.0", "direct", d1, map[string]any{"exception.type": "A"}))
	job := loadCrash(t)
	finish(t, &job, sampleResult("fp-v", "t"))
	write(t, 1, rec("exception", sid(2), "0.9.0", "direct", d2, map[string]any{"exception.type": "A"}))
	var c model.Crash
	if err := testDB.Where("session_id = ?", sid(2)).Take(&c).Error; err != nil {
		t.Fatal(err)
	}
	finish(t, &ClaimedCrash{Crash: c}, sampleResult("fp-v", "t"))
	var iss model.Issue
	if err := testDB.Take(&iss).Error; err != nil {
		t.Fatal(err)
	}
	if iss.FirstVersion != "1.0.0" || iss.LastVersion != "0.9.0" {
		t.Errorf("first=%s last=%s", iss.FirstVersion, iss.LastVersion)
	}
	if iss.FirstSeenDay.Format("2006-01-02") != "2026-09-20" || iss.LastSeenDay.Format("2006-01-02") != "2026-09-29" {
		t.Errorf("days %s %s", iss.FirstSeenDay, iss.LastSeenDay)
	}
}

func TestRequeueWhenSymbolsArrive(t *testing.T) {
	truncate(t)
	dir := withBlobs(t)
	appID := seedApp(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	write(t, appID,
		rec("exception", sid(1), "1.0.0", "direct", ts, map[string]any{"exception.type": "A"}),
		rec("exception", sid(2), "1.0.0", "direct", ts.Add(time.Second), map[string]any{"exception.type": "B"}),
	)
	if err := testDB.Exec(`UPDATE telemetry_crash SET status = ?, needs = 'dart:aa' WHERE session_id = ?`,
		model.CrashWaitingSymbols, sid(1)).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`UPDATE telemetry_crash SET status = ?, needs = 'dart:missing' WHERE session_id = ?`,
		model.CrashWaitingSymbols, sid(2)).Error; err != nil {
		t.Fatal(err)
	}
	elf := incoming(t, dir, "app.android-arm64.symbols", "dart_symbols", "arm64", "aa", []byte("elf-a"))
	if _, _, err := st.IngestSymbolUpload(context.Background(), appID, "1.0.0", "", []symbols.IncomingFile{elf}); err != nil {
		t.Fatal(err)
	}
	if err := st.RequeueReadyCrashes(context.Background()); err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		SessionID string
		Status    string
	}
	if err := testDB.Raw(`SELECT session_id, status FROM telemetry_crash ORDER BY session_id`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	by := map[string]string{}
	for _, r := range rows {
		by[r.SessionID] = r.Status
	}
	if by[sid(1)] != model.CrashPending {
		t.Errorf("ready status %s", by[sid(1)])
	}
	if by[sid(2)] != model.CrashWaitingSymbols {
		t.Errorf("missing status %s", by[sid(2)])
	}
}

func TestIssueDailyRetention(t *testing.T) {
	truncate(t)
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	keep := receiptNow()
	if err := testDB.Exec(`INSERT INTO telemetry_issue (
		app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
		first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at)
		VALUES (1,'fp','exception','t','c','open','', false, ?::date, ?::date, '1','1', now(), now())`,
		keep.Format("2006-01-02"), keep.Format("2006-01-02")).Error; err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := testDB.Raw(`SELECT id FROM telemetry_issue`).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`INSERT INTO telemetry_issue_daily (issue_id, day, service_version, events, sessions)
		VALUES (?, ?::date, '1.0', 1, 1), (?, ?::date, '1.0', 1, 1)`,
		id, old.Format("2006-01-02"), id, keep.Format("2006-01-02")).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeExpired(context.Background(), receiptNow()); err != nil {
		t.Fatal(err)
	}
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_issue_daily`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("daily=%d", n)
	}
}

func issueID(t *testing.T) int64 {
	t.Helper()
	var id int64
	if err := testDB.Raw(`SELECT id FROM telemetry_issue`).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func strPtr(s string) *string { return &s }
