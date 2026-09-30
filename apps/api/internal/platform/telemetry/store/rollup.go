package store

import (
	"context"
	"fmt"
	"time"
)

func (s *Store) Rollup(ctx context.Context, from, to time.Time) error {
	fromS := from.UTC().Format("2006-01-02")
	toS := to.UTC().Format("2006-01-02")
	const q = `
WITH sess AS (
	SELECT app_id, service_version, environment, day,
	       COUNT(*)::int AS sessions,
	       COUNT(*) FILTER (WHERE status = 'crashed')::int AS crashed_sessions,
	       COUNT(*) FILTER (WHERE anr)::int AS anr_sessions,
	       COUNT(*) FILTER (WHERE status = 'unhandled')::int AS unhandled_sessions,
	       COUNT(*) FILTER (WHERE status = 'abnormal')::int AS abnormal_sessions
	  FROM telemetry_session
	 WHERE started_at IS NOT NULL
	   AND day BETWEEN ?::date AND ?::date
	 GROUP BY 1, 2, 3, 4
),
ev AS (
	SELECT app_id, service_version, environment, event_day AS day,
	       COUNT(*) FILTER (WHERE event_name = 'exception')::int AS exceptions,
	       COUNT(*) FILTER (WHERE event_name = 'app.crash' AND attributes->>'app.exit.kind' = 'java')::int AS crash_java,
	       COUNT(*) FILTER (WHERE event_name = 'app.crash' AND attributes->>'app.exit.kind' = 'native')::int AS crash_native,
	       COUNT(*) FILTER (WHERE event_name = 'app.crash' AND attributes->>'app.exit.kind' = 'anr')::int AS crash_anr,
	       COUNT(*) FILTER (
	           WHERE event_name = 'app.startup'
	             AND (attributes->>'app.startup.type' IS NULL OR attributes->>'app.startup.type' = 'cold')
	             AND jsonb_typeof(attributes->'app.startup.ttid_ms') = 'number'
	       )::int AS startup_count,
	       percentile_cont(0.5) WITHIN GROUP (ORDER BY (attributes->>'app.startup.ttid_ms')::double precision)
	           FILTER (
	               WHERE event_name = 'app.startup'
	                 AND (attributes->>'app.startup.type' IS NULL OR attributes->>'app.startup.type' = 'cold')
	                 AND jsonb_typeof(attributes->'app.startup.ttid_ms') = 'number'
	           ) AS ttid_p50_ms,
	       percentile_cont(0.9) WITHIN GROUP (ORDER BY (attributes->>'app.startup.ttid_ms')::double precision)
	           FILTER (
	               WHERE event_name = 'app.startup'
	                 AND (attributes->>'app.startup.type' IS NULL OR attributes->>'app.startup.type' = 'cold')
	                 AND jsonb_typeof(attributes->'app.startup.ttid_ms') = 'number'
	           ) AS ttid_p90_ms,
	       COALESCE(SUM((attributes->>'app.jank.frame_count')::bigint) FILTER (
	           WHERE event_name = 'app.jank' AND jsonb_typeof(attributes->'app.jank.frame_count') = 'number'
	       ), 0) AS jank_frames_over,
	       COALESCE(SUM((attributes->>'app.jank.frames')::bigint) FILTER (
	           WHERE event_name = 'app.jank' AND jsonb_typeof(attributes->'app.jank.frames') = 'number'
	       ), 0) AS jank_frames_total
	  FROM telemetry_event
	 WHERE event_day BETWEEN ?::date AND ?::date
	 GROUP BY 1, 2, 3, 4
)
INSERT INTO telemetry_daily_metric (
	app_id, service_version, environment, day,
	sessions, crashed_sessions, anr_sessions, unhandled_sessions, abnormal_sessions,
	exceptions, crash_java, crash_native, crash_anr,
	startup_count, ttid_p50_ms, ttid_p90_ms,
	jank_frames_over, jank_frames_total, updated_at
)
SELECT
	COALESCE(s.app_id, e.app_id),
	COALESCE(s.service_version, e.service_version),
	COALESCE(s.environment, e.environment),
	COALESCE(s.day, e.day),
	COALESCE(s.sessions, 0),
	COALESCE(s.crashed_sessions, 0),
	COALESCE(s.anr_sessions, 0),
	COALESCE(s.unhandled_sessions, 0),
	COALESCE(s.abnormal_sessions, 0),
	COALESCE(e.exceptions, 0),
	COALESCE(e.crash_java, 0),
	COALESCE(e.crash_native, 0),
	COALESCE(e.crash_anr, 0),
	COALESCE(e.startup_count, 0),
	e.ttid_p50_ms,
	e.ttid_p90_ms,
	COALESCE(e.jank_frames_over, 0),
	COALESCE(e.jank_frames_total, 0),
	now()
FROM sess s
FULL OUTER JOIN ev e
  ON s.app_id = e.app_id
 AND s.service_version = e.service_version
 AND s.environment = e.environment
 AND s.day = e.day
ON CONFLICT (app_id, service_version, environment, day) DO UPDATE SET
	sessions = EXCLUDED.sessions,
	crashed_sessions = EXCLUDED.crashed_sessions,
	anr_sessions = EXCLUDED.anr_sessions,
	unhandled_sessions = EXCLUDED.unhandled_sessions,
	abnormal_sessions = EXCLUDED.abnormal_sessions,
	exceptions = EXCLUDED.exceptions,
	crash_java = EXCLUDED.crash_java,
	crash_native = EXCLUDED.crash_native,
	crash_anr = EXCLUDED.crash_anr,
	startup_count = EXCLUDED.startup_count,
	ttid_p50_ms = EXCLUDED.ttid_p50_ms,
	ttid_p90_ms = EXCLUDED.ttid_p90_ms,
	jank_frames_over = EXCLUDED.jank_frames_over,
	jank_frames_total = EXCLUDED.jank_frames_total,
	updated_at = EXCLUDED.updated_at`
	if err := s.db.WithContext(ctx).Exec(q, fromS, toS, fromS, toS).Error; err != nil {
		return fmt.Errorf("rollup: %w", err)
	}
	return nil
}
