package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/telemetry/model"
	"api/internal/platform/telemetry/otlp"
	"api/internal/platform/telemetry/symbolicate"

	"gorm.io/gorm"
)

func CrashKind(eventName string, attrs map[string]any) (string, bool) {
	switch eventName {
	case "exception":
		switch symbolicate.AttrString(attrs, "app.fault") {
		case model.KindContract:
			return model.KindContract, true
		case model.KindServer:
			return model.KindServer, true
		default:
			return model.KindException, true
		}
	case "app.crash":
		k := symbolicate.AttrString(attrs, "app.exit.kind")
		switch k {
		case model.KindJava, model.KindNative, model.KindANR:
			return k, true
		default:
			return model.KindNative, true
		}
	default:
		return "", false
	}
}

func insertCrashes(tx *gorm.DB, appID int64, recs []otlp.Record) error {
	type row struct {
		day, uid, version, eventName, kind, typ, msg, stack string
		sid                                                 any
		handled                                             any
	}
	seen := map[string]struct{}{}
	var rows []row
	for _, r := range recs {
		kind, ok := CrashKind(r.EventName, r.Attributes)
		if !ok {
			continue
		}
		key := r.EventDay.Format("2006-01-02") + "\x00" + r.RecordUID
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		var sid any
		if r.SessionID != "" {
			sid = r.SessionID
		}
		var handled any
		if p := symbolicate.AttrBoolPtr(r.Attributes, "app.exception.handled"); p != nil {
			handled = *p
		}
		rows = append(rows, row{
			day:       r.EventDay.Format("2006-01-02"),
			uid:       r.RecordUID,
			version:   r.ServiceVersion,
			eventName: r.EventName,
			kind:      kind,
			typ:       symbolicate.AttrString(r.Attributes, "exception.type"),
			msg:       symbolicate.AttrString(r.Attributes, "exception.message"),
			stack:     symbolicate.AttrString(r.Attributes, "exception.stacktrace"),
			sid:       sid,
			handled:   handled,
		})
	}
	if len(rows) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO telemetry_crash (
		event_day, record_uid, app_id, service_version, session_id,
		event_name, kind, handled, status, needs, attempts,
		exception_type, message, stack, frames, fingerprint
	) VALUES `)
	args := make([]any, 0, len(rows)*16)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?)")
		args = append(args,
			r.day, r.uid, appID, r.version, r.sid,
			r.eventName, r.kind, r.handled, model.CrashPending, "", 0,
			r.typ, r.msg, r.stack, "[]", "",
		)
	}
	b.WriteString(` ON CONFLICT (event_day, record_uid) DO NOTHING`)
	if err := tx.Exec(b.String(), args...).Error; err != nil {
		return fmt.Errorf("insert crashes: %w", err)
	}
	return nil
}

type ClaimedCrash struct {
	Crash      model.Crash
	Attributes map[string]any
	Prefixes   []string
}

func ClaimCrash(tx *gorm.DB) (*ClaimedCrash, error) {
	var row struct {
		EventDay       time.Time
		RecordUID      string
		AppID          int64
		ServiceVersion string
		SessionID      *string
		EventName      string
		Kind           string
		Handled        *bool
		Status         string
		Needs          string
		Attempts       int
		ExceptionType  string
		Message        string
		Stack          string
		Frames         []byte
		Fingerprint    string
		IssueID        *int64
		Attributes     []byte
		Prefixes       []byte
	}
	err := tx.Raw(`
SELECT c.event_day, c.record_uid, c.app_id, c.service_version, c.session_id,
       c.event_name, c.kind, c.handled, c.status, c.needs, c.attempts,
       c.exception_type, c.message, c.stack, c.frames, c.fingerprint, c.issue_id,
       e.attributes, COALESCE(a.in_app_prefixes, '[]'::jsonb) AS prefixes
  FROM telemetry_crash c
  JOIN telemetry_event e ON e.event_day = c.event_day AND e.record_uid = c.record_uid
  LEFT JOIN telemetry_app a ON a.id = c.app_id
 WHERE c.status = ?
 ORDER BY c.event_day, c.record_uid
 LIMIT 1
 FOR UPDATE OF c SKIP LOCKED`, model.CrashPending).Scan(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if row.RecordUID == "" {
		return nil, nil
	}
	attrs := map[string]any{}
	if len(row.Attributes) > 0 {
		if err := json.Unmarshal(row.Attributes, &attrs); err != nil {
			return nil, fmt.Errorf("crash attributes: %w", err)
		}
	}
	var prefixes []string
	if len(row.Prefixes) > 0 {
		_ = json.Unmarshal(row.Prefixes, &prefixes)
	}
	if prefixes == nil {
		prefixes = []string{}
	}
	return &ClaimedCrash{
		Crash: model.Crash{
			EventDay:       row.EventDay,
			RecordUID:      row.RecordUID,
			AppID:          row.AppID,
			ServiceVersion: row.ServiceVersion,
			SessionID:      row.SessionID,
			EventName:      row.EventName,
			Kind:           row.Kind,
			Handled:        row.Handled,
			Status:         row.Status,
			Needs:          row.Needs,
			Attempts:       row.Attempts,
			ExceptionType:  row.ExceptionType,
			Message:        row.Message,
			Stack:          row.Stack,
			Frames:         row.Frames,
			Fingerprint:    row.Fingerprint,
			IssueID:        row.IssueID,
		},
		Attributes: attrs,
		Prefixes:   prefixes,
	}, nil
}

type CrashResult struct {
	Status        string
	Needs         string
	Attempts      int
	ExceptionType string
	Message       string
	Stack         string
	Frames        []symbolicate.Frame
	Fingerprint   string
	Title         string
	Culprit       string
}

func FinishCrash(tx *gorm.DB, job *ClaimedCrash, res CrashResult) error {
	day := job.Crash.EventDay.Format("2006-01-02")
	if res.Status == model.CrashPending {
		if err := tx.Exec(`
UPDATE telemetry_crash SET attempts = ?
 WHERE event_day = ?::date AND record_uid = ?`,
			res.Attempts, day, job.Crash.RecordUID).Error; err != nil {
			return fmt.Errorf("bump crash attempts: %w", err)
		}
		return nil
	}
	frames, err := json.Marshal(res.Frames)
	if err != nil {
		return err
	}
	if string(frames) == "null" {
		frames = []byte("[]")
	}
	oldID := job.Crash.IssueID
	if err := tx.Exec(`
UPDATE telemetry_crash SET
	status = ?, needs = ?, attempts = ?, exception_type = ?, message = ?,
	stack = ?, frames = ?::jsonb, fingerprint = ?
 WHERE event_day = ?::date AND record_uid = ?`,
		res.Status, res.Needs, res.Attempts, res.ExceptionType, res.Message,
		res.Stack, string(frames), res.Fingerprint,
		day, job.Crash.RecordUID).Error; err != nil {
		return fmt.Errorf("update crash: %w", err)
	}
	issueID, err := upsertIssue(tx, job, res)
	if err != nil {
		return err
	}
	if err := tx.Exec(`
UPDATE telemetry_crash SET issue_id = ?
 WHERE event_day = ?::date AND record_uid = ?`,
		issueID, day, job.Crash.RecordUID).Error; err != nil {
		return err
	}
	if err := recountDaily(tx, issueID, job.Crash.EventDay, job.Crash.ServiceVersion); err != nil {
		return err
	}
	if oldID != nil && *oldID != issueID {
		if err := recountDaily(tx, *oldID, job.Crash.EventDay, job.Crash.ServiceVersion); err != nil {
			return err
		}
		if err := deleteIssueIfEmpty(tx, *oldID); err != nil {
			return err
		}
	}
	return nil
}

func upsertIssue(tx *gorm.DB, job *ClaimedCrash, res CrashResult) (int64, error) {
	day := job.Crash.EventDay.Format("2006-01-02")
	var id int64
	err := tx.Raw(`
INSERT INTO telemetry_issue (
	app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
	first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, '', false, ?::date, ?::date, ?, ?, now(), now())
ON CONFLICT (app_id, fingerprint) DO UPDATE SET
	last_seen_day = CASE WHEN EXCLUDED.last_seen_day > telemetry_issue.last_seen_day
		THEN EXCLUDED.last_seen_day ELSE telemetry_issue.last_seen_day END,
	last_version = CASE WHEN EXCLUDED.last_seen_day > telemetry_issue.last_seen_day
		THEN EXCLUDED.last_version ELSE telemetry_issue.last_version END,
	first_seen_day = CASE WHEN EXCLUDED.first_seen_day < telemetry_issue.first_seen_day
		THEN EXCLUDED.first_seen_day ELSE telemetry_issue.first_seen_day END,
	first_version = CASE WHEN EXCLUDED.first_seen_day < telemetry_issue.first_seen_day
		THEN EXCLUDED.first_version ELSE telemetry_issue.first_version END,
	status = CASE WHEN telemetry_issue.status = ? THEN ? ELSE telemetry_issue.status END,
	regressed = CASE WHEN telemetry_issue.status = ? THEN true ELSE telemetry_issue.regressed END,
	updated_at = now()
RETURNING id`,
		job.Crash.AppID, res.Fingerprint, job.Crash.Kind, res.Title, res.Culprit,
		model.IssueOpen, day, day, job.Crash.ServiceVersion, job.Crash.ServiceVersion,
		model.IssueResolved, model.IssueOpen, model.IssueResolved,
	).Scan(&id).Error
	if err != nil {
		return 0, fmt.Errorf("upsert issue: %w", err)
	}
	return id, nil
}

func recountDaily(tx *gorm.DB, issueID int64, day time.Time, version string) error {
	ds := day.Format("2006-01-02")
	if err := tx.Exec(`
DELETE FROM telemetry_issue_daily
 WHERE issue_id = ? AND day = ?::date AND service_version = ?`,
		issueID, ds, version).Error; err != nil {
		return fmt.Errorf("delete issue daily: %w", err)
	}
	if err := tx.Exec(`
INSERT INTO telemetry_issue_daily (issue_id, day, service_version, events, sessions)
SELECT issue_id, event_day, service_version,
       COUNT(*)::int,
       COUNT(DISTINCT session_id) FILTER (WHERE session_id IS NOT NULL)::int
  FROM telemetry_crash
 WHERE issue_id = ? AND event_day = ?::date AND service_version = ?
 GROUP BY issue_id, event_day, service_version`,
		issueID, ds, version).Error; err != nil {
		return fmt.Errorf("insert issue daily: %w", err)
	}
	return nil
}

func deleteIssueIfEmpty(tx *gorm.DB, issueID int64) error {
	if err := tx.Exec(`
DELETE FROM telemetry_issue i
 WHERE i.id = ?
   AND NOT EXISTS (SELECT 1 FROM telemetry_crash c WHERE c.issue_id = i.id)
   AND NOT EXISTS (SELECT 1 FROM telemetry_issue_daily d WHERE d.issue_id = i.id)`,
		issueID).Error; err != nil {
		return fmt.Errorf("delete empty issue: %w", err)
	}
	return nil
}

func (s *Store) RequeueReadyCrashes(ctx context.Context) error {
	var rows []struct {
		EventDay       time.Time
		RecordUID      string
		AppID          int64
		ServiceVersion string
		Needs          string
	}
	if err := s.db.WithContext(ctx).Raw(`
SELECT event_day, record_uid, app_id, service_version, needs
  FROM telemetry_crash
 WHERE status = ? AND needs <> ''`, model.CrashWaitingSymbols).Scan(&rows).Error; err != nil {
		return err
	}
	for _, r := range rows {
		ok, err := s.needsSatisfied(ctx, r.AppID, r.ServiceVersion, r.Needs)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err := s.db.WithContext(ctx).Exec(`
UPDATE telemetry_crash SET status = ?
 WHERE event_day = ?::date AND record_uid = ? AND status = ?`,
			model.CrashPending, r.EventDay.Format("2006-01-02"), r.RecordUID, model.CrashWaitingSymbols,
		).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) needsSatisfied(ctx context.Context, appID int64, version, needs string) (bool, error) {
	for _, n := range symbolicate.SplitNeeds(needs) {
		kind, val, ok := strings.Cut(n, ":")
		if !ok || val == "" {
			return false, nil
		}
		switch kind {
		case "dart":
			_, err := s.DartSymbolsByBuildID(ctx, appID, val)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
		case "r8":
			_, err := s.R8MappingForVersion(ctx, appID, val)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
		case "engine":
			_, err := s.EngineSymbolByBuildID(ctx, val)
			if errors.Is(err, ErrNotFound) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
		default:
			return false, nil
		}
	}
	return true, nil
}

func (s *Store) ClaimOne(ctx context.Context, fn func(tx *gorm.DB, job *ClaimedCrash) error) (bool, error) {
	var claimed bool
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		job, err := ClaimCrash(tx)
		if err != nil {
			return err
		}
		if job == nil {
			return nil
		}
		claimed = true
		return fn(tx, job)
	})
	return claimed, err
}
