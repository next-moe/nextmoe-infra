package middleware

import (
	"encoding/base64"
	stderrors "errors"
	"fmt"
	"slices"
	"strings"

	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/config"
	"api/pkg/errors"
	"api/pkg/oidctoken"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

const (
	LocalOAuthClient = "image:oauth_client"
	LocalSiteKey     = "image:site_key"
	LocalUserSub     = "image:user_sub"
	LocalAuthMethod  = "image:auth_method"
)

const ClientHeaderID = "X-Kun-Image-Client-Id"

func ClientAuth(clientRepo *siteRepo.OAuthClientRepository, cfg *config.Config) fiber.Handler {
	verifier := oidctoken.NewVerifierWithJWKS(cfg.JWT.Secret, cfg.OIDC.JWKSURL)
	return func(c fiber.Ctx) error {
		authHeader := c.Get("Authorization")

		var (
			client  *siteModel.OAuthClient
			userSub string
			method  string
			err     error
		)

		switch {
		case strings.HasPrefix(authHeader, "Basic "):
			client, err = authenticateBasic(c, clientRepo, authHeader)
			method = "basic"
		case strings.HasPrefix(authHeader, "Bearer "):
			client, userSub, err = authenticateJWT(c, clientRepo, authHeader, verifier)
			method = "jwt"
		default:
			return response.Unauthorized(c, errors.ErrImageUnauthorized)
		}

		if err != nil {
			if stderrors.Is(err, oidctoken.ErrKeyUnavailable) {
				return response.Error(c, fiber.StatusServiceUnavailable, errors.ErrImageUnauthorized, "token verification temporarily unavailable")
			}
			var code int
			switch {
			case stderrors.Is(err, errBadClient):
				code = errors.ErrImageBadClient
			case stderrors.Is(err, errBadSecret):
				code = errors.ErrImageBadSecret
			case stderrors.Is(err, errSiteDisabled):
				code = errors.ErrImageSiteDisabled
			case stderrors.Is(err, errSiteUnconfigured):
				code = errors.ErrImageSiteUnconfigured
			default:
				code = errors.ErrImageUnauthorized
			}
			if code == errors.ErrImageSiteDisabled || code == errors.ErrImageSiteUnconfigured {
				return response.Forbidden(c, code)
			}
			return response.Unauthorized(c, code)
		}

		c.Locals(LocalOAuthClient, client)
		c.Locals(LocalSiteKey, client.ImageSiteKey)
		c.Locals(LocalUserSub, userSub)
		c.Locals(LocalAuthMethod, method)
		return c.Next()
	}
}

var (
	errBadClient        = stderrors.New("bad client")
	errBadSecret        = stderrors.New("bad secret")
	errSiteDisabled     = stderrors.New("site disabled")
	errSiteUnconfigured = stderrors.New("site unconfigured")
	errScopeMissing     = stderrors.New("scope missing")
	errSiteMismatch     = stderrors.New("site mismatch")
)

func authenticateBasic(c fiber.Ctx, repo *siteRepo.OAuthClientRepository, authHeader string) (*siteModel.OAuthClient, error) {
	clientID, secret, err := parseBasicAuth(authHeader)
	if err != nil {
		return nil, errBadClient
	}

	client, err := repo.FindByClientID(c.Context(), clientID)
	if err != nil || client == nil {
		return nil, errBadClient
	}

	if !client.VerifySecret(secret) {
		return nil, errBadSecret
	}

	if !client.ImageEnabled {
		return nil, errSiteDisabled
	}
	if client.ImageSiteKey == "" {
		return nil, errSiteUnconfigured
	}
	return client, nil
}

func authenticateJWT(c fiber.Ctx, repo *siteRepo.OAuthClientRepository, authHeader string, verifier *oidctoken.Verifier) (*siteModel.OAuthClient, string, error) {
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	claims, err := verifier.Parse(c.Context(), tokenStr)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %w", errBadClient, err)
	}

	scopes := strings.Fields(claims.Scope)
	if !slices.Contains(scopes, "image:upload") {
		return nil, "", errScopeMissing
	}

	clientID := c.Get(ClientHeaderID)
	if clientID == "" {
		return nil, "", errBadClient
	}

	client, err := repo.FindByClientID(c.Context(), clientID)
	if err != nil || client == nil {
		return nil, "", errBadClient
	}
	if !client.ImageEnabled {
		return nil, "", errSiteDisabled
	}
	if client.ImageSiteKey == "" {
		return nil, "", errSiteUnconfigured
	}

	// Guard: the user's JWT must match the client's site. Otherwise a
	// user with a valid kungal JWT could impersonate as a moyu uploader.
	//
	// FAIL-CLOSED: any missing piece is a rejection, not a bypass.
	//   - client.SiteID == nil  → client is unconfigured / orphan;
	//                             never let it accept uploads.
	//   - claims.SiteID == 0    → token predates the site_id claim
	//                             (legacy / manually-issued); refuse.
	//   - mismatch              → wrong site.
	// Earlier versions had `if client.SiteID != nil && claims.SiteID != 0 && mismatch`
	// — that combined gate silently allowed all three failure modes.
	if client.SiteID == nil || claims.SiteID == 0 || *client.SiteID != claims.SiteID {
		return nil, "", errSiteMismatch
	}

	return client, claims.UserUUID, nil
}

func parseBasicAuth(header string) (user, pass string, err error) {
	if header == "" {
		return "", "", stderrors.New("missing auth header")
	}
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return "", "", stderrors.New("not Basic auth")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return "", "", err
	}
	u, p, ok := strings.Cut(string(raw), ":")
	if !ok {
		return "", "", stderrors.New("malformed Basic auth")
	}
	return u, p, nil
}

func ClientFromCtx(c fiber.Ctx) *siteModel.OAuthClient {
	v, _ := c.Locals(LocalOAuthClient).(*siteModel.OAuthClient)
	return v
}

func SiteKeyFromCtx(c fiber.Ctx) string {
	v, _ := c.Locals(LocalSiteKey).(string)
	return v
}

func UserSubFromCtx(c fiber.Ctx) string {
	v, _ := c.Locals(LocalUserSub).(string)
	return v
}
