package model

import (
	"time"
)

type OAuthAccount struct {
	ID                uint      `gorm:"primaryKey" json:"id"`
	UserID            uint      `gorm:"not null;index;uniqueIndex:idx_oauth_accounts_user_provider,priority:1" json:"user_id"`
	Provider          string    `gorm:"size:50;not null;uniqueIndex:idx_oauth_accounts_provider_account,priority:1;uniqueIndex:idx_oauth_accounts_user_provider,priority:2" json:"provider"`
	ProviderAccountID string    `gorm:"size:255;not null;uniqueIndex:idx_oauth_accounts_provider_account,priority:2" json:"provider_account_id"`
	AccessToken       *string   `gorm:"type:text" json:"-"`
	RefreshToken      *string   `gorm:"type:text" json:"-"`
	ExpiresAt         *int64    `json:"expires_at,omitempty"`
	CreatedAt         time.Time `json:"created_at"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (OAuthAccount) TableName() string {
	return "oauth_accounts"
}
