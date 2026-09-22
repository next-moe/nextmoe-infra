package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	authrepo "api/internal/platform/auth/repository"
	sitemodel "api/internal/platform/site/model"
	siterepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/oidctoken"
	"api/pkg/utils"

	"github.com/golang-jwt/jwt/v5"
)

type OAuthService struct {
	userRepo     *authrepo.UserRepository
	authCodeRepo *authrepo.AuthorizationCodeRepository
	sessionRepo  *authrepo.SessionRepository
	clientRepo   *siterepo.OAuthClientRepository
	siteRoleRepo *authrepo.UserSiteRoleRepository
	cfg          *config.Config
	signer       oidctoken.Signer
	idSigner     *oidctoken.IDSigner
}

func (s *OAuthService) WithTokenSigner(signer oidctoken.Signer) *OAuthService {
	s.signer = signer
	return s
}

func (s *OAuthService) WithIDSigner(idSigner *oidctoken.IDSigner) *OAuthService {
	s.idSigner = idSigner
	return s
}

func (s *OAuthService) signAccessToken(claims utils.TokenClaims, ttl time.Duration) (string, error) {
	if s.signer != nil {
		return s.signer.SignAccess(claims, ttl)
	}
	return utils.GenerateAccessToken(s.cfg.JWT.Secret, claims, ttl)
}

func (s *OAuthService) siteRoles(ctx context.Context, userID, siteID uint) []string {
	if siteID == 0 || s.siteRoleRepo == nil {
		return nil
	}
	names, err := s.siteRoleRepo.ActiveRoleNames(ctx, userID, siteID)
	if err != nil {
		slog.Warn("site_roles lookup failed; issuing token without them",
			"user_id", userID, "site_id", siteID, "err", err)
		return nil
	}
	return names
}

func NewOAuthService(
	userRepo *authrepo.UserRepository,
	authCodeRepo *authrepo.AuthorizationCodeRepository,
	sessionRepo *authrepo.SessionRepository,
	clientRepo *siterepo.OAuthClientRepository,
	siteRoleRepo *authrepo.UserSiteRoleRepository,
	cfg *config.Config,
) *OAuthService {
	return &OAuthService{
		userRepo:     userRepo,
		authCodeRepo: authCodeRepo,
		sessionRepo:  sessionRepo,
		clientRepo:   clientRepo,
		siteRoleRepo: siteRoleRepo,
		cfg:          cfg,
	}
}

type ClientPublicInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	AutoConsent bool   `json:"auto_consent"`
	SiteDomain  string `json:"site_domain"`
	LogoURL     string `json:"logo_url,omitempty"`
	ThirdParty  bool   `json:"third_party"`
}

func (s *OAuthService) GetClientPublicInfo(ctx context.Context, clientID string) (*ClientPublicInfo, error) {
	client, err := s.clientRepo.FindByClientIDWithSite(ctx, clientID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}
	info := &ClientPublicInfo{
		ID:          client.ID,
		Name:        client.Name,
		AutoConsent: client.AutoConsent,
		LogoURL:     client.LogoURL,
		ThirdParty:  client.OwnerUserID != nil,
	}
	if client.Site != nil {
		info.SiteDomain = client.Site.Domain
	}
	return info, nil
}

type EcosystemApp struct {
	Name        string `json:"name"`
	SiteDomain  string `json:"site_domain"`
	LogoURL     string `json:"logo_url,omitempty"`
	Tagline     string `json:"tagline,omitempty"`
	AutoConsent bool   `json:"auto_consent"`
}

func (s *OAuthService) ListEcosystem(ctx context.Context) ([]EcosystemApp, error) {
	clients, err := s.clientRepo.FindListed(ctx)
	if err != nil {
		return nil, err
	}
	apps := make([]EcosystemApp, 0, len(clients))
	for i := range clients {
		c := &clients[i]
		app := EcosystemApp{Name: c.Name, LogoURL: c.LogoURL, Tagline: c.Tagline, AutoConsent: c.AutoConsent}
		if c.Site != nil {
			app.SiteDomain = c.Site.Domain
		}
		apps = append(apps, app)
	}
	return apps, nil
}

