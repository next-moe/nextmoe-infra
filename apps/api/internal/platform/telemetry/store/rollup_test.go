package store

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/telemetry/model"
)

func metric(t *testing.T, version, env string) model.DailyMetric {
	t.Helper()
	var row model.DailyMetric
	if err := testDB.Where("service_version = ? AND environment = ?", version, env).First(&row).Error; err != nil {
		t.Fatalf("metric %s/%s: %v", version, env, err)
	}
	return row
}

func TestRollupSessionRates(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("session.start", sid(1), "1.0", "direct", t0, nil),
		rec("session.end", sid(1), "1.0", "direct", t0.Add(time.Minute), map[string]any{"app.session.status": "crashed"}),
		rec("session.start", sid(2), "1.0", "direct", t0, nil),
		rec("session.end", sid(2), "1.0", "direct", t0.Add(time.Minute), map[string]any{"app.session.status": "unhandled"}),
		rec("session.start", sid(3), "1.0", "direct", t0, nil),
		rec("session.end", sid(3), "1.0", "direct", t0.Add(time.Minute), map[string]any{"app.session.status": "abnormal"}),
		rec("session.start", sid(4), "1.0", "direct", t0, nil),
		rec("app.crash", sid(4), "1.0", "direct", t0.Add(time.Second), map[string]any{"app.exit.kind": "anr"}),
		rec("session.end", sid(4), "1.0", "direct", t0.Add(time.Minute), map[string]any{"app.session.status": "exited"}),
	)
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	m := metric(t, "1.0", "direct")
	if m.Sessions != 4 {
		t.Errorf("sessions=%d", m.Sessions)
	}
	if m.CrashedSessions != 1 || m.UnhandledSessions != 1 || m.AbnormalSessions != 1 || m.ANRSessions != 1 {
		t.Errorf("rates crashed=%d unhandled=%d abnormal=%d anr=%d", m.CrashedSessions, m.UnhandledSessions, m.AbnormalSessions, m.ANRSessions)
	}
}

func TestRollupStartupColdFilter(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("app.startup", "", "1.0", "direct", t0, map[string]any{"app.startup.ttid_ms": int64(100)}),
		rec("app.startup", "", "1.0", "direct", t0, map[string]any{"app.startup.ttid_ms": int64(200), "app.startup.type": "cold"}),
		rec("app.startup", "", "1.0", "direct", t0, map[string]any{"app.startup.ttid_ms": int64(400), "app.startup.type": "cold"}),
		rec("app.startup", "", "1.0", "direct", t0, map[string]any{"app.startup.ttid_ms": int64(999), "app.startup.type": "warm"}),
		rec("app.startup", "", "1.0", "direct", t0, map[string]any{"app.startup.ttid_ms": int64(998), "app.startup.type": "hot"}),
	)
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	m := metric(t, "1.0", "direct")
	if m.StartupCount != 3 {
		t.Errorf("startup_count=%d", m.StartupCount)
	}
	if m.TtidP50Ms == nil || *m.TtidP50Ms != 200 {
		t.Errorf("p50=%v", m.TtidP50Ms)
	}
	if m.TtidP90Ms == nil || *m.TtidP90Ms != 360 {
		t.Errorf("p90=%v", m.TtidP90Ms)
	}
}

func TestRollupJankSums(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("app.jank", "", "1.0", "direct", t0, map[string]any{"app.jank.frame_count": int64(10), "app.jank.frames": int64(100)}),
		rec("app.jank", "", "1.0", "direct", t0, map[string]any{"app.jank.frame_count": int64(5), "app.jank.frames": int64(50)}),
	)
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	m := metric(t, "1.0", "direct")
	if m.JankFramesOver != 15 || m.JankFramesTotal != 150 {
		t.Errorf("jank %d/%d", m.JankFramesOver, m.JankFramesTotal)
	}
}

func TestRollupEnvironmentSplit(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("session.start", sid(1), "1.0", "direct", t0, nil),
		rec("session.start", sid(2), "1.0", "check", t0, nil),
	)
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	d := metric(t, "1.0", "direct")
	c := metric(t, "1.0", "check")
	if d.Sessions != 1 || c.Sessions != 1 {
		t.Errorf("direct=%d check=%d", d.Sessions, c.Sessions)
	}
}

func TestRollupOneSidedKeys(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("exception", "", "1.0", "direct", t0, nil),
		rec("session.start", sid(1), "2.0", "direct", t0, nil),
	)
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	ev := metric(t, "1.0", "direct")
	if ev.Exceptions != 1 || ev.Sessions != 0 || ev.TtidP50Ms != nil {
		t.Errorf("event-only: %+v", ev)
	}
	se := metric(t, "2.0", "direct")
	if se.Sessions != 1 || se.Exceptions != 0 || se.StartupCount != 0 || se.TtidP50Ms != nil {
		t.Errorf("session-only: %+v", se)
	}
}

func TestRollupIdempotent(t *testing.T) {
	truncate(t)
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	write(t, 1, rec("session.start", sid(1), "1.0", "direct", t0, nil), rec("exception", sid(1), "1.0", "direct", t0, nil))
	from, to := receiptNow().AddDate(0, 0, -1), receiptNow()
	if err := st.Rollup(context.Background(), from, to); err != nil {
		t.Fatal(err)
	}
	first := metric(t, "1.0", "direct")
	if err := st.Rollup(context.Background(), from, to); err != nil {
		t.Fatal(err)
	}
	second := metric(t, "1.0", "direct")
	if first.Sessions != second.Sessions || first.Exceptions != second.Exceptions || first.CrashedSessions != second.CrashedSessions {
		t.Fatalf("not idempotent: %+v vs %+v", first, second)
	}
	var n int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_daily_metric`).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("row count %d", n)
	}
}

func TestPurgeExpired(t *testing.T) {
	truncate(t)
	oldDay := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	keepDay := receiptNow()
	if err := testDB.Exec(`INSERT INTO telemetry_session (app_id, session_id, service_version, environment, day, anr)
		VALUES (1, ?, '1.0', 'direct', ?::date, false), (1, ?, '1.0', 'direct', ?::date, false)`,
		sid(1), oldDay.Format("2006-01-02"), sid(2), keepDay.Format("2006-01-02")).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Exec(`INSERT INTO telemetry_daily_metric (
		app_id, service_version, environment, day, sessions, crashed_sessions, anr_sessions,
		unhandled_sessions, abnormal_sessions, exceptions, crash_java, crash_native, crash_anr,
		startup_count, jank_frames_over, jank_frames_total, updated_at)
		VALUES (1,'1.0','direct',?::date,1,0,0,0,0,0,0,0,0,0,0,0,now()),
		       (1,'1.0','direct',?::date,1,0,0,0,0,0,0,0,0,0,0,0,now())`,
		oldDay.Format("2006-01-02"), keepDay.Format("2006-01-02")).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.PurgeExpired(context.Background(), receiptNow()); err != nil {
		t.Fatal(err)
	}
	var sessions, metrics int64
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_session`).Scan(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if err := testDB.Raw(`SELECT COUNT(*) FROM telemetry_daily_metric`).Scan(&metrics).Error; err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || metrics != 1 {
		t.Fatalf("after purge sessions=%d metrics=%d", sessions, metrics)
	}
}
