package repository

import (
	"context"

	"api/internal/platform/auth/model"

	"gorm.io/gorm"
)

type OAuthAccountRepository struct {
	db *gorm.DB
}

func NewOAuthAccountRepository(db *gorm.DB) *OAuthAccountRepository {
	return &OAuthAccountRepository{db: db}
}

func (r *OAuthAccountRepository) FindByProviderSubject(ctx context.Context, provider, subject string) (*model.OAuthAccount, error) {
	var acc model.OAuthAccount
	if err := r.db.WithContext(ctx).
		Where("provider = ? AND provider_account_id = ?", provider, subject).
		First(&acc).Error; err != nil {
		return nil, err
	}
	return &acc, nil
}

func (r *OAuthAccountRepository) ExistsByUserAndProvider(ctx context.Context, userID uint, provider string) (bool, error) {
	var n int64
	if err := r.db.WithContext(ctx).Model(&model.OAuthAccount{}).
		Where("user_id = ? AND provider = ?", userID, provider).
		Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *OAuthAccountRepository) Create(ctx context.Context, acc *model.OAuthAccount) error {
	return r.db.WithContext(ctx).Create(acc).Error
}