func (s *OAuthService) ValidateClient(ctx context.Context, clientID, redirectURI string) (*sitemodel.OAuthClient, error) {
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}

	if !client.HasRedirectURI(redirectURI) {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidRedirectURI)
	}

	if !client.IsActive() {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}

	return client, nil
}

func (s *OAuthService) ValidatePostLogoutRedirect(ctx context.Context, clientID, redirect string) (string, bool) {
	if redirect == "" || clientID == "" {
		return "", false
	}
	target, err := url.Parse(redirect)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return "", false
	}
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return "", false
	}
	var uris []string
	if err := json.Unmarshal(client.RedirectURIs, &uris); err != nil {
		return "", false
	}
	for _, u := range uris {
		if ru, err := url.Parse(u); err == nil && ru.Scheme == target.Scheme && ru.Host == target.Host {
			return redirect, true
		}
	}
	return "", false
}

func (s *OAuthService) CreateAuthorizationCode(
	ctx context.Context,
	userID uint,
	clientID string,
	redirectURI string,
	scope string,
	codeChallenge string,
	codeChallengeMethod string,
	nonce string,
) (string, error) {
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return "", errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}
	if _, ok := client.CheckScope(scope); !ok {
		return "", errors.NewWithCode(errors.ErrOAuthInvalidScope)
	}

	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		return "", err
	}
	code := hex.EncodeToString(codeBytes)

	authCode := &model.AuthorizationCode{
		Code:                code,
		ClientID:            clientID,
		UserID:              userID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		Nonce:               nonce,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           time.Now().Add(10 * time.Minute),
	}

	if err := s.authCodeRepo.Create(ctx, authCode); err != nil {
		return "", err
	}

	return code, nil
}

func (s *OAuthService) verifyClientSecret(ctx context.Context, clientID, clientSecret string) error {
	client, err := s.clientRepo.FindByClientID(ctx, clientID)
	if err != nil {
		return errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}
	if !client.VerifySecret(clientSecret) {
		return errors.NewWithCode(errors.ErrOAuthInvalidClientSecret)
	}
	return nil
}

