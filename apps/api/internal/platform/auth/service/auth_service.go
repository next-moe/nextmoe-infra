package service

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"time"

	"api/internal/infrastructure/cache"
	"api/internal/infrastructure/mail"
	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/settings/keys"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/oidctoken"
	"api/pkg/utils"
)

type emailChangeData struct {
	Code     string `json:"code"`
	NewEmail string `json:"new_email"`
}

func verificationCodeTTL() time.Duration {
	return time.Duration(keys.AuthVerificationCodeTTLMinutes.Get()) * time.Minute
}

type AuthService struct {
	userRepo          *repository.UserRepository
	sessionRepo       *repository.SessionRepository
	passwordResetRepo *repository.PasswordResetRepository
	mailer            *mail.Mailer
	cache             *cache.RedisCache
	cfg               *config.Config
	moemoepointSvc    *MoemoepointService
	signer            oidctoken.Signer
	verifier          *oidctoken.Verifier
}

func (s *AuthService) WithMoemoepoint(mp *MoemoepointService) *AuthService {
	s.moemoepointSvc = mp
	return s
}

func (s *AuthService) WithTokenSigner(signer oidctoken.Signer) *AuthService {
	s.signer = signer
	return s
}

func (s *AuthService) WithTokenVerifier(v *oidctoken.Verifier) *AuthService {
	s.verifier = v
	return s
}

func NewAuthService(
	userRepo *repository.UserRepository,
	sessionRepo *repository.SessionRepository,
	jwtCfg config.JWTConfig,
) *AuthService {
	return &AuthService{
		userRepo:    userRepo,
		sessionRepo: sessionRepo,
		cfg:         &config.Config{JWT: jwtCfg},
	}
}

func NewAuthServiceFull(
	userRepo *repository.UserRepository,
	sessionRepo *repository.SessionRepository,
	passwordResetRepo *repository.PasswordResetRepository,
	mailer *mail.Mailer,
	cache *cache.RedisCache,
	cfg *config.Config,
) *AuthService {
	return &AuthService{
		userRepo:          userRepo,
		sessionRepo:       sessionRepo,
		passwordResetRepo: passwordResetRepo,
		mailer:            mailer,
		cache:             cache,
		cfg:               cfg,
	}
}

func (s *AuthService) SendRegisterCode(ctx context.Context, name, email string) error {
	if err := checkEmailDomainAllowed(email); err != nil {
		return err
	}
	if s.cache == nil {
		return fmt.Errorf("cache not configured")
	}

	nameExists, err := s.userRepo.ExistsByName(ctx, name)
	if err != nil {
		return err
	}
	if nameExists {
		return errors.NewWithCode(errors.ErrAuthNameExists)
	}
	emailExists, err := s.userRepo.ExistsByEmail(ctx, email)
	if err != nil {
		return err
	}
	if emailExists {
		return errors.NewWithCode(errors.ErrAuthEmailExists)
	}

	emailKey := strings.ToLower(strings.TrimSpace(email))
	redisKey := fmt.Sprintf("register_code:%s", emailKey)
	cooldownKey := fmt.Sprintf("register_code_cooldown:%s", emailKey)
	if cd, _ := s.cache.Get(cooldownKey); cd != nil {
		return errors.NewWithCode(errors.ErrAuthEmailChangeTooFrequent)
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}

	ttl := verificationCodeTTL()
	if err := s.cache.Set(redisKey, []byte(code), ttl); err != nil {
		return err
	}
	if err := s.cache.Set(cooldownKey, []byte("1"), time.Duration(keys.AuthVerificationResendCooldownSeconds.Get())*time.Second); err != nil {
		return err
	}

	if s.mailer != nil {
		ttlMinutes := int(ttl.Minutes())
		if err := s.mailer.SendRegisterCodeEmail(email, name, code, ttlMinutes); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}

	return nil
}

