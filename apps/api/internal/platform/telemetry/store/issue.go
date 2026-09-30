package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/telemetry/dto"
	"api/internal/platform/telemetry/model"

	"gorm.io/gorm"
)

type IssueListParams struct {
	AppID   int64
	Kind    string
	Status  string
	Version string
	From    time.Time
	To      time.Time
	Sort    string
	Limit   int
	Offset  int
}

func (s *Store) ListIssues(ctx context.Context, p IssueListParams) ([]dto.IssueListItem, error) {
	if p.Status == "" {
		p.Status = model.IssueOpen
	}
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	fromS := p.From.Format("2006-01-02")
	toS := p.To.Format("2006-01-02")
	q := `
SELECT i.id, i.app_id, i.fingerprint, i.kind, i.title, i.culprit, i.status,
       i.resolved_in_version, i.regressed, i.first_seen_day, i.last_seen_day,
       i.first_version, i.last_version, i.created_at, i.updated_at,
       COALESCE(SUM(d.events), 0)::int AS events,
       COALESCE(SUM(d.sessions), 0)::int AS sessions
  FROM telemetry_issue i
  JOIN telemetry_issue_daily d ON d.issue_id = i.id
   AND d.day BETWEEN ?::date AND ?::date`
	args := []any{fromS, toS}
	if p.Version != "" {
		q += ` AND d.service_version = ?`
		args = append(args, p.Version)
	}
	q += ` WHERE i.app_id = ? AND i.status = ?`
	args = append(args, p.AppID, p.Status)
	if p.Kind != "" {
		q += ` AND i.kind = ?`
		args = append(args, p.Kind)
	}
	q += ` GROUP BY i.id`
	if p.Sort == "last_seen" {
		q += ` ORDER BY i.last_seen_day DESC, events DESC, i.id DESC`
	} else {
		q += ` ORDER BY events DESC, i.last_seen_day DESC, i.id DESC`
	}
	q += ` LIMIT ? OFFSET ?`
	args = append(args, p.Limit, p.Offset)

	var rows []struct {
		ID                int64
		AppID             int64
		Fingerprint       string
		Kind              string
		Title             string
		Culprit           string
		Status            string
		ResolvedInVersion string
		Regressed         bool
		FirstSeenDay      time.Time
		LastSeenDay       time.Time
		FirstVersion      string
		LastVersion       string
		CreatedAt         time.Time
		UpdatedAt         time.Time
		Events            int
		Sessions          int
	}
	if err := s.db.WithContext(ctx).Raw(q, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]int64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	seriesBy := map[int64]map[string]int{}
	if len(ids) > 0 {
		sq := `
SELECT d.issue_id, d.day, SUM(d.events)::int AS events
  FROM telemetry_issue_daily d
 WHERE d.issue_id IN ?
   AND d.day BETWEEN ?::date AND ?::date`
		sargs := []any{ids, fromS, toS}
		if p.Version != "" {
			sq += ` AND d.service_version = ?`
			sargs = append(sargs, p.Version)
		}
		sq += ` GROUP BY d.issue_id, d.day`
		var pts []struct {
			IssueID int64
			Day     time.Time
			Events  int
		}
		if err := s.db.WithContext(ctx).Raw(sq, sargs...).Scan(&pts).Error; err != nil {
			return nil, err
		}
		for _, pt := range pts {
			m := seriesBy[pt.IssueID]
			if m == nil {
				m = map[string]int{}
				seriesBy[pt.IssueID] = m
			}
			m[pt.Day.Format("2006-01-02")] = pt.Events
		}
	}
	out := make([]dto.IssueListItem, 0, len(rows))
	for _, r := range rows {
		iss := model.Issue{
			ID:                r.ID,
			AppID:             r.AppID,
			Fingerprint:       r.Fingerprint,
			Kind:              r.Kind,
			Title:             r.Title,
			Culprit:           r.Culprit,
			Status:            r.Status,
			ResolvedInVersion: r.ResolvedInVersion,
			Regressed:         r.Regressed,
			FirstSeenDay:      r.FirstSeenDay,
			LastSeenDay:       r.LastSeenDay,
			FirstVersion:      r.FirstVersion,
			LastVersion:       r.LastVersion,
			CreatedAt:         r.CreatedAt,
			UpdatedAt:         r.UpdatedAt,
		}
		item := dto.IssueListItem{
			IssueView: dto.IssueViewFrom(iss),
			Events:    r.Events,
			Sessions:  r.Sessions,
			Series:    daySeries(p.From, p.To, seriesBy[r.ID]),
		}
		out = append(out, item)
	}
	return out, nil
}

