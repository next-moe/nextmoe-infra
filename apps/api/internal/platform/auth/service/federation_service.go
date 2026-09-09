package service

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"api/internal/infrastructure/cache"
	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/federation"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings/keys"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/utils"

	"gorm.io/gorm"
)

const (
	CallbackOutcomeLogin   = "login"
	CallbackOutcomePending = "pending"

	federationStateTTL   = 10 * time.Minute
	federationPendingTTL = 30 * time.Minute
	federationSessionTTL = 7 * 24 * time.Hour

	federationStateKeyPrefix   = "federation_state:"
	federationPendingKeyPrefix = "federation_pending:"
)

type federationKV interface {
	Set(key string, value []byte, expiration time.Duration) error
	Get(key string) ([]byte, error)
	Delete(key string) error
}

type SessionMeta struct {
	UserAgent string
	IPAddress string
	BrowserID string
}

type CallbackResult struct {
	Outcome      string
	Tokens       *dto.TokenPair
	PendingToken string
	RedirectTo   string
	RawRedirect  string
}

type PendingInfo struct {
	Provider      string
	SuggestedName string
	Email         string
	EmailLocked   bool
}

type callbackError struct {
	redirectCode string
	rawRedirect  string
}

func (e *callbackError) Error() string { return e.redirectCode }

func CallbackRedirectCode(err error) string {
	var ce *callbackError
	if stderrors.As(err, &ce) {
		return ce.redirectCode
	}
	return ""
}

func CallbackRawRedirect(err error) string {
	var ce *callbackError
	if stderrors.As(err, &ce) {
		return ce.rawRedirect
	}
	return ""
}

type federationState struct {
	Provider string `json:"provider"`
	Nonce    string `json:"nonce"`
	Redirect string `json:"redirect"`
}

type federationPending struct {
	Provider      string `json:"provider"`
	Subject       string `json:"subject"`
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	AvatarURL     string `json:"avatar_url"`
}

type FederationService struct {
	authSvc          *AuthService
	userRepo         *repository.UserRepository
	oauthAccountRepo *repository.OAuthAccountRepository
	sessionRepo      *repository.SessionRepository
	cache            *cache.RedisCache
	cfg              *config.Config
	registry         *federation.Registry
	kv               federationKV
}

func NewFederationService(
	authSvc *AuthService,
	userRepo *repository.UserRepository,
	oauthAccountRepo *repository.OAuthAccountRepository,
	sessionRepo *repository.SessionRepository,
	cache *cache.RedisCache,
	cfg *config.Config,
	registry *federation.Registry,
) *FederationService {
	s := &FederationService{
		authSvc:          authSvc,
		userRepo:         userRepo,
		oauthAccountRepo: oauthAccountRepo,
		sessionRepo:      sessionRepo,
		cache:            cache,
		cfg:              cfg,
		registry:         registry,
	}
	if cache != nil {
		s.kv = cache
	}
	return s
}

func (s *FederationService) EnabledProviders() []string {
	if s.registry == nil {
		return []string{}
	}
	names := s.registry.Enabled()
	if names == nil {
		return []string{}
	}
	return names
}

func (s *FederationService) Start(ctx context.Context, providerName, redirect string) (authorizeURL, state string, err error) {
	if !s.providerEnabled(providerName) {
		return "", "", errors.NewWithCode(errors.ErrAuthFederationDisabled)
	}
	provider, ok := s.registry.Get(providerName)
	if !ok {
		return "", "", errors.NewWithCode(errors.ErrAuthFederationDisabled)
	}

	state, err = generateSecureToken(32)
	if err != nil {
		return "", "", err
	}
	nonce, err := generateSecureToken(32)
	if err != nil {
		return "", "", err
	}

	payload, err := json.Marshal(federationState{
		Provider: providerName,
		Nonce:    nonce,
		Redirect: redirect,
	})
	if err != nil {
		return "", "", err
	}
	if err := s.kvSet(federationStateKeyPrefix+state, payload, federationStateTTL); err != nil {
		return "", "", err
	}

	redirectURI := s.callbackURI(providerName)
	return provider.AuthorizeURL(state, nonce, redirectURI), state, nil
}

