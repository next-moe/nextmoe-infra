package repository

import (
	"context"
	"errors"
	"time"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

type AuthorizationCodeRepository struct {
	db *gorm.DB
}

func NewAuthorizationCodeRepository(db *gorm.DB) *AuthorizationCodeRepository {
	return &AuthorizationCodeRepository{db: db}
}

func (r *AuthorizationCodeRepository) Create(ctx context.Context, code *model.AuthorizationCode) error {
	return r.db.WithContext(ctx).Create(code).Error
}

func (r *AuthorizationCodeRepository) FindValidByCode(ctx context.Context, code string) (*model.AuthorizationCode, error) {
	var authCode model.AuthorizationCode
	if err := r.db.WithContext(ctx).
		Where("code = ?", code).
		Where("expires_at > ?", time.Now()).
		Where("used_at IS NULL").
		First(&authCode).Error; err != nil {
		return nil, err
	}
	return &authCode, nil
}

var ErrCodeAlreadyUsed = errors.New("authorization code already used")

func (r *AuthorizationCodeRepository) MarkAsUsed(ctx context.Context, id uint) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&model.AuthorizationCode{}).
		Where("id = ? AND used_at IS NULL", id).
		Update("used_at", now)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCodeAlreadyUsed
	}
	return nil
}

func (r *AuthorizationCodeRepository) DeleteExpired(ctx context.Context) error {
	return r.db.WithContext(ctx).
		Where("expires_at < ?", time.Now()).
		Delete(&model.AuthorizationCode{}).Error
}
