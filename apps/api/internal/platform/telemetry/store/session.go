package store

import (
	"fmt"
	"sort"
	"time"

	"api/internal/platform/telemetry/otlp"

	"gorm.io/gorm"
)

type sessionDelta struct {
	serviceVersion string
	environment    string
	day            time.Time
	startedAt      *time.Time
	endedAt        *time.Time
	status         *string
	errors         *int
	anr            bool
}

var sessionUpsertObserver func(sessionID string)

func foldSessions(receipt time.Time, recs []otlp.Record) map[string]sessionDelta {
	type acc struct {
		delta   sessionDelta
		endSeen bool
	}
	out := map[string]*acc{}
	for _, r := range recs {
		if r.SessionID == "" {
			continue
		}
		a, ok := out[r.SessionID]
		if !ok {
			a = &acc{delta: sessionDelta{
				serviceVersion: r.ServiceVersion,
				environment:    r.Environment,
			}}
			out[r.SessionID] = a
		}
		if r.EventName == "session.start" {
			t := r.EventTime
			if a.delta.startedAt == nil || t.Before(*a.delta.startedAt) {
				a.delta.startedAt = &t
			}
		}
		if r.EventName == "session.end" && !a.endSeen {
			a.endSeen = true
			t := r.EventTime
			a.delta.endedAt = &t
			if s, ok := r.Attributes["app.session.status"].(string); ok {
				a.delta.status = &s
			}
			if n, ok := asInt(r.Attributes["app.session.errors"]); ok {
				a.delta.errors = &n
			}
		}
		if r.EventName == "app.crash" {
			if kind, _ := r.Attributes["app.exit.kind"].(string); kind == "anr" {
				a.delta.anr = true
			}
		}
	}
	result := make(map[string]sessionDelta, len(out))
	for id, a := range out {
		var t time.Time
		if a.delta.startedAt != nil {
			t = *a.delta.startedAt
		} else {
			t = earliestEvent(recs, id)
		}
		day, keep := otlp.ClassifyDay(t, receipt)
		if !keep {
			continue
		}
		a.delta.day = day
		result[id] = a.delta
	}
	return result
}

func orderedSessionIDs(deltas map[string]sessionDelta) []string {
	ids := make([]string, 0, len(deltas))
	for id := range deltas {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func earliestEvent(recs []otlp.Record, sessionID string) time.Time {
	var t time.Time
	for _, r := range recs {
		if r.SessionID != sessionID {
			continue
		}
		if t.IsZero() || r.EventTime.Before(t) {
			t = r.EventTime
		}
	}
	return t
}

func asInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		return int(x), true
	default:
		return 0, false
	}
}

func upsertSessions(tx *gorm.DB, appID int64, receipt time.Time, recs []otlp.Record) error {
	deltas := foldSessions(receipt, recs)
	const q = `
INSERT INTO telemetry_session (
	app_id, session_id, service_version, environment, day,
	started_at, ended_at, status, errors, anr
) VALUES (?, ?, ?, ?, ?::date, ?, ?, ?, ?, ?)
ON CONFLICT (app_id, session_id) DO UPDATE SET
	started_at = COALESCE(telemetry_session.started_at, EXCLUDED.started_at),
	status = CASE WHEN telemetry_session.status IS NOT NULL THEN telemetry_session.status ELSE EXCLUDED.status END,
	errors = CASE WHEN telemetry_session.status IS NOT NULL THEN telemetry_session.errors ELSE EXCLUDED.errors END,
	ended_at = CASE WHEN telemetry_session.status IS NOT NULL THEN telemetry_session.ended_at ELSE EXCLUDED.ended_at END,
	anr = telemetry_session.anr OR EXCLUDED.anr,
	day = CASE
		WHEN telemetry_session.started_at IS NULL AND EXCLUDED.started_at IS NOT NULL THEN EXCLUDED.day
		ELSE telemetry_session.day
	END`
	for _, sid := range orderedSessionIDs(deltas) {
		if sessionUpsertObserver != nil {
			sessionUpsertObserver(sid)
		}
		d := deltas[sid]
		if err := tx.Exec(q,
			appID, sid, d.serviceVersion, d.environment, d.day.Format("2006-01-02"),
			d.startedAt, d.endedAt, d.status, d.errors, d.anr,
		).Error; err != nil {
			return fmt.Errorf("upsert session %s: %w", sid, err)
		}
	}
	return nil
}