func (s *OAuthService) ExchangeCode(ctx context.Context, req *dto.TokenRequest) (*dto.TokenResponse, error) {
	client, err := s.clientRepo.FindByClientIDWithSite(ctx, req.ClientID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}

	if !client.IsPublic {
		if req.ClientSecret == "" {
			return nil, errors.NewWithCode(errors.ErrOAuthInvalidClientSecret)
		}
		if err := s.verifyClientSecret(ctx, req.ClientID, req.ClientSecret); err != nil {
			return nil, err
		}
	}

	if !client.IsGrantAllowed("authorization_code") {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidGrant)
	}

	authCode, err := s.authCodeRepo.FindValidByCode(ctx, req.Code)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidCode)
	}

	if authCode.ClientID != req.ClientID {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}

	if authCode.RedirectURI != req.RedirectURI {
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidRedirectURI)
	}

	hasPKCE := authCode.CodeChallenge != ""
	if client.IsPublic && !hasPKCE {
		return nil, errors.NewWithCode(errors.ErrOAuthPKCERequired)
	}

	if hasPKCE {
		if !s.verifyCodeVerifier(req.CodeVerifier, authCode.CodeChallenge, authCode.CodeChallengeMethod) {
			return nil, errors.NewWithCode(errors.ErrOAuthInvalidCodeVerifier)
		}
	}

	if err := s.authCodeRepo.MarkAsUsed(ctx, authCode.ID); err != nil {
		if stderrors.Is(err, authrepo.ErrCodeAlreadyUsed) {
			return nil, errors.NewWithCode(errors.ErrOAuthInvalidCode)
		}
		return nil, err
	}

	user, err := s.userRepo.FindByIDWithRoles(ctx, authCode.UserID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	if user.IsBanned() {
		return nil, errors.NewWithCode(errors.ErrAuthUserBanned)
	}

	// SiteID binds this JWT to the client's site. image_service's
	// middleware reads claims.SiteID to enforce that a kungal user's
	// token cannot drive uploads against moyu's quota (or vice versa).
	// Without writing SiteID here the check in image/middleware/auth.go
	// silently degrades to "always allow".
	var siteID uint
	if client.SiteID != nil {
		siteID = *client.SiteID
	}

	accessToken, err := s.signAccessToken(
		utils.TokenClaims{
			UserUUID:  user.UUID,
			ID:        user.ID,
			Email:     EmailForScope(authCode.Scope, user.Email),
			Name:      user.Name,
			Roles:     user.RoleNames(),
			SiteRoles: s.siteRoles(ctx, user.ID, siteID),
			Scope:     authCode.Scope,
			SiteID:    siteID,
			ClientID:  req.ClientID,
			RegisteredClaims: jwt.RegisteredClaims{
				Audience: clientAudience(client),
			},
		},
		15*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	refreshTokenTTL := client.RefreshTokenTTL()
	refreshToken, err := utils.GenerateOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}

	session := &model.Session{
		UserID:       user.ID,
		ClientID:     req.ClientID,
		Scope:        authCode.Scope,
		SessionToken: accessToken,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(refreshTokenTTL),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, err
	}

	resp := &dto.TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    900,
		RefreshToken: refreshToken,
		Scope:        authCode.Scope,
	}

	if s.idSigner != nil && parseScopes(authCode.Scope)["openid"] {
		if idt, err := s.idSigner.Sign(user.UUID, req.ClientID, authCode.Nonce, 15*time.Minute); err != nil {
			slog.Warn("id_token sign failed", "client_id", req.ClientID, "err", err)
		} else {
			resp.IDToken = idt
		}
	}
	return resp, nil
}

func (s *OAuthService) GetUserInfo(ctx context.Context, userUUID, scope string, siteID uint) (*dto.UserInfoResponse, error) {
	user, err := s.userRepo.FindByUUIDWithRoles(ctx, userUUID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	roles := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		roles = append(roles, r.Name)
	}

	info := &dto.UserInfoResponse{
		ID:        user.ID,
		Sub:       user.UUID,
		Roles:     roles,
		SiteRoles: s.siteRoles(ctx, user.ID, siteID),
		UpdatedAt: user.UpdatedAt.Unix(),
	}

	if ScopeGrants(scope, "profile") {
		info.Name = user.Name
		info.Picture = s.resolveAvatar(user)
		confirmed := user.AdultConfirmedAt != nil
		info.AdultConfirmed = &confirmed
		info.NSFWDisplay = user.NSFWDisplay
	}
	info.Email = EmailForScope(scope, user.Email)

	return info, nil
}

func (s *OAuthService) resolveAvatar(u *model.User) string {
	if u.AvatarImageHash != nil && len(*u.AvatarImageHash) >= 4 {
		h := *u.AvatarImageHash
		base := strings.TrimRight(s.cfg.ImageService.CDNBase, "/")
		return fmt.Sprintf("%s/%s/%s/%s.webp", base, h[0:2], h[2:4], h)
	}
	return u.Avatar
}

func clientAudience(client *sitemodel.OAuthClient) jwt.ClaimStrings {
	if client.Site != nil && client.Site.Domain != "" {
		return jwt.ClaimStrings{client.Site.Domain}
	}
	return nil
}

func parseScopes(scope string) map[string]bool {
	result := make(map[string]bool)
	for _, s := range strings.Fields(scope) {
		result[s] = true
	}
	return result
}

func ScopeGrants(scope, want string) bool {
	if strings.TrimSpace(scope) == "" {
		return true
	}
	return parseScopes(scope)[want]
}

