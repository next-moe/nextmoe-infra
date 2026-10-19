package handler

import (
	"encoding/base64"
	"log/slog"
	"net/url"
	"strings"

	"api/internal/middleware"
	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/service"
	sitemodel "api/internal/platform/site/model"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

func bindByContentType(c fiber.Ctx, out any) error {
	if strings.HasPrefix(c.Get("Content-Type"), "application/x-www-form-urlencoded") {
		return c.Bind().Form(out)
	}
	return c.Bind().JSON(out)
}

func basicClientAuth(authHeader string) (clientID, secret string, ok bool) {
	const prefix = "Basic "
	if !strings.HasPrefix(authHeader, prefix) {
		return "", "", false
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(authHeader, prefix))
	if err != nil {
		return "", "", false
	}
	id, sec, found := strings.Cut(string(raw), ":")
	if !found {
		return "", "", false
	}
	if u, err := url.QueryUnescape(id); err == nil {
		id = u
	}
	if u, err := url.QueryUnescape(sec); err == nil {
		sec = u
	}
	return id, sec, true
}

func buildRedirectURL(baseURI, code, state string) (string, error) {
	u, err := url.Parse(baseURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("code", code)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func buildErrorRedirectURL(baseURI, errCode, state string) (string, error) {
	u, err := url.Parse(baseURI)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("error", errCode)
	if state != "" {
		q.Set("state", state)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

type OAuthHandler struct {
	oauthService *service.OAuthService
	cfg          *config.Config
}

func NewOAuthHandler(oauthService *service.OAuthService, cfg *config.Config) *OAuthHandler {
	return &OAuthHandler{oauthService: oauthService, cfg: cfg}
}

func oauthErrString(appCode int) string {
	switch appCode {
	case errors.ErrOAuthInvalidClient, errors.ErrOAuthInvalidClientSecret:
		return "invalid_client"
	case errors.ErrOAuthInvalidCode, errors.ErrOAuthInvalidCodeVerifier, errors.ErrOAuthInvalidRedirectURI,
		errors.ErrAuthUserBanned, errors.ErrAuthTokenExpired, errors.ErrAuthInvalidToken:
		return "invalid_grant"
	case errors.ErrOAuthInvalidScope:
		return "invalid_scope"
	case errors.ErrOAuthInvalidGrant:
		return "unauthorized_client"
	case errors.ErrOAuthUnsupportedGrantType:
		return "unsupported_grant_type"
	default:
		return "invalid_request"
	}
}

func protoErr(c fiber.Ctx, appCode int) error {
	errStr := oauthErrString(appCode)
	status := fiber.StatusBadRequest
	if errStr == "invalid_client" {
		status = fiber.StatusUnauthorized
	}
	return c.Status(status).JSON(fiber.Map{
		"error":             errStr,
		"error_description": errors.GetMessage(appCode),
	})
}

func protoInvalidRequest(c fiber.Ctx, desc string) error {
	return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
		"error":             "invalid_request",
		"error_description": desc,
	})
}

func protoServerError(c fiber.Ctx) error {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error":             "server_error",
		"error_description": errors.GetMessage(errors.ErrOperationFailed),
	})
}

func (h *OAuthHandler) Authorize(c fiber.Ctx) error {
	var req dto.AuthorizeRequest
	if err := c.Bind().Query(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	client, err := h.oauthService.ValidateClient(c.Context(), req.ClientID, req.RedirectURI)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	// The refresh cookie is scoped to /api/v1/auth and never reaches this path,
	// so a browser that may already be signed in takes one hop to a sibling
	// route that can see it, instead of rendering the consent page to find out.
	if silentEligible(client, req.Prompt) {
		silent := strings.TrimSuffix(c.Route().Path, "/oauth/authorize") + silentAuthorizePath
		return c.Redirect().To(silent + "?" + authorizeQuery(&req).Encode())
	}
	return c.Redirect().To(h.frontendAuthorizeURL(&req))
}

const silentAuthorizePath = "/auth/oauth/silent-authorize"

func silentEligible(client *sitemodel.OAuthClient, prompt string) bool {
	return client.AutoConsent && (prompt == "" || prompt == "none")
}

func authorizeQuery(req *dto.AuthorizeRequest) url.Values {
	q := url.Values{}
	q.Set("client_id", req.ClientID)
	q.Set("redirect_uri", req.RedirectURI)
	q.Set("response_type", req.ResponseType)
	q.Set("scope", req.Scope)
	q.Set("state", req.State)
	if req.CodeChallenge != "" {
		q.Set("code_challenge", req.CodeChallenge)
	}
	if req.CodeChallengeMethod != "" {
		q.Set("code_challenge_method", req.CodeChallengeMethod)
	}
	if req.Prompt != "" {
		q.Set("prompt", req.Prompt)
	}
	if req.LoginHint != "" {
		q.Set("login_hint", req.LoginHint)
	}
	if req.Nonce != "" {
		q.Set("nonce", req.Nonce)
	}
	return q
}

func (h *OAuthHandler) frontendAuthorizeURL(req *dto.AuthorizeRequest) string {
	return h.cfg.Server.FrontendURL + "/oauth/authorize?" + authorizeQuery(req).Encode()
}

// SilentAuthorize answers an auto-consent client straight to its redirect_uri
// when the browser's account-center sign-in leaves nothing to ask; every other
// case goes to the frontend flow exactly as Authorize sent it before.
func (h *OAuthHandler) SilentAuthorize(c fiber.Ctx) error {
	c.Set(fiber.HeaderCacheControl, "no-store")

	var req dto.AuthorizeRequest
	if err := c.Bind().Query(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	client, err := h.oauthService.ValidateClient(c.Context(), req.ClientID, req.RedirectURI)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	frontend := h.frontendAuthorizeURL(&req)
	if !silentEligible(client, req.Prompt) {
		return c.Redirect().To(frontend)
	}

	refreshToken := c.Cookies(refreshTokenCookieName)
	session, user, ok := h.oauthService.LiveBrowserSession(c.Context(), refreshToken)
	if !ok {
		if req.Prompt != "none" {
			return c.Redirect().To(frontend)
		}
		target, err := buildErrorRedirectURL(req.RedirectURI, "login_required", req.State)
		if err != nil {
			return response.InternalError(c, errors.ErrOperationFailed)
		}
		return c.Redirect().To(target)
	}
	if req.LoginHint != "" && req.LoginHint != user.UUID && req.LoginHint != user.Email {
		return c.Redirect().To(frontend)
	}

	codeChallengeMethod := req.CodeChallengeMethod
	if codeChallengeMethod == "" && req.CodeChallenge != "" {
		codeChallengeMethod = "S256"
	}
	code, err := h.oauthService.CreateAuthorizationCode(
		c.Context(),
		user.ID,
		req.ClientID,
		req.RedirectURI,
		req.Scope,
		req.CodeChallenge,
		codeChallengeMethod,
		req.Nonce,
		c.Cookies(browserCookieName),
	)
	if err != nil {
		return c.Redirect().To(frontend)
	}
	target, err := buildRedirectURL(req.RedirectURI, code, req.State)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	maxAge := h.oauthService.KeepBrowserSessionAlive(c.Context(), session)
	if refreshToken == session.RefreshToken {
		setRefreshCookie(c, h.cfg, refreshToken, maxAge)
	}
	return c.Redirect().To(target)
}

func (h *OAuthHandler) Consent(c fiber.Ctx) error {
	var req dto.AuthorizeRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}

	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}

	_, err := h.oauthService.ValidateClient(c.Context(), req.ClientID, req.RedirectURI)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	userUUID := c.Locals("user_uuid").(string)
	userID, err := h.oauthService.GetUserIDByUUID(c.Context(), userUUID)
	if err != nil {
		return response.Unauthorized(c, errors.ErrAuthUserNotFound)
	}

	codeChallengeMethod := req.CodeChallengeMethod
	if codeChallengeMethod == "" && req.CodeChallenge != "" {
		codeChallengeMethod = "S256"
	}

	code, err := h.oauthService.CreateAuthorizationCode(
		c.Context(),
		userID,
		req.ClientID,
		req.RedirectURI,
		req.Scope,
		req.CodeChallenge,
		codeChallengeMethod,
		req.Nonce,
		c.Cookies(browserCookieName),
	)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	redirectURL, err := buildRedirectURL(req.RedirectURI, code, req.State)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}

	return response.Success(c, fiber.Map{
		"redirect_url": redirectURL,
	})
}

