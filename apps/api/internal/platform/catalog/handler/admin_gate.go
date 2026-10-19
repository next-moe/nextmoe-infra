package handler

import (
	"strings"

	"api/internal/middleware"
	catperm "api/internal/platform/catalog/perm"
	"api/pkg/errors"
	"api/pkg/response"
	"api/pkg/routepath"

	"github.com/gofiber/fiber/v3"
)

const AdminClaimsPrefix = "/api/v1/admin/catalog/claims"

func AdminGate(clients OAuthClientLookup) fiber.Handler {
	curation := middleware.RequirePermission(catperm.Resolver, catperm.Review)
	claims := middleware.RequirePermission(catperm.Resolver, catperm.ClaimReview)
	editPolicy := middleware.RequirePermission(catperm.Resolver, catperm.EditPolicyManage)
	return func(c fiber.Ctx) error {
		if err := refuseThirdPartyAdminClient(c, clients); err != nil {
			return err
		}
		path := routepath.Normalize(c.Path())
		switch {
		case strings.HasPrefix(path, AdminClaimsPrefix):
			return claims(c)
		case path == AdminEditPolicyPrefix || strings.HasPrefix(path, AdminEditPolicyPrefix+"/"):
			return editPolicy(c)
		}
		return curation(c)
	}
}

func refuseThirdPartyAdminClient(c fiber.Ctx, clients OAuthClientLookup) error {
	clientID, _ := c.Locals("token_client_id").(string)
	if clientID == "" {
		return nil
	}
	client, err := clients.FindByClientID(c.Context(), clientID)
	if err != nil || client == nil {
		return response.ForbiddenMsg(c, errors.ErrForbidden,
			"the access token's client is not registered")
	}
	if isThirdPartyClient(client) {
		return response.ForbiddenMsg(c, errors.ErrForbidden,
			"a third-party application is not a moderation surface; the catalog admin face needs a first-party client")
	}
	return nil
}
