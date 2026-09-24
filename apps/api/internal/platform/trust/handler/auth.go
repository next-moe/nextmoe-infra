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

const localClient = "trust:oauth_client"

type ctxKey string

const (
	ctxKeyClient      ctxKey = "trust:oauth_client"
	ctxKeyAdminID     ctxKey = "trust:admin_user_id"
	ctxKeyGlobalRoles ctxKey = "trust:user_global_roles"
	ctxKeyClientID    ctxKey = "trust:token_client_id"
)

type clientSiteLookup interface {
	FindByClientID(ctx context.Context, clientID string) (*siteModel.OAuthClient, error)
}

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

func siteBinding(ctx context.Context) (string, *houseError) {
	client := clientFromCtx(ctx)
	if client == nil || client.CommunityTenant() == "" {
		return "", apiErrMsg(http.StatusForbidden, errors.ErrForbidden,
			"client is not bound to a site; it cannot submit reports")
	}
	return client.CommunityTenant(), nil
}

func AdminBridge(ctx huma.Context, next func(huma.Context)) {
	fc := humafiber.Unwrap(ctx)
	if id, ok := fc.Locals("user_id").(uint); ok {
		ctx = huma.WithValue(ctx, ctxKeyAdminID, int64(id))
	}
	if roles, ok := fc.Locals("user_global_roles").([]string); ok {
		ctx = huma.WithValue(ctx, ctxKeyGlobalRoles, roles)
	}
	if cid, ok := fc.Locals("token_client_id").(string); ok {
		ctx = huma.WithValue(ctx, ctxKeyClientID, cid)
	}
	next(ctx)
}

func adminIDFromCtx(ctx context.Context) int64 {
	id, _ := ctx.Value(ctxKeyAdminID).(int64)
	return id
}

func globalRolesFromCtx(ctx context.Context) []string {
	roles, _ := ctx.Value(ctxKeyGlobalRoles).([]string)
	return roles
}

func tokenClientIDFromCtx(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyClientID).(string)
	return id
}