func (s *FederationService) Callback(ctx context.Context, providerName, code, state, cookieState string, meta SessionMeta) (*CallbackResult, error) {
	if state == "" || state != cookieState {
		if state != "" {
			_, _ = s.consumeState(state)
		}
		return nil, &callbackError{redirectCode: "federation_state"}
	}

	st, err := s.consumeState(state)
	if err != nil || st == nil || st.Provider != providerName {
		raw := ""
		if st != nil {
			raw = st.Redirect
		}
		return nil, &callbackError{redirectCode: "federation_state", rawRedirect: raw}
	}
	rawRedirect := st.Redirect
	redirectTo := resolveFederationRedirect(s.cfg, rawRedirect)

	provider, ok := s.registry.Get(providerName)
	if !ok {
		return nil, &callbackError{redirectCode: "federation_failed", rawRedirect: rawRedirect}
	}

	ident, err := provider.Exchange(ctx, code, s.callbackURI(providerName), st.Nonce)
	if err != nil || ident == nil {
		return nil, &callbackError{redirectCode: "federation_failed", rawRedirect: rawRedirect}
	}

	result, err := s.callbackIdentity(ctx, providerName, ident, meta, redirectTo, rawRedirect)
	if err != nil {
		if CallbackRedirectCode(err) != "" {
			return nil, err
		}
		slog.Warn("federation callback", "provider", providerName, "err", err)
		return nil, &callbackError{redirectCode: "federation_failed", rawRedirect: rawRedirect}
	}
	return result, nil
}

func (s *FederationService) callbackIdentity(ctx context.Context, providerName string, ident *federation.Identity, meta SessionMeta, redirectTo, rawRedirect string) (*CallbackResult, error) {
	fail := func(code string) error {
		return &callbackError{redirectCode: code, rawRedirect: rawRedirect}
	}

	acc, err := s.oauthAccountRepo.FindByProviderSubject(ctx, providerName, ident.Subject)
	if err == nil {
		user, uerr := s.userRepo.FindByIDWithRoles(ctx, acc.UserID)
		if uerr != nil {
			return nil, uerr
		}
		if user.IsBanned() {
			return nil, fail("federation_banned")
		}
		if hasAdminOrRen(user) {
			return nil, fail("federation_stepup")
		}
		tokens, _, merr := s.mintSession(ctx, user, meta)
		if merr != nil {
			return nil, merr
		}
		return &CallbackResult{Outcome: CallbackOutcomeLogin, Tokens: tokens, RedirectTo: redirectTo, RawRedirect: rawRedirect}, nil
	}
	if !stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	if ident.Email != "" && ident.EmailVerified {
		user, ferr := s.userRepo.FindByEmail(ctx, model.NormalizeEmail(ident.Email))
		if ferr == nil {
			user, rerr := s.userRepo.FindByIDWithRoles(ctx, user.ID)
			if rerr != nil {
				return nil, rerr
			}
			if user.IsBanned() {
				return nil, fail("federation_banned")
			}
			if hasAdminOrRen(user) {
				return nil, fail("federation_stepup")
			}
			exists, eerr := s.oauthAccountRepo.ExistsByUserAndProvider(ctx, user.ID, providerName)
			if eerr != nil {
				return nil, eerr
			}
			if exists {
				return nil, fail("federation_conflict")
			}
			if lerr := s.createLink(ctx, user.ID, providerName, ident.Subject); lerr != nil {
				if errors.Is(lerr, errors.ErrAuthFederationConflict) {
					return nil, fail("federation_conflict")
				}
				return nil, lerr
			}
			tokens, _, merr := s.mintSession(ctx, user, meta)
			if merr != nil {
				return nil, merr
			}
			return &CallbackResult{Outcome: CallbackOutcomeLogin, Tokens: tokens, RedirectTo: redirectTo, RawRedirect: rawRedirect}, nil
		}
		if !stderrors.Is(ferr, gorm.ErrRecordNotFound) {
			return nil, ferr
		}
	}

	token, err := generateSecureToken(32)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(federationPending{
		Provider:      providerName,
		Subject:       ident.Subject,
		Email:         ident.Email,
		EmailVerified: ident.EmailVerified,
		Name:          ident.Name,
		AvatarURL:     ident.AvatarURL,
	})
	if err != nil {
		return nil, err
	}
	if err := s.kvSet(federationPendingKeyPrefix+token, payload, federationPendingTTL); err != nil {
		return nil, err
	}
	return &CallbackResult{
		Outcome:      CallbackOutcomePending,
		PendingToken: token,
		RedirectTo:   redirectTo,
		RawRedirect:  rawRedirect,
	}, nil
}

