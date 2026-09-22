package handler

import (
	"strings"

	"api/internal/platform/auth/service"
	"api/pkg/config"

	"github.com/gofiber/fiber/v3"
)

type OIDCHandler struct {
	svc *service.SigningKeyService
	cfg *config.Config
}

func NewOIDCHandler(svc *service.SigningKeyService, cfg *config.Config) *OIDCHandler {
	return &OIDCHandler{svc: svc, cfg: cfg}
}

func (h *OIDCHandler) JWKS(c fiber.Ctx) error {
	set, err := h.svc.JWKS(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"error": "server_error"})
	}
	h.publicHeaders(c)
	return c.JSON(set)
}

func (h *OIDCHandler) Discovery(c fiber.Ctx) error {
	h.publicHeaders(c)
	return c.JSON(h.metadata())
}

func (h *OIDCHandler) publicHeaders(c fiber.Ctx) {
	c.Set("Access-Control-Allow-Origin", "*")
	c.Set("Cache-Control", "public, max-age=300")
}

func (h *OIDCHandler) metadata() fiber.Map {
	iss := strings.TrimRight(h.cfg.Server.SiteURL, "/")
	api := iss + "/api/v1/oauth"
	return fiber.Map{
		"issuer":                                iss,
		"authorization_endpoint":                api + "/authorize",
		"token_endpoint":                        api + "/token",
		"userinfo_endpoint":                     api + "/userinfo",
		"revocation_endpoint":                   api + "/revoke",
		"end_session_endpoint":                  api + "/logout",
		"jwks_uri":                              iss + "/oauth/jwks",
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"scopes_supported":                      []string{"openid", "profile", "email", PreferencesScope},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic", "client_secret_post", "none"},
		"code_challenge_methods_supported":      []string{"S256"},
		"claims_supported": []string{
			"iss", "sub", "aud", "exp", "iat", "nonce",
			"name", "email", "picture", "roles",
			"adult_confirmed", "nsfw_display",
		},
	}
}
