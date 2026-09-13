package middleware

import (
	stderrors "errors"
	"fmt"
	"log/slog"
	"strings"

	authService "api/internal/platform/auth/service"

	"github.com/gofiber/fiber/v3"
	"github.com/golang-jwt/jwt/v5"
)

const bearerRealm = "kungal"

// logTokenReject separates "this token died of old age" from "the caller never
// had a token". An expired one is the normal end of a 15-minute life and stays
// at Debug. A malformed one is an integration bug on the far side and used to
// be invisible: two third-party RPs spent a day sending `Bearer undefined` —
// they read access_token off the {code,message,data} envelope the protocol
// endpoints stopped sending in 2026-07 — and the only trace anywhere was an
// unexplained 401 one millisecond after a 200 from /oauth/token.
// Never log the token itself; the shape is what identifies the bug.
func logTokenReject(c fiber.Ctx, guard, token string, err error) {
	if !stderrors.Is(err, jwt.ErrTokenMalformed) {
		slog.Debug(guard+" reject", "stage", "token_invalid", "path", c.Path(), "err", err)
		return
	}
	slog.Warn(guard+" reject", "stage", "token_malformed", "path", c.Path(),
		"client_ip", c.IP(), "token_len", len(token),
		"token_segments", strings.Count(token, ".")+1,
		"token_literal", placeholderToken(token))
}

// placeholderToken echoes the token only when it is a language's own word for
// "I had nothing here", which is the whole tell.
func placeholderToken(token string) string {
	switch token {
	case "undefined", "null", "None", "nil", "false", "[object Object]":
		return token
	default:
		return ""
	}
}

func splitBearer(header string) (token string, ok bool) {
	scheme, rest, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return rest, true
}

func bearerChallenge(c fiber.Ctx, errCode, desc string) {
	if errCode == "" {
		c.Set(fiber.HeaderWWWAuthenticate, fmt.Sprintf("Bearer realm=%q", bearerRealm))
		return
	}
	c.Set(fiber.HeaderWWWAuthenticate,
		fmt.Sprintf("Bearer realm=%q, error=%q, error_description=%q", bearerRealm, errCode, desc))
}

func BearerError(c fiber.Ctx, status int, errCode, desc string) error {
	bearerChallenge(c, errCode, desc)
	if errCode == "" {
		c.Status(status)
		return nil
	}
	return c.Status(status).JSON(fiber.Map{
		"error":             errCode,
		"error_description": desc,
	})
}

func BearerAuth(authSvc *authService.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get(fiber.HeaderAuthorization)
		if authHeader == "" {
			return BearerError(c, fiber.StatusUnauthorized, "", "")
		}

		token, ok := splitBearer(authHeader)
		if !ok || token == "" {
			return BearerError(c, fiber.StatusBadRequest, "invalid_request",
				"Authorization header must use the Bearer scheme")
		}

		claims, err := authSvc.ValidateAccessToken(token)
		if err != nil {
			logTokenReject(c, "bearer", token, err)
			return BearerError(c, fiber.StatusUnauthorized, "invalid_token",
				"The access token is expired, revoked or malformed")
		}

		user, err := authSvc.GetCurrentUser(c.Context(), claims.UserUUID)
		if err != nil || user == nil {
			slog.Warn("bearer reject", "stage", "get_current_user",
				"path", c.Path(), "user_uuid", claims.UserUUID, "err", err)
			return BearerError(c, fiber.StatusUnauthorized, "invalid_token",
				"The access token does not identify a known user")
		}
		if user.IsBanned() {
			return BearerError(c, fiber.StatusForbidden, "invalid_token",
				"The account is banned")
		}

		setIdentityLocals(c, claims)

		return c.Next()
	}
}
