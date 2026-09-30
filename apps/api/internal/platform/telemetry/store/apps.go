package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"api/internal/platform/telemetry/dto"
	"api/internal/platform/telemetry/model"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("telemetry app not found")
var ErrConflict = errors.New("telemetry app already exists")

func randomKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (s *Store) ListAppRows(ctx context.Context) ([]model.App, error) {
	var rows []model.App
	if err := s.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (s *Store) CreateApp(ctx context.Context, serviceName, displayName string) (*model.App, error) {
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	row := model.App{
		ServiceName:   serviceName,
		DisplayName:   displayName,
		IngestKey:     key,
		Enabled:       true,
		InAppPrefixes: datatypes.JSONSlice[string]{},
	}
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return &row, nil
}

func (s *Store) UpdateApp(ctx context.Context, id int64, displayName *string, enabled *bool, prefixes *[]string, settings *model.AlertSettings) (*model.App, error) {
	var row model.App
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	updates := map[string]any{}
	if displayName != nil {
		updates["display_name"] = *displayName
	}
	if enabled != nil {
		updates["enabled"] = *enabled
	}
	if prefixes != nil {
		updates["in_app_prefixes"] = datatypes.JSONSlice[string](*prefixes)
	}
	if settings != nil {
		updates["alert_settings"] = *settings
	}
	if len(updates) > 0 {
		if err := s.db.WithContext(ctx).Model(&row).Updates(updates).Error; err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
			return nil, err
		}
	}
	return &row, nil
}

func (s *Store) RotateKey(ctx context.Context, id int64) (*model.App, error) {
	var row model.App
	if err := s.db.WithContext(ctx).First(&row, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&row).Update("ingest_key", key).Error; err != nil {
		return nil, err
	}
	row.IngestKey = key
	return &row, nil
}

func (s *Store) ListDailyMetrics(ctx context.Context, appID int64, environment string, from, to time.Time) ([]dto.DailyMetricView, error) {
	var rows []model.DailyMetric
	if err := s.db.WithContext(ctx).
		Where("app_id = ? AND environment = ? AND day BETWEEN ? AND ?", appID, environment, from.Format("2006-01-02"), to.Format("2006-01-02")).
		Order("day DESC, service_version DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]dto.DailyMetricView, 0, len(rows))
	for _, r := range rows {
		out = append(out, dto.FromModel(r))
	}
	return out, nil
}

func (s *Store) PurgeExpired(ctx context.Context, now time.Time) error {
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	sessionCut := today.AddDate(0, 0, -30).Format("2006-01-02")
	for {
		res := s.db.WithContext(ctx).Exec(
			`DELETE FROM telemetry_session WHERE ctid IN (
				SELECT ctid FROM telemetry_session WHERE day < ?::date LIMIT 10000
			)`, sessionCut)
		if res.Error != nil {
			return fmt.Errorf("purge sessions: %w", res.Error)
		}
		if res.RowsAffected < 10000 {
			break
		}
	}
	metricCut := today.AddDate(0, -13, 0).Format("2006-01-02")
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM telemetry_daily_metric WHERE day < ?::date`, metricCut).Error; err != nil {
		return fmt.Errorf("purge metrics: %w", err)
	}
	if err := purgeCrashes(s.db.WithContext(ctx), sessionCut); err != nil {
		return err
	}
	if err := purgeIssueDaily(s.db.WithContext(ctx), metricCut); err != nil {
		return err
	}
	alertCut := now.UTC().AddDate(0, 0, -400)
	if err := s.db.WithContext(ctx).Exec(`DELETE FROM telemetry_alert WHERE created_at < ?`, alertCut).Error; err != nil {
		return fmt.Errorf("purge alerts: %w", err)
	}
	return nil
}
