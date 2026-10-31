package repository

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

const activeHourRetention = 200 * 24 * time.Hour

func markSignIn(ctx context.Context, db *gorm.DB, userID uint) {
	err := db.WithContext(ctx).Exec(`
WITH hour AS (
  INSERT INTO user_active_hours (user_id, hour) VALUES (?, date_trunc('hour', now()))
  ON CONFLICT DO NOTHING
)
UPDATE users SET last_login_at = now(), last_active_at = now() WHERE id = ?`, userID, userID).Error
	if err != nil {
		slog.Warn("user activity: sign-in not recorded", "user_id", userID, "err", err)
	}
}

func markSessionActive(ctx context.Context, db *gorm.DB, sessionID uint) {
	err := db.WithContext(ctx).Exec(`
WITH hour AS (
  INSERT INTO user_active_hours (user_id, hour)
  SELECT user_id, date_trunc('hour', now()) FROM sessions WHERE id = ?
  ON CONFLICT DO NOTHING
  RETURNING user_id
)
UPDATE users SET last_active_at = now() WHERE id IN (SELECT user_id FROM hour)`, sessionID).Error
	if err != nil {
		slog.Warn("user activity: refresh not recorded", "session_id", sessionID, "err", err)
	}
}

type ActivityRepository struct {
	db *gorm.DB
}

func NewActivityRepository(db *gorm.DB) *ActivityRepository {
	return &ActivityRepository{db: db}
}

func (r *ActivityRepository) ActiveCountsByDay(ctx context.Context, since time.Time, tz string) (map[string]int, error) {
	type row struct {
		Day   string
		Count int
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&model.UserActiveHour{}).
		Select("to_char((hour AT TIME ZONE ?), 'YYYY-MM-DD') AS day, count(DISTINCT user_id) AS count", tz).
		Where("hour >= ?", since).
		Group("day").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int, len(rows))
	for _, rw := range rows {
		out[rw.Day] = rw.Count
	}
	return out, nil
}

func (r *ActivityRepository) ActiveCountsByHour(ctx context.Context, dayStart, dayEnd time.Time, tz string) (map[int]int, error) {
	type row struct {
		Hr    int
		Count int
	}
	var rows []row
	if err := r.db.WithContext(ctx).
		Model(&model.UserActiveHour{}).
		Select("extract(hour from (hour AT TIME ZONE ?))::int AS hr, count(*) AS count", tz).
		Where("hour >= ? AND hour < ?", dayStart, dayEnd).
		Group("hr").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int]int, len(rows))
	for _, rw := range rows {
		out[rw.Hr] = rw.Count
	}
	return out, nil
}

func (r *ActivityRepository) UniqueActive(ctx context.Context, from, to time.Time) (int, error) {
	var n int
	err := r.db.WithContext(ctx).
		Model(&model.UserActiveHour{}).
		Select("count(DISTINCT user_id)").
		Where("hour >= ? AND hour < ?", from, to).
		Scan(&n).Error
	return n, err
}

func (r *ActivityRepository) Prune(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("hour < ?", time.Now().Add(-activeHourRetention)).
		Delete(&model.UserActiveHour{}).Error
}

func (r *ActivityRepository) FirstHour(ctx context.Context) (*time.Time, error) {
	var t *time.Time
	err := r.db.WithContext(ctx).
		Model(&model.UserActiveHour{}).
		Select("min(hour)").
		Scan(&t).Error
	return t, err
}