func (s *AuthService) Register(ctx context.Context, req *dto.RegisterRequest) (*dto.TokenPair, *model.User, error) {
	if err := checkEmailDomainAllowed(req.Email); err != nil {
		return nil, nil, err
	}
	if s.cache == nil {
		return nil, nil, fmt.Errorf("cache not configured")
	}

	emailKey := strings.ToLower(strings.TrimSpace(req.Email))
	redisKey := fmt.Sprintf("register_code:%s", emailKey)
	storedCode, err := s.cache.Get(redisKey)
	if err != nil || storedCode == nil {
		return nil, nil, errors.NewWithCode(errors.ErrAuthCodeExpired)
	}
	if subtle.ConstantTimeCompare(storedCode, []byte(req.Code)) != 1 {
		return nil, nil, errors.NewWithCode(errors.ErrAuthCodeInvalid)
	}

	exists, err := s.userRepo.ExistsByEmail(ctx, req.Email)
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

	hashedPassword, err := utils.HashPassword(req.Password)
	if err != nil {
		return nil, nil, err
	}

	user := &model.User{
		Name:     req.Name,
		Email:    model.NormalizeEmail(req.Email),
		Password: &hashedPassword,
	}

	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, nil, err
	}

	tokens, err := s.generateTokens(user)
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
		ExpiresAt:    authAt.Add(7 * 24 * time.Hour),
	}
	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, nil, err
	}

	_ = s.cache.Delete(redisKey)

	if s.moemoepointSvc != nil {
		res, gErr := s.moemoepointSvc.Adjust(ctx, AdjustParams{
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

	return tokens, user, nil
}

func (s *AuthService) Login(ctx context.Context, req *dto.LoginRequest) (*dto.TokenPair, *model.User, error) {
	var (
		user *model.User
		err  error
	)
	if strings.Contains(req.Account, "@") {
		user, err = s.userRepo.FindByEmail(ctx, req.Account)
	} else {
		if !utils.IsValidName(req.Account) {
			return nil, nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
		}
		user, err = s.userRepo.FindByName(ctx, req.Account)
	}
	if err != nil {
		return nil, nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	if user.IsBanned() {
		return nil, nil, errors.NewWithCode(errors.ErrAuthUnauthorized)
	}

	if user.IsPasswordSet() {
		ok, err := utils.VerifyPassword(req.Password, *user.Password)
		if err != nil || !ok {
			return nil, nil, errors.NewWithCode(errors.ErrAuthInvalidPassword)
		}
	} else if user.HasLegacyPassword() {
		migrated := false

		if user.KungalPassword != nil && *user.KungalPassword != "" {
			if utils.VerifyBcryptPassword(req.Password, *user.KungalPassword) {
				migrated = true
			}
		}

		if !migrated && user.MoyuPassword != nil && *user.MoyuPassword != "" {
			if utils.VerifyMoyuPassword(req.Password, *user.MoyuPassword) {
				migrated = true
			}
		}

		if !migrated {
			return nil, nil, errors.NewWithCode(errors.ErrAuthInvalidPassword)
		}

		newHash, err := utils.HashPassword(req.Password)
		if err != nil {
			return nil, nil, err
		}
		if err := s.userRepo.MigrateLegacyPassword(ctx, user.ID, newHash); err != nil {
			return nil, nil, err
		}
	} else {
		return nil, nil, errors.NewWithCode(errors.ErrAuthPasswordRequired)
	}

	userWithRoles, err := s.userRepo.FindByIDWithRoles(ctx, user.ID)
	if err == nil {
		user.Roles = userWithRoles.Roles
	}

	tokens, err := s.generateTokens(user)
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
		ExpiresAt:    authAt.Add(7 * 24 * time.Hour),
	}

	if err := s.sessionRepo.Create(ctx, session); err != nil {
		return nil, nil, err
	}

	return tokens, user, nil
}

func (s *AuthService) loadBag(ctx context.Context, browserID, callerUUID, activeRefreshToken string) ([]model.Session, map[uint]*model.User, uint, error) {
	sessions, err := s.sessionRepo.FindByBrowserID(ctx, browserID)
	if err != nil {
		return nil, nil, 0, err
	}

	ids := make([]uint, 0, len(sessions))
	seen := make(map[uint]bool, len(sessions))
	for i := range sessions {
		if id := sessions[i].UserID; !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	users, err := s.userRepo.FindByIDsWithRoles(ctx, ids)
	if err != nil {
		return nil, nil, 0, err
	}
	usersByID := make(map[uint]*model.User, len(users))
	callerInBag := false
	for i := range users {
		usersByID[users[i].ID] = &users[i]
		if users[i].UUID == callerUUID {
			callerInBag = true
		}
	}
	if len(sessions) > 0 && !callerInBag {
		return nil, nil, 0, errors.NewWithCode(errors.ErrAuthUnauthorized)
	}

	var activeUserID uint
	if activeRefreshToken != "" {
		if act, e := s.sessionRepo.FindByRefreshTokenOrPrev(ctx, activeRefreshToken); e == nil && act != nil {
			activeUserID = act.UserID
		}
	}
	return sessions, usersByID, activeUserID, nil
}

func (s *AuthService) ListBrowserSessions(ctx context.Context, browserID, callerUUID, activeRefreshToken string) ([]dto.SessionBrief, error) {
	sessions, usersByID, activeUserID, err := s.loadBag(ctx, browserID, callerUUID, activeRefreshToken)
	if err != nil {
		return nil, err
	}

	out := make([]dto.SessionBrief, 0, len(usersByID))
	seen := make(map[uint]bool, len(sessions))
	for i := range sessions {
		sess := sessions[i]
		if seen[sess.UserID] {
			continue
		}
		seen[sess.UserID] = true
		u := usersByID[sess.UserID]
		if u == nil {
			continue
		}
		brief := dto.SessionBrief{
			Sub:             u.UUID,
			Name:            u.Name,
			Email:           u.Email,
			Avatar:          u.Avatar,
			AvatarImageHash: u.AvatarImageHash,
			Roles:           u.RoleNames(),
			Active:          sess.UserID == activeUserID,
		}
		if sess.LastUsedAt != nil {
			brief.LastUsedAt = sess.LastUsedAt.UTC().Format("2006-01-02T15:04:05Z")
		}
		out = append(out, brief)
	}
	return out, nil
}

func (s *AuthService) SwitchActiveSession(ctx context.Context, browserID, callerUUID, targetSub string) (*dto.TokenPair, *model.User, error) {
	sessions, usersByID, _, err := s.loadBag(ctx, browserID, callerUUID, "")
	if err != nil {
		return nil, nil, err
	}
	for i := range sessions {
		sess := sessions[i]
		user := usersByID[sess.UserID]
		if user == nil || user.UUID != targetSub {
			continue
		}
		names := user.RoleNames()
		if slices.Contains(names, "admin") || slices.Contains(names, "ren") {
			return nil, nil, errors.NewWithCode(errors.ErrAuthStepUpRequired)
		}
		tokens, e := s.RefreshToken(ctx, sess.RefreshToken)
		if e != nil {
			return nil, nil, e
		}
		_ = s.sessionRepo.TouchLastUsed(ctx, sess.ID, time.Now())
		return tokens, user, nil
	}
	return nil, nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
}

func (s *AuthService) LogoutBrowserAccount(ctx context.Context, browserID, callerUUID, sub, activeRefreshToken string) (removed, clearedActive bool, err error) {
	sessions, usersByID, activeUserID, err := s.loadBag(ctx, browserID, callerUUID, activeRefreshToken)
	if err != nil {
		return false, false, err
	}
	for i := range sessions {
		sess := sessions[i]
		u := usersByID[sess.UserID]
		if u == nil || u.UUID != sub {
			continue
		}
		if e := s.sessionRepo.Delete(ctx, sess.ID); e == nil {
			removed = true
			if sess.UserID == activeUserID {
				clearedActive = true
			}
		}
	}
	return removed, clearedActive, nil
}

func (s *AuthService) LogoutBrowserAll(ctx context.Context, browserID string) error {
	return s.sessionRepo.DeleteByBrowserID(ctx, browserID)
}

func (s *AuthService) Logout(ctx context.Context, sessionToken string) error {
	session, err := s.sessionRepo.FindBySessionToken(ctx, sessionToken)
	if err != nil {
		return nil
	}
	return s.sessionRepo.Delete(ctx, session.ID)
}

func (s *AuthService) RefreshToken(ctx context.Context, refreshToken string) (*dto.TokenPair, error) {
	session, err := s.sessionRepo.FindByRefreshTokenOrPrev(ctx, refreshToken)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	if session.ClientID != "" {
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	if session.IsExpired() {
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, errors.NewWithCode(errors.ErrAuthTokenExpired)
	}

	user, err := s.userRepo.FindByID(ctx, session.UserID)
	if err != nil {
		return nil, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	if user.IsBanned() {
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, errors.NewWithCode(errors.ErrAuthUserBanned)
	}

	tokens, err := s.generateTokens(user)
	if err != nil {
		return nil, err
	}

	if refreshToken != session.RefreshToken {
		if session.PrevTokenWithinGrace() {
			return &dto.TokenPair{
				AccessToken:  tokens.AccessToken,
				RefreshToken: session.RefreshToken,
			}, nil
		}
		_ = s.sessionRepo.Delete(ctx, session.ID)
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	now := time.Now()
	won, err := s.sessionRepo.RotateRefreshToken(
		ctx, session.ID, refreshToken, tokens.AccessToken, tokens.RefreshToken,
		now, now.Add(7*24*time.Hour),
	)
	if err != nil {
		return nil, err
	}
	if won {
		return tokens, nil
	}

	fresh, ferr := s.sessionRepo.FindByID(ctx, session.ID)
	if ferr != nil {
		return nil, errors.NewWithCode(errors.ErrAuthInvalidToken)
	}
	return &dto.TokenPair{
		AccessToken:  tokens.AccessToken,
		RefreshToken: fresh.RefreshToken,
	}, nil
}

func (s *AuthService) GetCurrentUser(ctx context.Context, userUUID string) (*model.User, error) {
	return s.userRepo.FindByUUID(ctx, userUUID)
}

func (s *AuthService) GetCurrentUserWithRoles(ctx context.Context, userUUID string) (*model.User, error) {
	return s.userRepo.FindByUUIDWithRoles(ctx, userUUID)
}

func (s *AuthService) ForgotPassword(ctx context.Context, email string) error {
	user, err := s.userRepo.FindByEmail(ctx, email)
	if err != nil {
		return nil
	}

	if user.IsBanned() {
		return nil
	}

	if s.passwordResetRepo == nil || s.mailer == nil {
		return fmt.Errorf("password reset not configured")
	}

	_ = s.passwordResetRepo.DeleteByUserID(ctx, user.ID)

	token, err := generateSecureToken(32)
	if err != nil {
		return err
	}

	reset := &model.PasswordReset{
		UserID:    user.ID,
		Token:     token,
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}

	if err := s.passwordResetRepo.Create(ctx, reset); err != nil {
		return err
	}

	resetLink := fmt.Sprintf("%s/auth/reset-password?token=%s", s.cfg.Server.FrontendURL, token)
	if err := s.mailer.SendPasswordResetEmail(user.Email, user.Name, resetLink); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	return nil
}

func (s *AuthService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if s.passwordResetRepo == nil {
		return fmt.Errorf("password reset not configured")
	}

	reset, err := s.passwordResetRepo.FindValidByToken(ctx, token)
	if err != nil {
		return errors.NewWithCode(errors.ErrAuthInvalidToken)
	}

	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}

	user, err := s.userRepo.FindByID(ctx, reset.UserID)
	if err != nil {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	if user.IsBanned() {
		return errors.NewWithCode(errors.ErrAuthUserBanned)
	}

	if err := s.passwordResetRepo.MarkAsUsed(ctx, reset.ID); err != nil {
		return err
	}

	if err := s.userRepo.UpdatePassword(ctx, user.UUID, hashedPassword); err != nil {
		return err
	}

	if err := s.sessionRepo.DeleteByUserID(ctx, user.ID); err != nil {
		slog.Warn("reset-password: session purge failed; old sessions may persist",
			"user_id", user.ID, "err", err)
	}

	return nil
}

func (s *AuthService) ChangePassword(ctx context.Context, userUUID string, oldPassword, newPassword string) error {
	user, err := s.userRepo.FindByUUID(ctx, userUUID)
	if err != nil {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	if user.IsPasswordSet() {
		ok, err := utils.VerifyPassword(oldPassword, *user.Password)
		if err != nil || !ok {
			return errors.NewWithCode(errors.ErrAuthInvalidPassword)
		}
	}

	hashedPassword, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.userRepo.UpdatePassword(ctx, userUUID, hashedPassword)
}

func (s *AuthService) ValidateAccessToken(tokenString string) (*utils.TokenClaims, error) {
	if s.verifier != nil {
		return s.verifier.Parse(context.Background(), tokenString)
	}
	claims, err := utils.ParseToken(tokenString, s.cfg.JWT.Secret)
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func (s *AuthService) signAccessToken(claims utils.TokenClaims, ttl time.Duration) (string, error) {
	if s.signer != nil {
		return s.signer.SignAccess(claims, ttl)
	}
	return utils.GenerateAccessToken(s.cfg.JWT.Secret, claims, ttl)
}

func (s *AuthService) generateTokens(user *model.User) (*dto.TokenPair, error) {
	if user.Roles == nil {
		userWithRoles, err := s.userRepo.FindByIDWithRoles(context.Background(), user.ID)
		if err == nil {
			user.Roles = userWithRoles.Roles
		}
	}

	accessToken, err := s.signAccessToken(
		utils.TokenClaims{
			UserUUID: user.UUID,
			ID:       user.ID,
			Email:    user.Email,
			Name:     user.Name,
			Roles:    user.RoleNames(),
		},
		15*time.Minute,
	)
	if err != nil {
		return nil, err
	}

	refreshToken, err := utils.GenerateOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}

	return &dto.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *AuthService) SendEmailChangeCode(ctx context.Context, userUUID, newEmail string) error {
	if err := checkEmailDomainAllowed(newEmail); err != nil {
		return err
	}
	user, err := s.userRepo.FindByUUID(ctx, userUUID)
	if err != nil {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}

	if strings.EqualFold(user.Email, newEmail) {
		return errors.NewWithCode(errors.ErrAuthEmailSameAsCurrent)
	}

	exists, err := s.userRepo.ExistsByEmailExcluding(ctx, newEmail, userUUID)
	if err != nil {
		return err
	}
	if exists {
		return errors.NewWithCode(errors.ErrAuthEmailExists)
	}

	redisKey := fmt.Sprintf("email_change:%s", userUUID)
	cooldownKey := fmt.Sprintf("email_change_cooldown:%s", userUUID)
	if s.cache != nil {
		if cd, _ := s.cache.Get(cooldownKey); cd != nil {
			return errors.NewWithCode(errors.ErrAuthEmailChangeTooFrequent)
		}
	}

	code, err := generateNumericCode(6)
	if err != nil {
		return err
	}

	ttl := verificationCodeTTL()
	if s.cache != nil {
		data, _ := json.Marshal(emailChangeData{Code: code, NewEmail: newEmail})
		if err := s.cache.Set(redisKey, data, ttl); err != nil {
			return err
		}
		if err := s.cache.Set(cooldownKey, []byte("1"), time.Duration(keys.AuthVerificationResendCooldownSeconds.Get())*time.Second); err != nil {
			return err
		}
	}

	if s.mailer != nil {
		ttlMinutes := int(ttl.Minutes())
		if err := s.mailer.SendEmailChangeCodeEmail(user.Email, user.Name, code, ttlMinutes); err != nil {
			return fmt.Errorf("failed to send email: %w", err)
		}
	}

	return nil
}

// nameChangeCost matches the forum's CostChangeUsername (17); charging lives
// here because a username is an OAuth-global attribute and the balance is too.
const nameChangeCost = 17

func (s *AuthService) UpdateProfile(ctx context.Context, userUUID string, req *dto.UpdateProfileRequest) (*model.User, error) {
	fields := map[string]any{}

	if req.Name != nil {
		exists, err := s.userRepo.ExistsByNameExcluding(ctx, *req.Name, userUUID)
		if err != nil {
			return nil, err
		}
		if exists {
			return nil, errors.NewWithCode(errors.ErrAuthNameExists)
		}
		if err := s.userRepo.RenameWithCharge(ctx, userUUID, *req.Name, nameChangeCost); err != nil {
			if err == repository.ErrMoemoepointInsufficient {
				return nil, errors.NewWithCode(errors.ErrMoemoepointInsufficient)
			}
			return nil, err
		}
	}
	if req.Avatar != nil {
		fields["avatar"] = *req.Avatar
	}
	if req.AvatarImageHash != nil {
		fields["avatar_image_hash"] = *req.AvatarImageHash
	}
	if req.Bio != nil {
		fields["bio"] = *req.Bio
	}

	if err := s.userRepo.UpdateProfile(ctx, userUUID, fields); err != nil {
		return nil, err
	}

	return s.userRepo.FindByUUIDWithRoles(ctx, userUUID)
}

func (s *AuthService) ChangeEmail(ctx context.Context, userUUID, code, newEmail string) error {
	if err := checkEmailDomainAllowed(newEmail); err != nil {
		return err
	}
	redisKey := fmt.Sprintf("email_change:%s", userUUID)

	if s.cache == nil {
		return fmt.Errorf("cache not configured")
	}

	raw, err := s.cache.Get(redisKey)
	if err != nil || raw == nil {
		return errors.NewWithCode(errors.ErrAuthCodeExpired)
	}

	var data emailChangeData
	if err := json.Unmarshal(raw, &data); err != nil {
		return errors.NewWithCode(errors.ErrAuthCodeInvalid)
	}

	if subtle.ConstantTimeCompare([]byte(data.Code), []byte(code)) != 1 ||
		!strings.EqualFold(data.NewEmail, newEmail) {
		return errors.NewWithCode(errors.ErrAuthCodeInvalid)
	}

	exists, err := s.userRepo.ExistsByEmailExcluding(ctx, newEmail, userUUID)
	if err != nil {
		return err
	}
	if exists {
		return errors.NewWithCode(errors.ErrAuthEmailExists)
	}

	if err := s.userRepo.UpdateEmail(ctx, userUUID, model.NormalizeEmail(newEmail)); err != nil {
		return err
	}

	_ = s.cache.Delete(redisKey)

	return nil
}

func generateNumericCode(length int) (string, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(length)), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", length, n), nil
}

func generateSecureToken(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