func daySeries(from, to time.Time, events map[string]int) []dto.IssueDayPoint {
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	to = time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	n := int(to.Sub(from).Hours()/24) + 1
	if n < 0 {
		n = 0
	}
	out := make([]dto.IssueDayPoint, 0, n)
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		out = append(out, dto.IssueDayPoint{Day: key, Events: events[key]})
	}
	return out
}

func (s *Store) GetIssue(ctx context.Context, id int64, now time.Time) (*dto.IssueDetail, error) {
	var iss model.Issue
	if err := s.db.WithContext(ctx).First(&iss, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	from := today.AddDate(0, 0, -29)
	var dailies []model.IssueDaily
	if err := s.db.WithContext(ctx).
		Where("issue_id = ? AND day BETWEEN ? AND ?", id, from.Format("2006-01-02"), today.Format("2006-01-02")).
		Order("day, service_version").
		Find(&dailies).Error; err != nil {
		return nil, err
	}
	dailyViews := make([]dto.IssueDailyView, 0, len(dailies))
	for _, d := range dailies {
		dailyViews = append(dailyViews, dto.IssueDailyView{
			Day:            d.Day.Format("2006-01-02"),
			ServiceVersion: d.ServiceVersion,
			Events:         d.Events,
			Sessions:       d.Sessions,
		})
	}
	var crashRows []struct {
		EventDay       time.Time
		ServiceVersion string
		Status         string
		Needs          string
		ExceptionType  string
		Message        string
		Stack          string
		Handled        *bool
		Attributes     []byte
		DeviceModel    string
		OSVersion      string
		APILevel       *int
		HostArch       string
	}
	if err := s.db.WithContext(ctx).Raw(`
SELECT c.event_day, c.service_version, c.status, c.needs, c.exception_type, c.message, c.stack, c.handled,
       e.attributes, e.device_model, e.os_version, e.api_level, e.host_arch
  FROM telemetry_crash c
  JOIN telemetry_event e ON e.event_day = c.event_day AND e.record_uid = c.record_uid
 WHERE c.issue_id = ?
 ORDER BY c.event_day DESC, c.record_uid DESC
 LIMIT 20`, id).Scan(&crashRows).Error; err != nil {
		return nil, err
	}
	crashes := make([]dto.IssueCrashView, 0, len(crashRows))
	for _, r := range crashRows {
		attrs := map[string]any{}
		if len(r.Attributes) > 0 {
			_ = json.Unmarshal(r.Attributes, &attrs)
		}
		crashes = append(crashes, dto.IssueCrashView{
			EventDay:       r.EventDay.Format("2006-01-02"),
			ServiceVersion: r.ServiceVersion,
			Status:         r.Status,
			Needs:          r.Needs,
			ExceptionType:  r.ExceptionType,
			Message:        r.Message,
			Stack:          r.Stack,
			RawStack:       attrString(attrs, "exception.stacktrace"),
			Breadcrumbs:    breadcrumbs(attrs),
			Handled:        r.Handled,
			DeviceModel:    r.DeviceModel,
			OSVersion:      r.OSVersion,
			APILevel:       r.APILevel,
			HostArch:       r.HostArch,
		})
	}
	view := dto.IssueViewFrom(iss)
	return &dto.IssueDetail{IssueView: view, Daily: dailyViews, Crashes: crashes}, nil
}

func attrString(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func breadcrumbs(attrs map[string]any) []string {
	v, ok := attrs["app.breadcrumbs"]
	if !ok || v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		if t == nil {
			return []string{}
		}
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, x := range t {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return []string{}
}

func (s *Store) UpdateIssue(ctx context.Context, id int64, status string, resolvedIn *string) (*model.Issue, error) {
	var row model.Issue
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	updates := map[string]any{"status": status}
	if status == model.IssueResolved {
		updates["regressed"] = false
	}
	if resolvedIn != nil {
		updates["resolved_in_version"] = *resolvedIn
	}
	if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func purgeCrashes(db *gorm.DB, cut string) error {
	for {
		res := db.Exec(`DELETE FROM telemetry_crash WHERE ctid IN (
			SELECT ctid FROM telemetry_crash WHERE event_day < ?::date LIMIT 10000
		)`, cut)
		if res.Error != nil {
			return fmt.Errorf("purge crashes: %w", res.Error)
		}
		if res.RowsAffected < 10000 {
			return nil
		}
	}
}

func purgeIssueDaily(db *gorm.DB, cut string) error {
	if err := db.Exec(`DELETE FROM telemetry_issue_daily WHERE day < ?::date`, cut).Error; err != nil {
		return fmt.Errorf("purge issue daily: %w", err)
	}
	return nil
}
