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

	// SSOBrowserID is set only on OAuth client sessions: the browser whose
	// account-center sign-in issued the code. It is deliberately not BrowserID,
	// which marks a row as one of that browser's signed-in accounts and is what
	// the account switcher and "log out of this browser" enumerate.
	SSOBrowserID string `gorm:"column:sso_browser_id;size:64;index;default:''" json:"-"`

	// RotationLate marks a rotation that finished after its caller had likely
	// given up (callers time out at 10s). On 2026-10-22 oauth committed
	// rotations 12s into the request, the forum and moyu kept the old token,
	// and presenting it minutes later revoked the session as a stolen-token
	// reuse: 199 sessions were logged out by a slow disk.
	RotationLate bool `gorm:"not null;default:false" json:"-"`

	User User `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE" json:"-"`
}

func (Session) TableName() string {
	return "sessions"
}

const RefreshGraceWindow = 2 * time.Minute

const LateRotationAfter = 5 * time.Second

func (s *Session) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

func (s *Session) PrevTokenWithinGrace() bool {
	return s.RotatedAt != nil && time.Since(*s.RotatedAt) <= RefreshGraceWindow
}

// PrevTokenReplayable reports whether presenting the previous refresh token is
// a lost response rather than a reuse. A late rotation stays replayable until
// the next one: whoever holds the new token has not used it yet, and whoever
// holds the old one most likely never received the new.
func (s *Session) PrevTokenReplayable() bool {
	return s.RotationLate || s.PrevTokenWithinGrace()
}
