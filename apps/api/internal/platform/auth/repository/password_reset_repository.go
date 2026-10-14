package repository

import (
	"context"
	"time"

	"api/internal/platform/auth/model"
	"api/pkg/errors"

	"gorm.io/gorm"
)

type PasswordResetRepository struct {
	db *gorm.DB
}

func NewPasswordResetRepository(db *gorm.DB) *PasswordResetRepository {
	return &PasswordResetRepository{db: db}
}

func (r *PasswordResetRepository) Create(ctx context.Context, reset *model.PasswordReset) error {
	return r.db.WithContext(ctx).Create(reset).Error
}

func (r *PasswordResetRepository) FindValidByToken(ctx context.Context, token string) (*model.PasswordReset, error) {
	var reset model.PasswordReset
	if err := r.db.WithContext(ctx).
		Where("token = ?", token).
		Where("expires_at > ?", time.Now()).
		Where("used_at IS NULL").
		First(&reset).Error; err != nil {
		return nil, err
	}
	return &reset, nil
}

func (r *PasswordResetRepository) MarkAsUsed(ctx context.Context, id uint) error {
	res := r.db.WithContext(ctx).Model(&model.PasswordReset{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.NewWithCode(errors.ErrAuthInvalidToken)
	}
	return nil
}

func (r *PasswordResetRepository) DeleteByUserID(ctx context.Context, userID uint) error {
	return r.db.WithContext(ctx).
		Where("user_id = ?", userID).
		Delete(&model.PasswordReset{}).Error
}
