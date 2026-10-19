package service

import (
	"time"

	"api/internal/platform/auth/model"
	"api/internal/platform/settings/keys"
)

func ssoSessionIdle() time.Duration {
	return time.Duration(keys.AuthSSOSessionIdleDays.Get()) * 24 * time.Hour
}

func ssoSessionMax() time.Duration {
	return time.Duration(keys.AuthSSOSessionMaxDays.Get()) * 24 * time.Hour
}

func ssoSessionExpiry(authAt, now time.Time) time.Time {
	idle := now.Add(ssoSessionIdle())
	if deadline := authAt.Add(ssoSessionMax()); idle.After(deadline) {
		return deadline
	}
	return idle
}

func ssoCookieMaxAge(authAt, now time.Time) int {
	secs := int(authAt.Add(ssoSessionMax()).Sub(now).Seconds())
	if secs < 1 {
		return 1
	}
	return secs
}

func sessionAuthTime(s *model.Session) time.Time {
	if s.AuthTime != nil {
		return *s.AuthTime
	}
	return s.CreatedAt
}

// SSOCookieMaxAgeFallback is the refresh-cookie lifetime for a token pair that
// does not carry its own: the full absolute cap.
func SSOCookieMaxAgeFallback() int {
	return int(ssoSessionMax().Seconds())
}
