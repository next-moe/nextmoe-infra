package handler

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"net/http"
	"strings"

	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

const localClient = "community:oauth_client"

type ctxKey string

const ctxKeyClient ctxKey = "community:oauth_client"

func S2SAuth(clients *siteRepo.OAuthClientRepository) fiber.Handler {
	return func(c fiber.Ctx) error {
		client, err := authenticateBasic(c, clients)
		if err != nil {
			return response.Unauthorized(c, errors.ErrAuthUnauthorized)
		}
		c.Locals(localClient, client)
		return c.Next()
	}
}

func authenticateBasic(c fiber.Ctx, clients *siteRepo.OAuthClientRepository) (*siteModel.OAuthClient, error) {
	header := c.Get("Authorization")
	const prefix = "Basic "
	if !strings.HasPrefix(header, prefix) {
		return nil, stderrors.New("not Basic auth")
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(header, prefix))
	if err != nil {
		return nil, err
	}
	clientID, secret, ok := strings.Cut(string(raw), ":")
	if !ok {
		return nil, stderrors.New("malformed Basic auth")
	}
	client, err := clients.FindByClientID(c.Context(), clientID)
	if err != nil || client == nil {
		return nil, stderrors.New("bad client")
	}
	if !client.VerifySecret(secret) {
		return nil, stderrors.New("bad secret")
	}
	return client, nil
}

func S2SBridge(ctx huma.Context, next func(huma.Context)) {
	fc := humafiber.Unwrap(ctx)
	if client, ok := fc.Locals(localClient).(*siteModel.OAuthClient); ok {
		ctx = huma.WithValue(ctx, ctxKeyClient, client)
	}
	next(ctx)
}

func clientFromCtx(ctx context.Context) *siteModel.OAuthClient {
	c, _ := ctx.Value(ctxKeyClient).(*siteModel.OAuthClient)
	return c
}

// siteBinding answers the tenant this call acts in: community_site when the
// client declares one, and catalog_site otherwise.
//
// The fallback is not a convenience — it is what keeps every client that
// predates community_site on the tenant it already has rows under. Only a site
// that must be separated from the one it files catalog claims as sets the
// column; see siteModel.OAuthClient.CommunitySite for the case that forced it.
func siteBinding(ctx context.Context) (string, *houseError) {
	client := clientFromCtx(ctx)
	if client == nil {
		return "", apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"client is not bound to a site; it cannot act on the community")
	}
	if client.CommunitySite != "" {
		return client.CommunitySite, nil
	}
	if client.CatalogSite == "" {
		return "", apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"client is not bound to a site; it cannot act on the community")
	}
	return client.CatalogSite, nil
}