func (s *FederationService) Pending(ctx context.Context, token string) (*PendingInfo, error) {
	pending, err := s.loadPending(token)
	if err != nil {
		return nil, err
	}
	return &PendingInfo{
		Provider:      pending.Provider,
		SuggestedName: pending.Name,
		Email:         pending.Email,
		EmailLocked:   pendingEmailLocked(pending),
	}, nil
}

func (s *FederationService) Complete(ctx context.Context, req *dto.FederationCompleteRequest) (*dto.TokenPair, *model.User, error) {
	pending, err := s.loadPending(req.Token)
	if err != nil {
		return nil, nil, err
	}
	if !s.providerEnabled(pending.Provider) {
		return nil, nil, errors.NewWithCode(errors.ErrAuthFederationDisabled)
	}

	email := pending.Email
	locked := pendingEmailLocked(pending)
	if !locked {
		if err := checkEmailDomainAllowed(req.Email); err != nil {
			return nil, nil, err
		}
		if s.kv == nil {
			return nil, nil, fmt.Errorf("cache not configured")
		}
		emailKey := strings.ToLower(strings.TrimSpace(req.Email))
		redisKey := fmt.Sprintf("register_code:%s", emailKey)
		storedCode, gerr := s.kvGet(redisKey)
		if gerr != nil || storedCode == nil {
			return nil, nil, errors.NewWithCode(errors.ErrAuthCodeExpired)
		}
		if subtle.ConstantTimeCompare(storedCode, []byte(req.Code)) != 1 {
			return nil, nil, errors.NewWithCode(errors.ErrAuthCodeInvalid)
		}
		email = req.Email
	}

	exists, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, nil, errors.NewWithCode(errors.ErrAuthEmailExists)
	}
	exists, err = s.userRepo.ExistsByName(ctx, req.Name)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, nil, errors.NewWithCode(errors.ErrAuthNameExists)
	}

	if _, ferr := s.oauthAccountRepo.FindByProviderSubject(ctx, pending.Provider, pending.Subject); ferr == nil {
		return nil, nil, errors.NewWithCode(errors.ErrAuthFederationConflict)
	} else if !stderrors.Is(ferr, gorm.ErrRecordNotFound) {
		return nil, nil, ferr
	}

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, nil, err
	}
	user := &model.User{
		Name:     req.Name,
		Email:    model.NormalizeEmail(email),
		Password: &hashedPassword,
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, nil, err
	}

	if err := s.createLink(ctx, user.ID, pending.Provider, pending.Subject); err != nil {
		return nil, nil, err
	}

	tokens, err := s.authSvc.generateTokens(user)
	if err != nil {
		return nil, nil, err
	}
	authAt := time.Now()
	session := &model.Session{
		UserID:       user.ID,
		SessionToken: tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		UserAgent:    req.UserAgent,
		IPAddress:    req.IPAddress,
		BrowserID:    req.BrowserID,
		AuthTime:     &authAt,
		LastUsedAt:   &authAt,
		ExpiresAt:    authAt.Add(federationSessionTTL),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, nil, err
	}

	if s.authSvc != nil && s.authSvc.moemoepointSvc != nil {
		res, gErr := s.authSvc.moemoepointSvc.Adjust(ctx, AdjustParams{
			UserID:         user.ID,
			Delta:          int(keys.AuthRegisterGiftPoints.Get()),
			Reason:         model.MoemoepointReasonRegisterGift,
			SourceApp:      "oauth",
			IdempotencyKey: fmt.Sprintf("oauth:register_gift:%d", user.ID),
			Note:           "鲲给予你的第一份礼物",
		})
		if gErr != nil {
			slog.Warn("register welcome gift failed (best-effort)", "user_id", user.ID, "err", gErr)
		} else {
			user.Moemoepoint = res.Balance
		}
	}

	_ = s.kvDelete(federationPendingKeyPrefix + req.Token)
	if !locked {
		_ = s.kvDelete(fmt.Sprintf("register_code:%s", strings.ToLower(strings.TrimSpace(email))))
	}

	return tokens, user, nil
}

func (s *FederationService) providerEnabled(name string) bool {
	return slices.Contains(s.EnabledProviders(), name)
}

