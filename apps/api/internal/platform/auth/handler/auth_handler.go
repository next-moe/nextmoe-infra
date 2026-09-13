package handler

import (
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/service"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
)

const refreshTokenCookieName = "refresh_token"

type AuthHandler struct {
	authService *service.AuthService
	cfg         *config.Config
}

func NewAuthHandler(authService *service.AuthService, cfg *config.Config) *AuthHandler {
	return &AuthHandler{authService: authService, cfg: cfg}
}

func (h *AuthHandler) setRefreshTokenCookie(c fiber.Ctx, token string) {
	secure := h.cfg.Server.Env == "production"
	c.Cookie(&fiber.Cookie{
		Name:     refreshTokenCookieName,
		Value:    token,
		Path:     "/api/v1/auth",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
}

func (h *AuthHandler) clearRefreshTokenCookie(c fiber.Ctx) {
	secure := h.cfg.Server.Env == "production"
	c.Cookie(&fiber.Cookie{
		Name:     refreshTokenCookieName,
		Value:    "",
		Path:     "/api/v1/auth",
		HTTPOnly: true,
		Secure:   secure,
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (h *AuthHandler) clearBrowserCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     browserCookieName,
		Value:    "",
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   -1,
	})
}

const browserCookieName = "nm_browser"

func (h *AuthHandler) browserID(c fiber.Ctx) string {
	id := c.Cookies(browserCookieName)
	if id == "" {
		id = uuid.NewString()
	}
	c.Cookie(&fiber.Cookie{
		Name:     browserCookieName,
		Value:    id,
		Path:     "/",
		HTTPOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   int((365 * 24 * time.Hour).Seconds()),
	})
	return id
}

func (h *AuthHandler) ListSessions(c fiber.Ctx) error {
	callerUUID := c.Locals("user_uuid").(string)
	browserID := c.Cookies(browserCookieName)
	activeRT := c.Cookies(refreshTokenCookieName)
	items, err := h.authService.ListBrowserSessions(c.Context(), browserID, callerUUID, activeRT)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.Unauthorized(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, dto.ListSessionsResponse{Items: items})
}

func (h *AuthHandler) SwitchSession(c fiber.Ctx) error {
	var req dto.AccountSubRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	callerUUID := c.Locals("user_uuid").(string)
	browserID := c.Cookies(browserCookieName)
	tokens, user, err := h.authService.SwitchActiveSession(c.Context(), browserID, callerUUID, req.Sub)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.Unauthorized(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	h.setRefreshTokenCookie(c, tokens.RefreshToken)
	h.browserID(c)
	return response.Success(c, dto.LoginResponse{
		User: dto.UserResponse{
			UUID:            user.UUID,
			Name:            user.Name,
			Email:           user.Email,
			Avatar:          user.Avatar,
			AvatarImageHash: user.AvatarImageHash,
			Bio:             user.Bio,
			Moemoepoint:     user.Moemoepoint,
			Status:          user.Status,
			Roles:           user.RoleNames(),
			CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
		AccessToken: tokens.AccessToken,
	})
}

func (h *AuthHandler) LogoutAccount(c fiber.Ctx) error {
	var req dto.AccountSubRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	callerUUID := c.Locals("user_uuid").(string)
	browserID := c.Cookies(browserCookieName)
	activeRT := c.Cookies(refreshTokenCookieName)
	removed, clearedActive, err := h.authService.LogoutBrowserAccount(c.Context(), browserID, callerUUID, req.Sub, activeRT)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.Unauthorized(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	if !removed {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}
	if clearedActive {
		h.clearRefreshTokenCookie(c)
	}
	return response.Success(c, nil)
}

func (h *AuthHandler) LogoutAll(c fiber.Ctx) error {
	browserID := c.Cookies(browserCookieName)
	if err := h.authService.LogoutBrowserAll(c.Context(), browserID); err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	h.clearRefreshTokenCookie(c)
	h.clearBrowserCookie(c)
	return response.Success(c, nil)
}

func (h *AuthHandler) SendRegisterCode(c fiber.Ctx) error {
	var req dto.SendRegisterCodeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	if err := h.authService.SendRegisterCode(c.Context(), req.Name, req.Email); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "验证码已发送到该邮箱", nil)
}

func (h *AuthHandler) Register(c fiber.Ctx) error {
	var req dto.RegisterRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	req.UserAgent = string(c.Request().Header.UserAgent())
	req.IPAddress = c.IP()
	req.BrowserID = h.browserID(c)

	tokens, user, err := h.authService.Register(c.Context(), &req)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)

	return response.Success(c, dto.LoginResponse{
		User: dto.UserResponse{
			UUID:            user.UUID,
			Name:            user.Name,
			Email:           user.Email,
			Avatar:          user.Avatar,
			AvatarImageHash: user.AvatarImageHash,
			Bio:             user.Bio,
			Moemoepoint:     user.Moemoepoint,
			Status:          user.Status,
			Roles:           []string{},
			CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
		AccessToken: tokens.AccessToken,
	})
}

func (h *AuthHandler) Login(c fiber.Ctx) error {
	var req dto.LoginRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	req.UserAgent = string(c.Request().Header.UserAgent())
	req.IPAddress = c.IP()
	req.BrowserID = h.browserID(c)

	tokens, user, err := h.authService.Login(c.Context(), &req)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.Unauthorized(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)

	return response.Success(c, dto.LoginResponse{
		User: dto.UserResponse{
			UUID:            user.UUID,
			Name:            user.Name,
			Email:           user.Email,
			Avatar:          user.Avatar,
			AvatarImageHash: user.AvatarImageHash,
			Bio:             user.Bio,
			Moemoepoint:     user.Moemoepoint,
			Status:          user.Status,
			Roles:           user.RoleNames(),
			CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		},
		AccessToken: tokens.AccessToken,
	})
}

func (h *AuthHandler) Logout(c fiber.Ctx) error {
	token := c.Get("Authorization")
	if token != "" {
		if len(token) > 7 && token[:7] == "Bearer " {
			token = token[7:]
		}
		_ = h.authService.Logout(c.Context(), token)
	}

	h.clearRefreshTokenCookie(c)

	return response.Success(c, nil)
}

func (h *AuthHandler) Refresh(c fiber.Ctx) error {
	rt := c.Cookies(refreshTokenCookieName)
	if rt == "" {
		var req dto.RefreshRequest
		if err := c.Bind().JSON(&req); err == nil && req.RefreshToken != "" {
			rt = req.RefreshToken
		}
	}
	if rt == "" {
		return response.Unauthorized(c, errors.ErrAuthInvalidToken)
	}

	tokens, err := h.authService.RefreshToken(c.Context(), rt)
	if err != nil {
		h.clearRefreshTokenCookie(c)
		if appErr, ok := err.(*errors.AppError); ok {
			return response.Unauthorized(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	h.setRefreshTokenCookie(c, tokens.RefreshToken)

	return response.Success(c, dto.RefreshResponse{
		AccessToken: tokens.AccessToken,
	})
}

func (h *AuthHandler) Me(c fiber.Ctx) error {
	userUUID := c.Locals("user_uuid").(string)
	scope, _ := c.Locals("user_scope").(string)

	user, err := h.authService.GetCurrentUserWithRoles(c.Context(), userUUID)
	if err != nil {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}

	return response.Success(c, dto.UserResponse{
		UUID:            user.UUID,
		Name:            user.Name,
		Email:           service.EmailForScope(scope, user.Email),
		Avatar:          user.Avatar,
		AvatarImageHash: user.AvatarImageHash,
		Bio:             user.Bio,
		Moemoepoint:     user.Moemoepoint,
		Status:          user.Status,
		Roles:           user.RoleNames(),
		CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *AuthHandler) UpdateProfile(c fiber.Ctx) error {
	var req dto.UpdateProfileRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	userUUID := c.Locals("user_uuid").(string)
	scope, _ := c.Locals("user_scope").(string)

	user, err := h.authService.UpdateProfile(c.Context(), userUUID, &req)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	roles := make([]string, 0, len(user.Roles))
	for _, r := range user.Roles {
		roles = append(roles, r.Name)
	}

	return response.Success(c, dto.UserResponse{
		UUID:            user.UUID,
		Name:            user.Name,
		Email:           service.EmailForScope(scope, user.Email),
		Avatar:          user.Avatar,
		AvatarImageHash: user.AvatarImageHash,
		Bio:             user.Bio,
		Moemoepoint:     user.Moemoepoint,
		Status:          user.Status,
		Roles:           roles,
		CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *AuthHandler) ChangePassword(c fiber.Ctx) error {
	var req dto.ChangePasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	userUUID := c.Locals("user_uuid").(string)

	if err := h.authService.ChangePassword(c.Context(), userUUID, req.OldPassword, req.NewPassword); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "密码修改成功", nil)
}

func (h *AuthHandler) ForgotPassword(c fiber.Ctx) error {
	var req dto.ForgotPasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	_ = h.authService.ForgotPassword(c.Context(), req.Email)

	return response.SuccessWithMessage(c, "如果该邮箱已注册，我们已发送密码重置链接", nil)
}

func (h *AuthHandler) ResetPassword(c fiber.Ctx) error {
	var req dto.ResetPasswordRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	if err := h.authService.ResetPassword(c.Context(), req.Token, req.Password); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "密码重置成功", nil)
}

func (h *AuthHandler) GetProfile(c fiber.Ctx) error {
	uuid := c.Params("uuid")
	if uuid == "" {
		return response.BadRequest(c, errors.ErrMissingParam)
	}

	user, err := h.authService.GetCurrentUserWithRoles(c.Context(), uuid)
	if err != nil {
		return response.NotFound(c, errors.ErrAuthUserNotFound)
	}

	return response.Success(c, dto.UserResponse{
		UUID:            user.UUID,
		Name:            user.Name,
		Avatar:          user.Avatar,
		AvatarImageHash: user.AvatarImageHash,
		Bio:             user.Bio,
		Moemoepoint:     user.Moemoepoint,
		Roles:           user.RoleNames(),
		CreatedAt:       user.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	})
}

func (h *AuthHandler) SendEmailChangeCode(c fiber.Ctx) error {
	var req dto.SendEmailChangeCodeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	userUUID := c.Locals("user_uuid").(string)

	if err := h.authService.SendEmailChangeCode(c.Context(), userUUID, req.NewEmail); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "验证码已发送到当前邮箱", nil)
}

func (h *AuthHandler) ChangeEmail(c fiber.Ctx) error {
	var req dto.ChangeEmailRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	userUUID := c.Locals("user_uuid").(string)

	if err := h.authService.ChangeEmail(c.Context(), userUUID, req.Code, req.NewEmail); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.SuccessWithMessage(c, "邮箱修改成功", nil)
}
