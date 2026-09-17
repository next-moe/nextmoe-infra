package model

import (
	"time"
)

type Session struct {
	ID       uint   `gorm:"primaryKey" json:"id"`
	UserID   uint   `gorm:"not null;index" json:"user_id"`
	ClientID string `gorm:"size:50;index;default:''" json:"client_id"`
	// Scope is the OAuth scope granted at code-exchange time. Persisted
	// here so refresh can re-issue access_tokens carrying the SAME scope
	// — without this, a refreshed token would silently lose its scope
	// claim and /oauth/userinfo would treat it as "all fields" (privacy
	// regression: `openid`-only token would upgrade to email+profile
	// access after one refresh).
	Scope        string `gorm:"type:text;default:''" json:"scope"`
	SessionToken string `gorm:"type:text;uniqueIndex;not null" json:"-"`
	RefreshToken string `gorm:"type:text;uniqueIndex;not null" json:"-"`

	PrevRefreshToken string     `gorm:"type:text;index;default:''" json:"-"`
	RotatedAt        *time.Time `json:"-"`

	UserAgent string    `gorm:"type:text;default:''" json:"user_agent"`
	IPAddress string    `gorm:"size:45;default:''" json:"ip_address"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`

	BrowserID  string     `gorm:"size:64;index;default:''" json:"-"`
	AuthTime   *time.Time `json:"-"`
	LastUsedAt *time.Time `json:"-"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (Session) TableName() string {
	return "sessions"
}

const RefreshGraceWindow = 2 * time.Minute

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *Session) PrevTokenWithinGrace() bool {
	return s.RotatedAt != nil && time.Since(*s.RotatedAt) <= RefreshGraceWindow
}
