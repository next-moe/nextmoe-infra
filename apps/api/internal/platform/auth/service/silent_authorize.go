package service

import (
	"context"
	"log/slog"
	"time"

	"api/internal/platform/auth/model"
)

// LiveBrowserSession is the account-center sign-in a refresh cookie names, as
// the account frontend's own refresh would accept it, minus the rotation.
func (s *OAuthService) LiveBrowserSession(ctx context.Context, refreshToken string) (*model.Session, *model.User, bool) {
	if refreshToken == "" || s.sessionRepo == nil {
		return nil, nil, false
	}
	session, err := s.sessionRepo.FindByRefreshTokenOrPrev(ctx, refreshToken)
	if err != nil || session.ClientID != "" || session.IsExpired() {
		return nil, nil, false
	}
	if refreshToken != session.RefreshToken && !session.PrevTokenReplayable() {
		return nil, nil, false
	}
	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil || user.IsBanned() || user.IsAnonymized() {
		return nil, nil, false
	}
	return session, user, true
}

// KeepBrowserSessionAlive slides the sign-in the way an account refresh would,
// and returns the refresh cookie's Max-Age for it.
func (s *OAuthService) KeepBrowserSessionAlive(ctx context.Context, session *model.Session) int {
	now := time.Now()
	if err := s.sessionRepo.ExtendSSO(ctx, session.ID, now, ssoSessionIdle(), ssoSessionMax()); err != nil {
		slog.Warn("silent authorize: sso session extension failed",
			"session_id", session.ID, "user_id", session.UserID, "err", err)
	}
	return ssoCookieMaxAge(sessionAuthTime(session), now)
}