func (s *FederationService) callbackURI(providerName string) string {
	base := ""
	if s.cfg != nil {
		base = strings.TrimRight(s.cfg.Server.SiteURL, "/")
	}
	return base + "/api/v1/auth/federation/" + providerName + "/callback"
}

func (s *FederationService) loadPending(token string) (*federationPending, error) {
	if token == "" {
		return nil, errors.NewWithCode(errors.ErrAuthFederationExpired)
	}
	raw, err := s.kvGet(federationPendingKeyPrefix + token)
	if err != nil || len(raw) == 0 {
		return nil, errors.NewWithCode(errors.ErrAuthFederationExpired)
	}
	var pending federationPending
	if err := json.Unmarshal(raw, &pending); err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthFederationExpired)
	}
	return &pending, nil
}

func (s *FederationService) consumeState(state string) (*federationState, error) {
	key := federationStateKeyPrefix + state
	raw, err := s.kvGet(key)
	_ = s.kvDelete(key)
	if err != nil || len(raw) == 0 {
		return nil, fmt.Errorf("federation state missing")
	}
	var st federationState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *FederationService) createLink(ctx context.Context, userID uint, provider, subject string) error {
	acc := &model.OAuthAccount{
		UserID:            userID,
		Provider:          provider,
		ProviderAccountID: subject,
	}
	if err := s.oauthAccountRepo.Create(ctx, acc); err != nil {
		if isUniqueViolation(err) {
			return errors.NewWithCode(errors.ErrAuthFederationConflict)
		}
		return err
	}
	return nil
}

func (s *FederationService) mintSession(ctx context.Context, user *model.User, meta SessionMeta) (*dto.TokenPair, *model.User, error) {
	userWithRoles, err := s.userRepo.FindByIDWithRoles(ctx, user.ID)
	if err == nil {
		user.Roles = userWithRoles.Roles
	}
	tokens, err := s.authSvc.generateTokens(user)
	if err != nil {
		return nil, nil, err
	}
	authAt := time.Now()
	session := &model.Session{
		UserID:       user.ID,
		SessionToken: tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		UserAgent:    meta.UserAgent,
		IPAddress:    meta.IPAddress,
		BrowserID:    meta.BrowserID,
		AuthTime:     &authAt,
		LastUsedAt:   &authAt,
		ExpiresAt:    authAt.Add(federationSessionTTL),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, nil, err
	}
	return tokens, user, nil
}

func (s *FederationService) kvSet(key string, value []byte, ttl time.Duration) error {
	if s.kv == nil {
		return cache.ErrCacheDisabled
	}
	return s.kv.Set(key, value, ttl)
}

func (s *FederationService) kvGet(key string) ([]byte, error) {
	if s.kv == nil {
		return nil, cache.ErrCacheDisabled
	}
	return s.kv.Get(key)
}

func (s *FederationService) kvDelete(key string) error {
	if s.kv == nil {
		return nil
	}
	return s.kv.Delete(key)
}

func pendingEmailLocked(p *federationPending) bool {
	return p.EmailVerified && p.Email != "" && checkEmailDomainAllowed(p.Email) == nil
}

func hasAdminOrRen(user *model.User) bool {
	names := user.RoleNames()
	return slices.Contains(names, "admin") || slices.Contains(names, "ren")
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if stderrors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "UNIQUE constraint") ||
		strings.Contains(msg, "idx_oauth_accounts")
}

func resolveFederationRedirect(cfg *config.Config, r string) string {
	frontend := ""
	site := ""
	if cfg != nil {
		frontend = strings.TrimRight(cfg.Server.FrontendURL, "/")
		site = strings.TrimRight(cfg.Server.SiteURL, "/")
	}
	fallback := frontend + "/profile"
	if r == "" {
		return fallback
	}
	if strings.HasPrefix(r, "/") && !strings.HasPrefix(r, "//") {
		return frontend + r
	}
	target, ok := urlOrigin(r)
	if !ok {
		return fallback
	}
	frontOrigin, frontOK := urlOrigin(frontend)
	siteOrigin, siteOK := urlOrigin(site)
	if (frontOK && target == frontOrigin) || (siteOK && target == siteOrigin) {
		return r
	}
	return fallback
}

func urlOrigin(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", false
	}
	return strings.ToLower(u.Scheme) + "://" + strings.ToLower(u.Host), true
}