func (h *OAuthHandler) AuthorizeError(c fiber.Ctx) error {
	var req dto.AuthorizeErrorRequest
	if err := c.Bind().JSON(&req); err != nil {
		return response.BadRequest(c, errors.ErrBadRequest)
	}
	if err := utils.Validate(&req); err != nil {
		return response.BadRequestMsg(c, errors.ErrValidationFailed, err.Error())
	}
	if _, err := h.oauthService.ValidateClient(c.Context(), req.ClientID, req.RedirectURI); err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.BadRequest(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	redirectURL, err := buildErrorRedirectURL(req.RedirectURI, req.Error, req.State)
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, fiber.Map{"redirect_url": redirectURL})
}

func (h *OAuthHandler) Token(c fiber.Ctx) error {
	var req dto.TokenRequest
	if err := bindByContentType(c, &req); err != nil {
		slog.Warn("oauth token bind failed",
			"content_type", c.Get("Content-Type"),
			"body_len", len(c.Body()),
			"err", err,
		)
		return protoErr(c, errors.ErrBadRequest)
	}
	if id, secret, ok := basicClientAuth(c.Get("Authorization")); ok {
		if req.ClientID == "" {
			req.ClientID = id
		}
		if req.ClientSecret == "" {
			req.ClientSecret = secret
		}
	}

	if err := utils.Validate(&req); err != nil {
		slog.Warn("oauth token validate failed", "grant_type", req.GrantType, "client_id", req.ClientID, "err", err)
		return protoInvalidRequest(c, err.Error())
	}

	switch req.GrantType {
	case "authorization_code":
		tokenResp, err := h.oauthService.ExchangeCode(c.Context(), &req)
		if err != nil {
			if appErr, ok := err.(*errors.AppError); ok {
				return protoErr(c, appErr.Code)
			}
			return protoServerError(c)
		}
		return c.JSON(tokenResp)

	case "refresh_token":
		if req.RefreshToken == "" {
			return protoInvalidRequest(c, "refresh_token is required for grant_type=refresh_token")
		}
		tokenResp, err := h.oauthService.RefreshWithClient(c.Context(), req.RefreshToken, req.ClientID, req.ClientSecret)
		if err != nil {
			if appErr, ok := err.(*errors.AppError); ok {
				return protoErr(c, appErr.Code)
			}
			return protoServerError(c)
		}
		return c.JSON(tokenResp)

	default:
		return protoErr(c, errors.ErrOAuthUnsupportedGrantType)
	}
}

