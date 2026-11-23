package api

import (
	"context"
	"strings"

	"api/internal/canon/perm"
	"api/internal/canon/registry"
	"api/internal/platform/authz"
	"api/pkg/utils"

	"github.com/danielgtaylor/huma/v2"
)

type Verifier interface {
	Parse(ctx context.Context, token string) (*utils.TokenClaims, error)
}

type AuthInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token; each operation names the canon permission it needs."`
}

type staff struct {
	key   string
	roles []string
}

func (s staff) can(p authz.Permission) bool { return perm.Holder.Can(s.roles, p) }

func (s staff) todoKinds() []string {
	var out []string
	for _, k := range registry.TodoKinds {
		if p, ok := perm.ForTodo(k); ok && s.can(p) {
			out = append(out, k)
		}
	}
	return out
}

// staffOf parses the token. Only global roles count: a site role is one
// product site's own moderation and does not reach the shared catalog.
func (a *API) staffOf(ctx context.Context, header string) (staff, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || a.verifier == nil {
		return staff{}, huma.Error401Unauthorized("a bearer token is required")
	}
	claims, err := a.verifier.Parse(ctx, token)
	if err != nil {
		return staff{}, huma.Error401Unauthorized("the token is invalid or expired")
	}
	return staff{key: "user:" + claims.UserUUID, roles: claims.Roles}, nil
}

func (a *API) require(ctx context.Context, header string, p authz.Permission) (staff, error) {
	s, err := a.staffOf(ctx, header)
	if err != nil {
		return s, err
	}
	if !s.can(p) {
		return s, forbidden(p)
	}
	return s, nil
}

func forbidden(p authz.Permission) error {
	return huma.Error403Forbidden("this needs the " + string(p) + " permission")
}