// ScopeHolds is ScopeGrants without the empty-scope amnesty, for scopes that
// were never part of the implicit grant. ScopeGrants reads an empty scope as
// "everything" because first-party session tokens never negotiated one — but
// /oauth/authorize also accepts an empty scope, so an OAuth client that asks
// for nothing would inherit every later-added scope for free.
func ScopeHolds(scope, want string) bool {
	return parseScopes(scope)[want]
}

func EmailForScope(scope, email string) string {
	if ScopeGrants(scope, "email") {
		return email
	}
	return ""
}

func (s *OAuthService) RevokeToken(ctx context.Context, token, hint string) error {
	lookupBySession := func() (uint, bool) {
		if hint == "access_token" {
			if sess, err := s.sessionRepo.FindBySessionToken(ctx, token); err == nil {
				return sess.ID, true
			}
			if sess, err := s.sessionRepo.FindByRefreshToken(ctx, token); err == nil {
				return sess.ID, true
			}
			return 0, false
		}
		if sess, err := s.sessionRepo.FindByRefreshToken(ctx, token); err == nil {
			return sess.ID, true
		}
		if sess, err := s.sessionRepo.FindBySessionToken(ctx, token); err == nil {
			return sess.ID, true
		}
		return 0, false
	}

	sessionID, found := lookupBySession()
	if !found {
		return nil
	}
	return s.sessionRepo.Delete(ctx, sessionID)
}

func (s *OAuthService) GetUserIDByUUID(ctx context.Context, uuid string) (uint, error) {
	user, err := s.userRepo.FindByUUID(ctx, uuid)
	if err != nil {
		return 0, err
	}
	return user.ID, nil
}

