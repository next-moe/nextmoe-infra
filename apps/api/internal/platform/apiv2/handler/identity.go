package handler

import (
	"context"
	"strings"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/apiv2/protocol"
	"api/internal/platform/devapi"
	"api/pkg/routepath"

	"github.com/gofiber/fiber/v3"
)

type ctxKey string

const (
	ctxUserID     ctxKey = "v2_user_id"
	ctxClientID   ctxKey = "v2_client_id"
	ctxSite       ctxKey = "v2_catalog_site"
	ctxRoles      ctxKey = "v2_user_roles"
	ctxCredClient ctxKey = "v2_credential_client_id"
	ctxThirdParty ctxKey = "v2_third_party_client"
)

// SiteBinding is what the token's OAuth client is, not just where it points.
// Wave R3 deleted the v1 surface that filled PolicyContext.ModerationCapped
// from ThirdParty and carried nothing to v2, so from that day the editing
// engine's review cap was a flag nothing ever set.
type SiteBinding struct {
	Site       string
	ThirdParty bool
}

func appClientFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxCredClient).(string)
	return v
}

func requireAppClient(ctx context.Context) (string, *problem.Problem) {
	if id := appClientFrom(ctx); id != "" {
		return id, nil
	}
	return "", problem.New(problem.CodeMissingCredential, "", "", "this operation requires an application key.")
}

func userIDFrom(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxUserID).(int64)
	return v
}

func clientIDFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxClientID).(string)
	return v
}

func requireUser(ctx context.Context) (int64, string, error) {
	uid := userIDFrom(ctx)
	client := clientIDFrom(ctx)
	if uid <= 0 {
		return 0, "", problem.New(problem.CodeUserIdentityRequired, "", "", "this operation requires a user access token.")
	}
	if client == "" {
		return 0, "", problem.New(problem.CodeUserIdentityRequired, "", "", "the access token is missing a client id.")
	}
	return uid, client, nil
}

func siteFrom(ctx context.Context) string {
	v, _ := ctx.Value(ctxSite).(string)
	return v
}

func rolesFrom(ctx context.Context) []string {
	v, _ := ctx.Value(ctxRoles).([]string)
	return v
}

func thirdPartyFrom(ctx context.Context) bool {
	v, _ := ctx.Value(ctxThirdParty).(bool)
	return v
}

func requireSite(ctx context.Context) (string, error) {
	site := siteFrom(ctx)
	if site == "" {
		return "", problem.New(problem.CodeSiteNotBound, "", "", "the access token's client is not bound to a catalog site.")
	}
	return site, nil
}

func requireIfMatch(header, etag string) error {
	if strings.TrimSpace(header) == "" {
		p := problem.New(problem.CodePreconditionRequired, "", "", "this operation requires If-Match.")
		p.Errors = []problem.FieldError{{Header: "If-Match", Reason: problem.ReasonRequired, Detail: "send the current ETag"}}
		return p
	}
	if !protocol.IfMatch(header, etag) {
		return problem.New(problem.CodePreconditionFailed, "", "", "If-Match did not match the current representation.")
	}
	return nil
}

type UserIdentity struct {
	UID      int64
	ClientID string
	Roles    []string
	// The token's granted OAuth scopes. /v2/me and /v2/moderation gate on the
	// person, not on what the app was consented for, so nothing read this until
	// the catalog read surface began accepting user tokens.
	Scopes []string
}

// The catalog gate and this one must leave a request in the same state, or the
// rate limiter reads a user bucket on one face and the anonymous per-IP one on
// the other.
func applyUserIdentity(c fiber.Ctx, ident UserIdentity) {
	c.Locals("user_id", ident.UID)
	c.Locals("token_client_id", ident.ClientID)
	if len(ident.Roles) > 0 {
		c.Locals("user_roles", ident.Roles)
	}
}

func userAuth(lookup func(context.Context, string) (UserIdentity, error), lookupSite func(context.Context, string) (SiteBinding, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		// Same trap as catalogAuth: c.Path() is not what fiber matched on, so
		// GET /v2/Me/claims skipped this gate entirely.
		path := routepath.Normalize(c.Path())
		if !strings.HasPrefix(path, "/v2/me/") && !strings.HasPrefix(path, "/v2/moderation/") {
			return c.Next()
		}
		h := c.Get("Authorization")
		const pfx = "Bearer "
		if h == "" {
			return problem.WriteFiberError(c, problem.New(problem.CodeMissingCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is required."))
		}
		if !strings.HasPrefix(h, pfx) {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is invalid."))
		}
		token := strings.TrimSpace(h[len(pfx):])
		if token == "" {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"Authorization Bearer token is invalid."))
		}
		if devapi.HasKeyPrefix(token) {
			return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
				"this face requires a user access token, not an application key."))
		}
		if lookup != nil {
			ident, err := lookup(c.Context(), token)
			if err != nil {
				return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential, problem.RequestID(c), problem.Instance(c),
					"Authorization Bearer token is invalid."))
			}
			if ident.UID <= 0 {
				return problem.WriteFiberError(c, problem.New(problem.CodeUserIdentityRequired, problem.RequestID(c), problem.Instance(c),
					"this operation requires a user access token."))
			}
			applyUserIdentity(c, ident)
			if lookupSite != nil && ident.ClientID != "" {
				if bind, serr := lookupSite(c.Context(), ident.ClientID); serr == nil {
					if bind.Site != "" {
						c.Locals("catalog_site", bind.Site)
					}
					if bind.ThirdParty {
						c.Locals("catalog_third_party", true)
					}
				}
			}
		}
		return c.Next()
	}
}
