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
	LocalOAuthClient = "artifact:oauth_client"
	LocalSiteKey     = "artifact:site_key"
	LocalUserSub     = "artifact:user_sub"
	LocalAuthMethod  = "artifact:auth_method"
)

const ClientHeaderID = "X-Kun-Artifact-Client-Id"

const (
	MethodBasic = "basic"
	MethodJWT   = "jwt"
)

const uploadScope = "artifact:upload"

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
			method = MethodBasic
		case strings.HasPrefix(authHeader, "Bearer "):
			client, userSub, err = authenticateJWT(c, clientRepo, authHeader, verifier)
			method = MethodJWT
		default:
			return response.Unauthorized(c, errors.ErrArtifactUnauthorized)
		}

		if err != nil {
			if stderrors.Is(err, oidctoken.ErrKeyUnavailable) {
				return response.Error(c, fiber.StatusServiceUnavailable, errors.ErrArtifactUnauthorized, "token verification temporarily unavailable")
			}
			var code int
			switch {
			case stderrors.Is(err, errBadClient):
				code = errors.ErrArtifactBadClient
			case stderrors.Is(err, errBadSecret):
				code = errors.ErrArtifactBadSecret
			case stderrors.Is(err, errSiteDisabled):
				code = errors.ErrArtifactSiteDisabled
			case stderrors.Is(err, errSiteUnconfigured):
				code = errors.ErrArtifactSiteUnconfigured
			default:
				code = errors.ErrArtifactUnauthorized
			}
			if code == errors.ErrArtifactSiteDisabled || code == errors.ErrArtifactSiteUnconfigured {
				return response.Forbidden(c, code)
			}
			return response.Unauthorized(c, code)
		}

		c.Locals(LocalOAuthClient, client)
		c.Locals(LocalSiteKey, client.ArtifactSiteKey)
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
	if !client.ArtifactEnabled {
		return nil, errSiteDisabled
	}
	if client.ArtifactSiteKey == "" {
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
	if !slices.Contains(scopes, uploadScope) {
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
	if !client.ArtifactEnabled {
		return nil, "", errSiteDisabled
	}
	if client.ArtifactSiteKey == "" {
		return nil, "", errSiteUnconfigured
	}

	if client.SiteID == nil || claims.SiteID == 0 || *client.SiteID != claims.SiteID {
		return nil, "", errSiteMismatch
	}
	if claims.UserUUID == "" {
		return nil, "", errBadClient
	}
	return client, claims.UserUUID, nil
}

func parseBasicAuth(header string) (user, pass string, err error) {
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

func AuthMethodFromCtx(c fiber.Ctx) string {
	v, _ := c.Locals(LocalAuthMethod).(string)
	return v
}