func (s *OAuthService) RefreshWithClient(ctx context.Context, refreshToken, clientID, clientSecret string) (*dto.TokenResponse, error) {
	client, err := s.clientRepo.FindByClientIDWithSite(ctx, clientID)
	if err != nil {
		slog.Warn("oauth refresh reject", "stage", "client_lookup", "client_id", clientID, "err", err)
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidClient)
	}

	if !client.IsGrantAllowed("refresh_token") {
		slog.Warn("oauth refresh reject", "stage", "grant_not_allowed", "client_id", clientID, "grants", client.Grants)
		return nil, errors.NewWithCode(errors.ErrOAuthInvalidGrant)
	}

	if client.IsPublic {
	} else {
		if clientSecret == "" {
			slog.Warn("oauth refresh reject", "stage", "missing_secret", "client_id", clientID)
			return nil, errors.NewWithCode(errors.ErrOAuthInvalidClientSecret)
		}
		if err := s.verifyClientSecret(ctx, clientID, clientSecret); err != nil {
			slog.Warn("oauth refresh reject", "stage", "bad_secret", "client_id", clientID)
			return nil, err
		}
	}

	session, err := s.sessionRepo.FindByRefreshTokenOrPrev(ctx, refreshToken)
	if err != nil {
		slog.Warn("oauth refresh reject", "stage", "session_not_found",
			"client_id", clientID, "refresh_token_fp", tokenFingerprint(refreshToken), "err", err)
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	if session.ClientID != clientID {
		slog.Warn("oauth refresh reject", "stage", "client_id_mismatch",
			"request_client_id", clientID, "session_client_id", session.ClientID,
			"session_id", session.ID, "user_id", session.UserID)
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	if session.IsExpired() {
		slog.Warn("oauth refresh reject", "stage", "session_expired",
			"session_id", session.ID, "user_id", session.UserID, "expires_at", session.ExpiresAt)
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, errors.NewWithCode(errors.ErrAuthTokenExpired)
	}

	user, err := s.userRepo.FindByIDWithRoles(ctx, session.UserID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	if user.IsBanned() {
		return nil, errors.NewWithCode(errors.ErrAuthUserBanned)
	}

	var siteID uint
	if client.SiteID != nil {
		siteID = *client.SiteID
	}

	freshAccess := func() (string, error) {
		return s.signAccessToken(
			utils.TokenClaims{
				UserUUID:  user.UUID,
				ID:        user.ID,
				Email:     EmailForScope(session.Scope, user.Email),
				Name:      user.Name,
				Roles:     user.RoleNames(),
				SiteRoles: s.siteRoles(ctx, user.ID, siteID),
				Scope:     session.Scope,
				SiteID:    siteID,
				ClientID:  clientID,
				RegisteredClaims: jwt.RegisteredClaims{
					Audience: clientAudience(client),
				},
			},
			15*time.Minute,
		)
	}

	freshIDToken := func() string {
		if s.idSigner == nil || !parseScopes(session.Scope)["openid"] {
			return ""
		}
		idt, err := s.idSigner.Sign(user.UUID, clientID, "", 15*time.Minute)
		if err != nil {
			slog.Warn("id_token sign failed on refresh", "client_id", clientID, "err", err)
			return ""
		}
		return idt
	}

	if refreshToken != session.RefreshToken {
		if session.PrevTokenWithinGrace() {
			accessToken, gerr := freshAccess()
			if gerr != nil {
				return nil, gerr
			}
			slog.Info("oauth refresh grace-replay",
				"client_id", clientID, "session_id", session.ID, "user_id", session.UserID,
				"presented_fp", tokenFingerprint(refreshToken))
			return &dto.TokenResponse{
				AccessToken:  accessToken,
				TokenType:    "Bearer",
				ExpiresIn:    900,
				RefreshToken: session.RefreshToken,
				Scope:        session.Scope,
				IDToken:      freshIDToken(),
			}, nil
		}
		slog.Warn("oauth refresh reuse-detected; revoking session",
			"client_id", clientID, "session_id", session.ID, "user_id", session.UserID,
			"presented_fp", tokenFingerprint(refreshToken),
			"rotated_at", session.RotatedAt)
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	accessToken, err := freshAccess()
	if err != nil {
		return nil, err
	}

	refreshTokenTTL := client.RefreshTokenTTL()
	newRefreshToken, err := utils.GenerateOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	won, err := s.sessionRepo.RotateRefreshToken(
		ctx, session.ID, refreshToken, accessToken, newRefreshToken,
		now, now.Add(refreshTokenTTL),
	)
	if err != nil {
		slog.Error("oauth refresh reject", "stage", "session_update_failed",
			"session_id", session.ID, "user_id", session.UserID, "err", err)
		return nil, err
	}
	if !won {
		fresh, ferr := s.sessionRepo.FindByID(ctx, session.ID)
		if ferr != nil {
			return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
		}
		slog.Info("oauth refresh rotation-race converged",
			"client_id", clientID, "session_id", session.ID, "user_id", session.UserID,
			"presented_fp", tokenFingerprint(refreshToken))
		return &dto.TokenResponse{
			AccessToken:  accessToken,
			TokenType:    "Bearer",
			ExpiresIn:    900,
			RefreshToken: fresh.RefreshToken,
			Scope:        session.Scope,
			IDToken:      freshIDToken(),
		}, nil
	}

	slog.Debug("oauth refresh ok",
		"client_id", clientID, "session_id", session.ID, "user_id", session.UserID,
		"old_rt_fp", tokenFingerprint(refreshToken),
		"new_rt_fp", tokenFingerprint(newRefreshToken),
	)

	return &dto.TokenResponse{
		AccessToken:  accessToken,
		TokenType:    "Bearer",
		ExpiresIn:    900,
		RefreshToken: newRefreshToken,
		Scope:        session.Scope,
		IDToken:      freshIDToken(),
	}, nil
}

func tokenFingerprint(tok string) string {
	if tok == "" {
		return "(empty)"
	}
	n := len(tok)
	prefix := tok
	if n > 8 {
		prefix = tok[:8]
	}
	return prefix + "..len=" + strconv.Itoa(n)
}

func (s *OAuthService) verifyCodeVerifier(verifier, challenge, method string) bool {
	if method != "" && method != "S256" {
		return false
	}
	h := sha256.Sum256([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(h[:])
	return computed == challenge
}
