package model

import (
	"time"
)

type PasswordReset struct {
	ID        uint       `gorm:"primaryKey" json:"id"`
	UserID    uint       `gorm:"not null;index" json:"user_id"`
	Token     string     `gorm:"size:64;uniqueIndex;not null" json:"token"`
	ExpiresAt time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt    *time.Time `json:"used_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (PasswordReset) TableName() string {
	return "password_resets"
}

func (p *PasswordReset) IsExpired() bool {
	return time.Now().After(p.ExpiresAt)
}

func (p *PasswordReset) IsUsed() bool {
	return p.UsedAt != nil
}

func (p *PasswordReset) IsValid() bool {
	return !p.IsExpired() && !p.IsUsed()
}
