package middleware

import (
	stderrors "errors"

	"api/pkg/errors"
	"api/pkg/oidctoken"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
)

func keyUnavailable(c fiber.Ctx) error {
	return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
		"code":    errors.ErrOperationFailed,
		"message": "令牌验证暂不可用，请稍后重试",
	})
}

func JWTAuth(verifier *oidctoken.Verifier) fiber.Handler {
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

		claims, err := verifier.Parse(c.Context(), token)
		if err != nil {
			if stderrors.Is(err, oidctoken.ErrKeyUnavailable) {
				return keyUnavailable(c)
			}
			bearerChallenge(c, "invalid_token", "The access token is expired, revoked or malformed")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"code":    errors.ErrAuthTokenExpired,
				"message": errors.GetMessage(errors.ErrAuthTokenExpired),
			})
		}

		setIdentityLocals(c, claims)

		return c.Next()
	}
}

func setIdentityLocals(c fiber.Ctx, claims *utils.TokenClaims) {
	c.Locals("user_uuid", claims.UserUUID)
	c.Locals("user_id", claims.ID)
	c.Locals("user_roles", UnionRoles(claims.Roles, claims.SiteRoles))
	c.Locals("user_scope", claims.Scope)
	c.Locals("user_site", claims.SiteID)
	c.Locals("user_global_roles", claims.Roles)
	c.Locals("token_client_id", claims.ClientID)
}

// UnionRoles is what "the roles this token carries" means everywhere. A site
// grant is a role like any other, and a face that reads claims.Roles alone
// cannot see one.
func UnionRoles(global, site []string) []string {
	if len(site) == 0 {
		return global
	}
	seen := make(map[string]struct{}, len(global)+len(site))
	out := make([]string, 0, len(global)+len(site))
	for _, r := range global {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	for _, r := range site {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

func OptionalJWT(verifier *oidctoken.Verifier) fiber.Handler {
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Next()
		}
		token, ok := splitBearer(authHeader)
		if !ok || token == "" {
			return c.Next()
		}
		claims, err := verifier.Parse(c.Context(), token)
		if err != nil {
			if stderrors.Is(err, oidctoken.ErrKeyUnavailable) {
				return keyUnavailable(c)
			}
			return c.Next()
		}
		setIdentityLocals(c, claims)
		return c.Next()
	}
}
