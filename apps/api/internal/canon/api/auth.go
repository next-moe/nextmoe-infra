package api

import (
	"context"
	"strings"

	"api/pkg/utils"

	"github.com/danielgtaylor/huma/v2"
)

type Verifier interface {
	Parse(ctx context.Context, token string) (*utils.TokenClaims, error)
}

// reviewerRoles may settle todos, merge and take down. The finer permission
// registry lives in the legacy platform; canon starts from the role names.
var reviewerRoles = map[string]bool{"moderator": true, "admin": true, "ren": true}

type AuthInput struct {
	Authorization string `header:"Authorization" doc:"Bearer access token of a moderator, admin or ren."`
}

// reviewer returns the actor key ("user:<uuid>") of a reviewer token.
func (a *API) reviewer(ctx context.Context, header string) (string, error) {
	token, ok := strings.CutPrefix(header, "Bearer ")
	if !ok || a.verifier == nil {
		return "", huma.Error401Unauthorized("a bearer token is required")
	}
	claims, err := a.verifier.Parse(ctx, token)
	if err != nil {
		return "", huma.Error401Unauthorized("the token is invalid or expired")
	}
	for _, r := range claims.Roles {
		if reviewerRoles[r] {
			return "user:" + claims.UserUUID, nil
		}
	}
	return "", huma.Error403Forbidden("reviewing needs the moderator, admin or ren role")
}
