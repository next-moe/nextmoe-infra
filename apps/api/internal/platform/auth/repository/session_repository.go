package repository

import (
	"context"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

type SessionRepository struct {
	db *gorm.DB
}

func NewSessionRepository(db *gorm.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) FindBySessionToken(ctx context.Context, token string) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).Where("session_token = ?", token).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) FindByRefreshToken(ctx context.Context, token string) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).Where("refresh_token = ?", token).First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) FindByRefreshTokenOrPrev(ctx context.Context, token string) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).
		Where("refresh_token = ? OR prev_refresh_token = ?", token, token).
		First(&session).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

func (r *SessionRepository) FindByID(ctx context.Context, id uint) (*model.Session, error) {
	var session model.Session
	if err := r.db.WithContext(ctx).First(&session, id).Error; err != nil {
		return nil, err
	}
	return &session, nil
}

// RotateRefreshToken atomically rotates a session's tokens, but ONLY if its
// current refresh_token still equals oldRefresh (compare-and-swap). It demotes
// oldRefresh to prev_refresh_token and installs the new access/refresh tokens.
// Returns won=true when THIS call performed the rotation (1 row matched), or
// won=false when a concurrent refresh already rotated (0 rows) — the caller
// then converges on the winner's current token.
//
// This is the fix for the lost-update race: two concurrent refreshes that read
// the same current token would otherwise both Save divergent new tokens, so the
// cookie and DB end up holding different tokens → the next request 401s with a
// token that is neither current nor prev. The CAS lets exactly one win. Only
// the rotation columns are written (like TouchLastUsed) so it can't clobber a
// concurrent single-column update on the same row.
func (r *SessionRepository) RotateRefreshToken(ctx context.Context, id uint, oldRefresh, newAccess, newRefresh string, rotatedAt, expiresAt time.Time) (bool, error) {
	res := r.db.WithContext(ctx).
		Model(&model.Session{}).
		Where("id = ? AND refresh_token = ?", id, oldRefresh).
		Updates(map[string]any{
			"session_token":      newAccess,
			"prev_refresh_token": oldRefresh,
			"refresh_token":      newRefresh,
			"rotated_at":         rotatedAt,
			"expires_at":         expiresAt,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

func (r *SessionRepository) FindByUserID(ctx context.Context, userID uint) ([]model.Session, error) {
	var sessions []model.Session
	if err := r.db.WithContext(ctx).Where("user_id = ?", userID).Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *SessionRepository) FindByBrowserID(ctx context.Context, browserID string) ([]model.Session, error) {
	var sessions []model.Session
	if browserID == "" {
		return sessions, nil
	}
	if err := r.db.WithContext(ctx).
		Where("browser_id = ? AND expires_at > ?", browserID, time.Now()).
		Order("last_used_at DESC NULLS LAST").
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	return sessions, nil
}

func (r *SessionRepository) ExtendBrowserSSO(ctx context.Context, userID uint, browserID string, now time.Time, idle, max time.Duration) (int64, error) {
	if browserID == "" {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET expires_at = GREATEST(expires_at,
		    LEAST(?::timestamptz, COALESCE(auth_time, created_at) + (? * interval '1 second')))
		WHERE client_id = '' AND browser_id = ? AND user_id = ? AND expires_at > ?`,
		now.Add(idle), int64(max.Seconds()), browserID, userID, now)
	return res.RowsAffected, res.Error
}

func (r *SessionRepository) ExtendSSO(ctx context.Context, id uint, now time.Time, idle, max time.Duration) error {
	return r.db.WithContext(ctx).Exec(`
		UPDATE sessions
		SET expires_at = GREATEST(expires_at,
		    LEAST(?::timestamptz, COALESCE(auth_time, created_at) + (? * interval '1 second'))),
		    last_used_at = ?
		WHERE id = ? AND client_id = '' AND expires_at > ?`,
		now.Add(idle), int64(max.Seconds()), now, id, now).Error
}

func (r *SessionRepository) TouchLastUsed(ctx context.Context, id uint, t time.Time) error {
	return r.db.WithContext(ctx).Model(&model.Session{}).Where("id = ?", id).Update("last_used_at", t).Error
}

func (r *SessionRepository) Create(ctx context.Context, session *model.Session) error {
	return r.db.WithContext(ctx).Create(session).Error
}

func (r *SessionRepository) Update(ctx context.Context, session *model.Session) error {
	return r.db.WithContext(ctx).Save(session).Error
}

func (r *SessionRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&model.Session{}, id).Error
}

func (r *SessionRepository) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).Where("user_id = ?", userID).Delete(&model.Session{}).Error
}

func (r *SessionRepository) DeleteByBrowserID(ctx context.Context, browserID string) error {
	if browserID == "" {
		return nil
	}
	return r.db.WithContext(ctx).Where("browser_id = ?", browserID).Delete(&model.Session{}).Error
}

func (r *SessionRepository) DeleteExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).Where("expires_at < ?", time.Now()).Delete(&model.Session{}).Error
}

func (r *SessionRepository) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&model.Session{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
