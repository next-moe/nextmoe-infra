package store

import (
	"testing"
	"time"

	"api/internal/platform/telemetry/otlp"
)

func loadSession(t *testing.T, id string) (started, ended *time.Time, status *string, errors *int, anr bool, day time.Time, version, env string) {
	t.Helper()
	row := struct {
		StartedAt      *time.Time
		EndedAt        *time.Time
		Status         *string
		Errors         *int
		ANR            bool
		Day            time.Time
		ServiceVersion string
		Environment    string
	}{}
	if err := testDB.Raw(`SELECT started_at, ended_at, status, errors, anr, day, service_version, environment
		FROM telemetry_session WHERE session_id = ?`, id).Scan(&row).Error; err != nil {
		t.Fatal(err)
	}
	return row.StartedAt, row.EndedAt, row.Status, row.Errors, row.ANR, row.Day, row.ServiceVersion, row.Environment
}

func TestSessionMergeNaturalOrder(t *testing.T) {
	truncate(t)
	id := sid(1)
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	write(t, 1, rec("session.start", id, "1.0", "direct", start, nil))
	write(t, 1, rec("session.end", id, "1.0", "direct", end, map[string]any{"app.session.status": "exited", "app.session.errors": int64(2)}))
	started, ended, status, errs, anr, day, ver, env := loadSession(t, id)
	if started == nil || !started.Equal(start) {
		t.Errorf("started_at %#v", started)
	}
	if ended == nil || !ended.Equal(end) {
		t.Errorf("ended_at %#v", ended)
	}
	if status == nil || *status != "exited" {
		t.Errorf("status %#v", status)
	}
	if errs == nil || *errs != 2 {
		t.Errorf("errors %#v", errs)
	}
	if anr {
		t.Error("anr")
	}
	if day.Format("2006-01-02") != "2026-09-29" {
		t.Errorf("day %s", day)
	}
	if ver != "1.0" || env != "direct" {
		t.Errorf("ver/env %s %s", ver, env)
	}
}

func TestSessionMergeEndBeforeStart(t *testing.T) {
	truncate(t)
	id := sid(1)
	start := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Minute)
	write(t, 1, rec("session.end", id, "1.0", "direct", end, map[string]any{"app.session.status": "exited", "app.session.errors": int64(2)}))
	write(t, 1, rec("session.start", id, "1.0", "direct", start, nil))
	started, ended, status, errs, _, day, _, _ := loadSession(t, id)
	if started == nil || !started.Equal(start) {
		t.Errorf("started_at %#v", started)
	}
	if ended == nil || !ended.Equal(end) {
		t.Errorf("ended_at %#v", ended)
	}
	if status == nil || *status != "exited" {
		t.Errorf("status %#v", status)
	}
	if errs == nil || *errs != 2 {
		t.Errorf("errors %#v", errs)
	}
	if day.Format("2006-01-02") != "2026-09-29" {
		t.Errorf("day %s want start's day", day)
	}
}

func TestDuplicateSessionEndKeepsFirst(t *testing.T) {
	truncate(t)
	id := sid(1)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("session.start", id, "1.0", "direct", t0, nil),
		rec("session.end", id, "1.0", "direct", t0.Add(time.Minute), map[string]any{"app.session.status": "exited", "app.session.errors": int64(1)}),
	)
	write(t, 1, rec("session.end", id, "1.0", "direct", t0.Add(2*time.Minute), map[string]any{"app.session.status": "crashed", "app.session.errors": int64(9)}))
	_, ended, status, errs, _, _, _, _ := loadSession(t, id)
	if status == nil || *status != "exited" {
		t.Errorf("status %#v", status)
	}
	if errs == nil || *errs != 1 {
		t.Errorf("errors %#v", errs)
	}
	if ended == nil || !ended.Equal(t0.Add(time.Minute)) {
		t.Errorf("ended_at %#v", ended)
	}
}

func TestAnrFlag(t *testing.T) {
	truncate(t)
	id := sid(1)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1, rec("session.start", id, "1.0", "direct", t0, nil))
	write(t, 1, rec("app.crash", id, "1.0", "direct", t0.Add(time.Second), map[string]any{"app.exit.kind": "anr"}))
	_, _, _, _, anr, _, _, _ := loadSession(t, id)
	if !anr {
		t.Fatal("anr not set")
	}
	write(t, 1, rec("exception", id, "1.0", "direct", t0.Add(2*time.Second), nil))
	_, _, _, _, anr, _, _, _ = loadSession(t, id)
	if !anr {
		t.Fatal("anr cleared")
	}
}

func TestProvisionalDayReplaced(t *testing.T) {
	truncate(t)
	id := sid(1)
	crashTime := time.Date(2026, 9, 28, 23, 0, 0, 0, time.UTC)
	startTime := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
	write(t, 1, rec("app.crash", id, "1.0", "direct", crashTime, map[string]any{"app.exit.kind": "java"}))
	_, _, _, _, _, day1, _, _ := loadSession(t, id)
	if day1.Format("2006-01-02") != "2026-09-28" {
		t.Fatalf("provisional day %s", day1)
	}
	write(t, 1, rec("session.start", id, "1.0", "direct", startTime, nil))
	_, _, _, _, _, day2, _, _ := loadSession(t, id)
	if day2.Format("2006-01-02") != "2026-09-29" {
		t.Fatalf("replaced day %s", day2)
	}
}

func TestSessionUpsertOrder(t *testing.T) {
	truncate(t)
	var got []string
	sessionUpsertObserver = func(id string) { got = append(got, id) }
	t.Cleanup(func() { sessionUpsertObserver = nil })
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("session.start", sid(3), "1.0", "direct", t0, nil),
		rec("session.start", sid(1), "1.0", "direct", t0.Add(time.Second), nil),
		rec("session.start", sid(2), "1.0", "direct", t0.Add(2*time.Second), nil),
	)
	want := []string{sid(1), sid(2), sid(3)}
	if len(got) != len(want) {
		t.Fatalf("upserts=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upserts=%v want %v", got, want)
		}
	}
	ids := orderedSessionIDs(foldSessions(receiptNow(), []otlp.Record{
		rec("session.start", sid(3), "1.0", "direct", t0, nil),
		rec("session.start", sid(1), "1.0", "direct", t0, nil),
	}))
	if len(ids) != 2 || ids[0] != sid(1) || ids[1] != sid(3) {
		t.Fatalf("ordered=%v", ids)
	}
}
