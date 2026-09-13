package middleware

import (
	"log/slog"

	authService "api/internal/platform/auth/service"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
)

func Auth(authSvc *authService.AuthService) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			bearerChallenge(c, "", "")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":    errors.ErrAuthUnauthorized,
				"message": errors.GetMessage(errors.ErrAuthUnauthorized),
			})
		}

		token, ok := splitBearer(authHeader)
		if !ok || token == "" {
			bearerChallenge(c, "invalid_request", "Authorization header must use the Bearer scheme")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":    errors.ErrAuthInvalidToken,
				"message": errors.GetMessage(errors.ErrAuthInvalidToken),
			})
		}

		claims, err := authSvc.ValidateAccessToken(token)
		if err != nil {
			logTokenReject(c, "auth", token, err)
			bearerChallenge(c, "invalid_token", "The access token is expired, revoked or malformed")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":    errors.ErrAuthTokenExpired,
				"message": errors.GetMessage(errors.ErrAuthTokenExpired),
			})
		}

		user, err := authSvc.GetCurrentUser(c.Context(), claims.UserUUID)
		if err != nil || user == nil {
			slog.Warn("auth reject", "stage", "get_current_user",
				"path", c.Path(), "user_uuid", claims.UserUUID, "err", err)
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":    errors.ErrAuthUserNotFound,
				"message": errors.GetMessage(errors.ErrAuthUserNotFound),
			})
		}
		if user.IsBanned() {
			bearerChallenge(c, "invalid_token", "The account is banned")
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"code":    errors.ErrAuthUserBanned,
				"message": errors.GetMessage(errors.ErrAuthUserBanned),
			})
		}

		setIdentityLocals(c, claims)

		return c.Next()
	}
}