func (h *OAuthHandler) UserInfo(c fiber.Ctx) error {
	userUUID := c.Locals("user_uuid").(string)
	scope, _ := c.Locals("user_scope").(string)
	siteID, _ := c.Locals("user_site").(uint)

	info, err := h.oauthService.GetUserInfo(c.Context(), userUUID, scope, siteID)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			slog.Warn("oauth userinfo failed", "user_uuid", userUUID, "code", appErr.Code)
			return middleware.BearerError(c, fiber.StatusUnauthorized, "invalid_token",
				errors.GetMessage(appErr.Code))
		}
		return protoServerError(c)
	}

	return c.JSON(info)
}

func (h *OAuthHandler) GetClientPublic(c fiber.Ctx) error {
	clientID := c.Query("client_id")
	if clientID == "" {
		return response.BadRequest(c, errors.ErrInvalidID)
	}
	info, err := h.oauthService.GetClientPublicInfo(c.Context(), clientID)
	if err != nil {
		if appErr, ok := err.(*errors.AppError); ok {
			return response.NotFound(c, appErr.Code)
		}
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, info)
}

func (h *OAuthHandler) GetEcosystem(c fiber.Ctx) error {
	apps, err := h.oauthService.ListEcosystem(c.Context())
	if err != nil {
		return response.InternalError(c, errors.ErrOperationFailed)
	}
	return response.Success(c, fiber.Map{"apps": apps})
}

func (h *OAuthHandler) PostLogoutRedirect(c fiber.Ctx) error {
	clientID := c.Query("client_id")
	redirect := c.Query("redirect")
	validated, _ := h.oauthService.ValidatePostLogoutRedirect(c.Context(), clientID, redirect)
	return response.Success(c, fiber.Map{"url": validated})
}

func (h *OAuthHandler) LogoutRedirect(c fiber.Ctx) error {
	param := func(key string) string {
		if v := c.Query(key); v != "" {
			return v
		}
		return c.FormValue(key)
	}
	clientID := param("client_id")
	redirect := param("redirect")
	if redirect == "" {
		redirect = param("post_logout_redirect_uri")
	}
	hint := param("id_token_hint")
	if clientID == "" && hint != "" {
		clientID = clientIDFromIDTokenHint(hint)
	}

	q := url.Values{}
	if clientID != "" {
		q.Set("client_id", clientID)
	}
	if redirect != "" {
		q.Set("redirect", redirect)
	}
	if st := param("state"); st != "" {
		q.Set("state", st)
	}
	if hint != "" {
		q.Set("id_token_hint", hint)
	}
	dest := h.cfg.Server.FrontendURL + "/auth/logout"
	if enc := q.Encode(); enc != "" {
		dest += "?" + enc
	}
	return c.Redirect().To(dest)
}

func clientIDFromIDTokenHint(hint string) string {
	tok, _, err := jwt.NewParser().ParseUnverified(hint, jwt.MapClaims{})
	if err != nil {
		return ""
	}
	aud, err := tok.Claims.GetAudience()
	if err != nil || len(aud) == 0 {
		return ""
	}
	return aud[0]
}

func (h *OAuthHandler) Revoke(c fiber.Ctx) error {
	var req struct {
		Token         string `json:"token" form:"token" validate:"required"`
		TokenTypeHint string `json:"token_type_hint" form:"token_type_hint"`
	}
	if err := bindByContentType(c, &req); err != nil {
		return protoInvalidRequest(c, "the request body could not be parsed")
	}

	if req.Token == "" {
		return protoInvalidRequest(c, "token is required")
	}

	_ = h.oauthService.RevokeToken(c.Context(), req.Token, req.TokenTypeHint)

	return c.SendStatus(fiber.StatusOK)
}
