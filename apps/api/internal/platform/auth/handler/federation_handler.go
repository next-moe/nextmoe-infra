package handler

import (
	"net/url"
	"strings"
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

const federationStateCookieName = "nm_fed_state"

type FederationHandler struct {
	fedService *service.FederationService
	cfg        *config.Config
}

func NewFederationHandler(fedSvc *service.FederationService, cfg *config.Config) *FederationHandler {
	return &FederationHandler{fedService: fedSvc, cfg: cfg}
}

func (h *FederationHandler) Providers(c fiber.Ctx) error {
	names := h.fedService.EnabledProviders()
	items := make([]dto.FederationProviderItem, 0, len(names))
	for _, name := range names {
		items = append(items, dto.FederationProviderItem{Name: name})
	}
	return response.Success(c, dto.FederationProvidersResponse{Providers: items})
}

func (h *FederationHandler) Start(c fiber.Ctx) error {
	provider := c.Params("provider")
	redirect := c.Query("redirect")
	authorizeURL, state, err := h.fedService.Start(c.Context(), provider, redirect)
	if err != nil {
		code := "federation_failed"
		if appErr, ok := err.(*errors.AppError); ok && appErr.Code == errors.ErrAuthFederationDisabled {
			code = "federation_disabled"
		}
		return h.errorRedirect(c, code, redirect)
	}
	h.setStateCookie(c, state)
	return c.Redirect().To(authorizeURL)
}

func (h *FederationHandler) Callback(c fiber.Ctx) error {
	provider := c.Params("provider")
	rawRedirect := ""
	if errQ := c.Query("error"); errQ != "" {
		h.clearStateCookie(c)
		return h.errorRedirect(c, "federation_denied", rawRedirect)
	}

	meta := service.SessionMeta{
		UserAgent: string(c.Request().Header.UserAgent()),
		IPAddress: c.IP(),
		BrowserID: h.browserID(c),
	}
	result, err := h.fedService.Callback(
		c.Context(),
		provider,
		c.Query("code"),
		c.Query("state"),
		c.Cookies(federationStateCookieName),
		meta,
	)
	h.clearStateCookie(c)
	if err != nil {
		code := service.CallbackRedirectCode(err)
		if code == "" {
			code = "federation_failed"
		}
		return h.errorRedirect(c, code, service.CallbackRawRedirect(err))
	}

	if result.Outcome == service.CallbackOutcomePending {
		dest := strings.TrimRight(h.cfg.Server.FrontendURL, "/") +
			"/auth/federation/complete?token=" + result.PendingToken +
			"&redirect=" + url.QueryEscape(result.RawRedirect)
		return c.Redirect().To(dest)
	}

	if result.Tokens != nil {
		h.setRefreshTokenCookie(c, result.Tokens.RefreshToken)
	}
	return c.Redirect().To(result.RedirectTo)
}

func (h *FederationHandler) Pending(c fiber.Ctx) error {
	info, err := h.fedService.Pending(c.Context(), c.Query("token"))
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, dto.FederationPendingResponse{
		Provider:      info.Provider,
		SuggestedName: info.SuggestedName,
		Email:         info.Email,
		EmailLocked:   info.EmailLocked,
	})
}

func (h *FederationHandler) Complete(c fiber.Ctx) error {
	var req dto.FederationCompleteRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	req.UserAgent = string(c.Request().Header.UserAgent())
	req.IPAddress = c.IP()
	req.BrowserID = h.browserID(c)

	tokens, user, err := h.fedService.Complete(c.Context(), &req)
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

func (h *FederationHandler) setRefreshTokenCookie(c fiber.Ctx, token string) {
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

func (h *FederationHandler) browserID(c fiber.Ctx) string {
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

func (h *FederationHandler) setStateCookie(c fiber.Ctx, state string) {
	c.Cookie(&fiber.Cookie{
		Name:     federationStateCookieName,
		Value:    state,
		Path:     "/api/v1/auth/federation",
		HTTPOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   600,
	})
}

func (h *FederationHandler) clearStateCookie(c fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     federationStateCookieName,
		Value:    "",
		Path:     "/api/v1/auth/federation",
		HTTPOnly: true,
		Secure:   h.cfg.Server.Env == "production",
		SameSite: fiber.CookieSameSiteLaxMode,
		MaxAge:   -1,
	})
}

func (h *FederationHandler) errorRedirect(c fiber.Ctx, code, rawRedirect string) error {
	q := url.Values{}
	q.Set("error", code)
	if rawRedirect != "" {
		q.Set("redirect", rawRedirect)
	}
	dest := strings.TrimRight(h.cfg.Server.FrontendURL, "/") + "/auth/login?" + q.Encode()
	return c.Redirect().To(dest)
}
