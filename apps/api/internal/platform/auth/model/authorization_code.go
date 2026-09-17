package model

import (
	"time"
)

type AuthorizationCode struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	Code                string     `gorm:"size:64;uniqueIndex;not null" json:"code"`
	ClientID            string     `gorm:"size:50;not null;index" json:"client_id"`
	UserID              uint       `gorm:"not null;index" json:"user_id"`
	RedirectURI         string     `gorm:"size:255;not null" json:"redirect_uri"`
	Scope               string     `gorm:"size:255" json:"scope"`
	Nonce               string     `gorm:"size:255" json:"nonce,omitempty"`
	CodeChallenge       string     `gorm:"size:128" json:"code_challenge"`
	CodeChallengeMethod string     `gorm:"size:10" json:"code_challenge_method"`
	ExpiresAt           time.Time  `gorm:"not null" json:"expires_at"`
	UsedAt              *time.Time `json:"used_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`

	User *User `gorm:"foreignKey:UserID" json:"user,omitempty"`
}

func (AuthorizationCode) TableName() string {
	return "authorization_codes"
}

func (c *AuthorizationCode) IsExpired() bool {
	return time.Now().After(c.ExpiresAt)
}

func (c *AuthorizationCode) IsUsed() bool {
	return c.UsedAt != nil
}

func (c *AuthorizationCode) IsValid() bool {
	return !c.IsExpired() && !c.IsUsed()
}
