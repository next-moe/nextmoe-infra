package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"api/internal/platform/telemetry/model"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func dateUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func alertFacts(v any) (datatypes.JSON, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(b) == 0 || string(b) == "null" {
		b = []byte("{}")
	}
	return datatypes.JSON(b), nil
}

func candidate(appID int64, rule, subject, urgency, title string, facts any, now time.Time) (model.Alert, error) {
	raw, err := alertFacts(facts)
	if err != nil {
		return model.Alert{}, err
	}
	return model.Alert{
		AppID:      appID,
		Rule:       rule,
		SubjectKey: subject,
		Urgency:    urgency,
		Title:      title,
		Facts:      raw,
		Status:     model.AlertQueued,
		CreatedAt:  now.UTC(),
	}, nil
}

func (s *Store) ListAlertChannels(ctx context.Context) ([]model.AlertChannel, error) {
	var rows []model.AlertChannel
	if err := s.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) ListEnabledAlertChannels(ctx context.Context) ([]model.AlertChannel, error) {
	var rows []model.AlertChannel
	if err := s.db.WithContext(ctx).Where("enabled").Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) AlertChannelByID(ctx context.Context, id int64) (*model.AlertChannel, error) {
	var row model.AlertChannel
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) CreateAlertChannel(ctx context.Context, kind, target string, enabled bool) (*model.AlertChannel, error) {
	row := model.AlertChannel{Kind: kind, Target: target, Enabled: enabled}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) UpdateAlertChannel(ctx context.Context, id int64, target *string, enabled *bool) (*model.AlertChannel, error) {
	row, err := s.AlertChannelByID(ctx, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]any{}
	if target != nil {
		updates["target"] = *target
	}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(row).Updates(updates).Error; err != nil {
			if isUniqueViolation(err) {
				return nil, ErrConflict
			}
			return nil, err
		}
		if err := s.db.WithContext(ctx).First(row, id).Error; err != nil {
			return nil, err
		}
	}
	return row, nil
}

func (s *Store) DeleteAlertChannel(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Delete(&model.AlertChannel{}, id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) InsertAlertCandidates(ctx context.Context, rows []model.Alert) error {
	if len(rows) == 0 {
		return nil
	}
	var b strings.Builder
	b.WriteString(`INSERT INTO telemetry_alert (
		app_id, rule, subject_key, urgency, title, facts, status, attempts, last_error, created_at
	) VALUES `)
	args := make([]any, 0, len(rows)*10)
	for i, r := range rows {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("(?,?,?,?,?,?::jsonb,?,?,?,?)")
		facts := []byte(r.Facts)
		if len(facts) == 0 {
			facts = []byte("{}")
		}
		args = append(args, r.AppID, r.Rule, r.SubjectKey, r.Urgency, r.Title, string(facts),
			model.AlertQueued, 0, "", r.CreatedAt.UTC())
	}
	b.WriteString(` ON CONFLICT (rule, app_id, subject_key) DO NOTHING`)
	if err := s.db.WithContext(ctx).Exec(b.String(), args...).Error; err != nil {
		return fmt.Errorf("insert alert candidates: %w", err)
	}
	return nil
}

func (s *Store) CollectAlertCandidates(ctx context.Context, now time.Time) ([]model.Alert, error) {
	now = now.UTC()
	settings, err := s.enabledAlertSettings(ctx)
	if err != nil {
		return nil, err
	}
	fns := []func(context.Context, time.Time, map[int64]model.AlertSettings) ([]model.Alert, error){
		s.ruleContractNew,
		s.ruleIssueNew,
		s.ruleIssueRegressed,
		s.ruleCrashRate,
		s.ruleAnrRate,
		s.ruleCrashRegression,
		s.ruleServerFaults,
		s.ruleSymbolsMissing,
		s.ruleSilentApp,
		s.ruleEngineFetchFailed,
	}
	var out []model.Alert
	for _, fn := range fns {
		rows, err := fn(ctx, now, settings)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (s *Store) enabledAlertSettings(ctx context.Context) (map[int64]model.AlertSettings, error) {
	var apps []model.App
	if err := s.db.WithContext(ctx).Where("enabled").Find(&apps).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]model.AlertSettings, len(apps))
	for _, a := range apps {
		out[a.ID] = a.AlertSettings.Effective()
	}
	return out, nil
}

type ListAlertsParams struct {
	AppID  *int64
	Status string
	Limit  int
	Offset int
}

func (s *Store) ListAlerts(ctx context.Context, p ListAlertsParams) ([]model.Alert, error) {
	if p.Limit <= 0 {
		p.Limit = 50
	}
	if p.Limit > 200 {
		p.Limit = 200
	}
	if p.Offset < 0 {
		p.Offset = 0
	}
	q := s.db.WithContext(ctx).Model(&model.Alert{})
	if p.AppID != nil {
		q = q.Where("app_id = ?", *p.AppID)
	}
	if p.Status != "" {
		q = q.Where("status = ?", p.Status)
	}
	var rows []model.Alert
	if err := q.Order("created_at DESC, id DESC").Limit(p.Limit).Offset(p.Offset).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) SuppressQueuedAlerts(ctx context.Context, reason string) error {
	return s.db.WithContext(ctx).Exec(
		`UPDATE telemetry_alert SET status = ?, last_error = ? WHERE status = ?`,
		model.AlertSuppressed, reason, model.AlertQueued,
	).Error
}

func (s *Store) ListQueuedImmediateAlerts(ctx context.Context) ([]model.Alert, error) {
	var rows []model.Alert
	if err := s.db.WithContext(ctx).
		Where("status = ? AND urgency = ?", model.AlertQueued, model.UrgencyImmediate).
		Order("created_at, id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) ListReadyDigestAlerts(ctx context.Context, now time.Time, age time.Duration) ([]model.Alert, error) {
	cutoff := now.UTC().Add(-age)
	var appIDs []int64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT app_id FROM telemetry_alert
		 WHERE status = ? AND urgency = ?
		 GROUP BY app_id
		HAVING MIN(created_at) <= ?`,
		model.AlertQueued, model.UrgencyDigest, cutoff,
	).Scan(&appIDs).Error; err != nil {
		return nil, err
	}
	if len(appIDs) == 0 {
		return nil, nil
	}
	var rows []model.Alert
	if err := s.db.WithContext(ctx).
		Where("status = ? AND urgency = ? AND app_id IN ?", model.AlertQueued, model.UrgencyDigest, appIDs).
		Order("app_id, rule, created_at, id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) MarkAlertsSent(ctx context.Context, ids []int64, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Exec(
		`UPDATE telemetry_alert SET status = ?, sent_at = ?, last_error = '' WHERE id IN ?`,
		model.AlertSent, at.UTC(), ids,
	).Error
}

func (s *Store) RecordAlertFailure(ctx context.Context, ids []int64, errMsg string) error {
	if len(ids) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Exec(
		`UPDATE telemetry_alert
		    SET attempts = attempts + 1,
		        last_error = ?,
		        status = CASE WHEN attempts + 1 >= 5 THEN ? ELSE status END
		  WHERE id IN ?`,
		errMsg, model.AlertFailed, ids,
	).Error
}

func (s *Store) AppServiceNames(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []model.App
	if err := s.db.WithContext(ctx).Select("id, service_name").Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ID] = r.ServiceName
	}
	return out, nil
}
