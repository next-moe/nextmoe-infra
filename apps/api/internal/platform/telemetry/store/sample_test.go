package store

import (
	"context"
	"testing"
	"time"

	"api/internal/platform/telemetry/model"
)

func TestSampleReplay(t *testing.T) {
	truncate(t)
	const (
		v1  = "0.1.0+9001"
		v2  = "0.1.0+9002"
		env = "check"
	)
	s := func(n byte) string { return sid(n) }
	t0 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	at := func(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }

	write(t, 1,
		rec("session.start", s(1), v1, env, at(0), nil),
		rec("session.start", s(2), v1, env, at(1), nil),
		rec("session.start", s(3), v1, env, at(2), nil),
	)
	write(t, 1,
		rec("exception", s(1), v1, env, at(3), map[string]any{"exception.type": "A"}),
		rec("exception", s(2), v1, env, at(4), map[string]any{"exception.type": "B"}),
		rec("app.startup", s(1), v1, env, at(5), map[string]any{"app.startup.ttid_ms": int64(100)}),
		rec("app.startup", s(2), v1, env, at(6), map[string]any{"app.startup.ttid_ms": int64(200), "app.startup.type": "cold"}),
		rec("app.startup", s(3), v1, env, at(7), map[string]any{"app.startup.ttid_ms": int64(400)}),
		rec("app.jank", s(1), v1, env, at(8), map[string]any{"app.jank.frame_count": int64(1), "app.jank.frames": int64(100)}),
		rec("app.jank", s(1), v1, env, at(9), map[string]any{"app.jank.frame_count": int64(2), "app.jank.frames": int64(100)}),
		rec("app.jank", s(2), v1, env, at(10), map[string]any{"app.jank.frame_count": int64(3), "app.jank.frames": int64(100)}),
		rec("app.jank", s(2), v1, env, at(11), map[string]any{"app.jank.frame_count": int64(4), "app.jank.frames": int64(100)}),
		rec("app.jank", s(3), v1, env, at(12), map[string]any{"app.jank.frame_count": int64(5), "app.jank.frames": int64(100)}),
	)
	write(t, 1,
		rec("session.end", s(1), v1, env, at(20), map[string]any{"app.session.status": "crashed"}),
		rec("app.crash", s(1), v1, env, at(20), map[string]any{"app.exit.kind": "java"}),
		rec("session.start", s(5), v2, env, at(21), nil),
	)
	write(t, 1,
		rec("session.start", s(4), v1, env, at(22), nil),
		rec("session.start", s(6), v2, env, at(23), nil),
		rec("session.end", s(5), v2, env, at(24), map[string]any{"app.session.status": "exited"}),
		rec("session.end", s(6), v2, env, at(25), map[string]any{"app.session.status": "crashed"}),
		rec("app.crash", s(6), v2, env, at(25), map[string]any{"app.exit.kind": "native"}),
		rec("app.startup", s(5), v2, env, at(26), map[string]any{"app.startup.ttid_ms": int64(50)}),
		rec("app.startup", s(6), v2, env, at(27), map[string]any{"app.startup.ttid_ms": int64(150)}),
		rec("app.jank", s(5), v2, env, at(28), map[string]any{"app.jank.frame_count": int64(7), "app.jank.frames": int64(200)}),
	)
	write(t, 1,
		rec("session.end", s(2), v1, env, at(30), map[string]any{"app.session.status": "unhandled"}),
		rec("session.end", s(3), v1, env, at(31), map[string]any{"app.session.status": "exited"}),
	)

	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}

	assert := func(version string, want model.DailyMetric) {
		t.Helper()
		got := metric(t, version, env)
		if got.Sessions != want.Sessions || got.CrashedSessions != want.CrashedSessions ||
			got.UnhandledSessions != want.UnhandledSessions || got.AbnormalSessions != want.AbnormalSessions ||
			got.ANRSessions != want.ANRSessions || got.Exceptions != want.Exceptions ||
			got.CrashJava != want.CrashJava || got.CrashNative != want.CrashNative || got.CrashANR != want.CrashANR ||
			got.StartupCount != want.StartupCount || got.JankFramesOver != want.JankFramesOver ||
			got.JankFramesTotal != want.JankFramesTotal {
			t.Errorf("%s counts got sessions=%d crashed=%d unhandled=%d abnormal=%d anr=%d ex=%d java=%d native=%d anrC=%d start=%d jank=%d/%d",
				version, got.Sessions, got.CrashedSessions, got.UnhandledSessions, got.AbnormalSessions, got.ANRSessions,
				got.Exceptions, got.CrashJava, got.CrashNative, got.CrashANR, got.StartupCount, got.JankFramesOver, got.JankFramesTotal)
		}
		if (got.TtidP50Ms == nil) != (want.TtidP50Ms == nil) || (got.TtidP50Ms != nil && *got.TtidP50Ms != *want.TtidP50Ms) {
			t.Errorf("%s p50 got %v want %v", version, got.TtidP50Ms, want.TtidP50Ms)
		}
		if (got.TtidP90Ms == nil) != (want.TtidP90Ms == nil) || (got.TtidP90Ms != nil && *got.TtidP90Ms != *want.TtidP90Ms) {
			t.Errorf("%s p90 got %v want %v", version, got.TtidP90Ms, want.TtidP90Ms)
		}
	}
	p50a, p90a := 200.0, 360.0
	p50b, p90b := 100.0, 140.0
	assert(v1, model.DailyMetric{
		Sessions: 4, CrashedSessions: 1, UnhandledSessions: 1, AbnormalSessions: 0, ANRSessions: 0,
		Exceptions: 2, CrashJava: 1, CrashNative: 0, CrashANR: 0, StartupCount: 3,
		TtidP50Ms: &p50a, TtidP90Ms: &p90a, JankFramesOver: 15, JankFramesTotal: 500,
	})
	assert(v2, model.DailyMetric{
		Sessions: 2, CrashedSessions: 1, UnhandledSessions: 0, AbnormalSessions: 0, ANRSessions: 0,
		Exceptions: 0, CrashJava: 0, CrashNative: 1, CrashANR: 0, StartupCount: 2,
		TtidP50Ms: &p50b, TtidP90Ms: &p90b, JankFramesOver: 7, JankFramesTotal: 200,
	})
}
