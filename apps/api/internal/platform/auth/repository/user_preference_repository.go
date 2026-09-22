package repository

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

type UserPreferenceRepository struct {
	db *gorm.DB
}

func NewUserPreferenceRepository(db *gorm.DB) *UserPreferenceRepository {
	return &UserPreferenceRepository{db: db}
}

type PreferenceSummary struct {
	Namespace string    `gorm:"column:namespace"`
	Version   int       `gorm:"column:version"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
	SizeBytes int64     `gorm:"column:size_bytes"`
}

type PreferenceWrite struct {
	Version   int       `gorm:"column:version"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

var ErrPreferenceVersionMismatch = errors.New("user preference version mismatch")

func (r *UserPreferenceRepository) List(ctx context.Context, userID uint) ([]PreferenceSummary, error) {
	out := make([]PreferenceSummary, 0)
	err := r.db.WithContext(ctx).
		Model(&model.UserPreference{}).
		Select("namespace, version, updated_at, octet_length(doc::text) AS size_bytes").
		Where("user_id = ?", userID).
		Order("namespace").
		Scan(&out).Error
	return out, err
}

func (r *UserPreferenceRepository) Get(ctx context.Context, userID uint, namespace string) (*model.UserPreference, error) {
	var pref model.UserPreference
	err := r.db.WithContext(ctx).
		Where("user_id = ? AND namespace = ?", userID, namespace).
		First(&pref).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pref, nil
}

func (r *UserPreferenceRepository) Put(ctx context.Context, userID uint, namespace string, doc []byte) (*PreferenceWrite, error) {
	var w PreferenceWrite
	err := r.db.WithContext(ctx).Raw(`
		INSERT INTO user_preferences (user_id, namespace, doc, version, created_at, updated_at)
		VALUES (?, ?, ?, 1, now(), now())
		ON CONFLICT (user_id, namespace) DO UPDATE
		SET doc = EXCLUDED.doc,
		    version = user_preferences.version + 1,
		    updated_at = now()
		RETURNING version, updated_at
	`, userID, namespace, string(doc)).Scan(&w).Error
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// PutIfVersion is the If-Match half. expected == 0 means "no row must exist
// yet", which is a separate statement rather than a WHERE clause: an UPDATE
// matching nothing and an absent row are the same empty result, so the insert
// has to be the one that reports the conflict.
func (r *UserPreferenceRepository) PutIfVersion(ctx context.Context, userID uint, namespace string, doc []byte, expected int) (*PreferenceWrite, error) {
	var w PreferenceWrite
	var err error
	if expected == 0 {
		err = r.db.WithContext(ctx).Raw(`
			INSERT INTO user_preferences (user_id, namespace, doc, version, created_at, updated_at)
			VALUES (?, ?, ?, 1, now(), now())
			ON CONFLICT (user_id, namespace) DO NOTHING
			RETURNING version, updated_at
		`, userID, namespace, string(doc)).Scan(&w).Error
	} else {
		err = r.db.WithContext(ctx).Raw(`
			UPDATE user_preferences
			SET doc = ?, version = version + 1, updated_at = now()
			WHERE user_id = ? AND namespace = ? AND version = ?
			RETURNING version, updated_at
		`, string(doc), userID, namespace, expected).Scan(&w).Error
	}
	if err != nil {
		return nil, err
	}
	if w.Version == 0 {
		return nil, ErrPreferenceVersionMismatch
	}
	return &w, nil
}

func (r *UserPreferenceRepository) Delete(ctx context.Context, userID uint, namespace string) error {
	return r.db.WithContext(ctx).
		Where("user_id = ? AND namespace = ?", userID, namespace).
		Delete(&model.UserPreference{}).Error
}
