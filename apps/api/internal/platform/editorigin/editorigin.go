// Package editorigin is attribution only. Permissions, quotas, rewards and
// tenant fencing read the token's catalog site, never this.
package editorigin

import (
	"context"
	"slices"
)

const (
	Kungal  = "kungal"
	Moyu    = "moyu"
	Letmoe  = "letmoe"
	Nextmoe = "nextmoe"
	Editor  = "editor"
)

const Header = "X-NextMoe-Origin-Site"

var sites = []string{Kungal, Moyu, Letmoe, Nextmoe, Editor}

func Sites() []string { return slices.Clone(sites) }

func Valid(s string) bool { return slices.Contains(sites, s) }

// OfClient is the origin of an edit filed with a client's token: the client's
// community tenant, the property it belongs to. moyu and the forum share
// catalog_site=kungal, so catalog_site alone cannot tell them apart.
func OfClient(communityTenant string) string {
	if Valid(communityTenant) {
		return communityTenant
	}
	return ""
}

func Resolve(clientOrigin string, mayDeclare bool, declared string) string {
	if mayDeclare && Valid(declared) {
		return declared
	}
	return clientOrigin
}

type ctxKey struct{}

func Key() any { return ctxKey{} }

func With(ctx context.Context, site string) context.Context {
	return context.WithValue(ctx, ctxKey{}, site)
}

func From(ctx context.Context) string {
	s, _ := ctx.Value(ctxKey{}).(string)
	if Valid(s) {
		return s
	}
	return ""
}

func Ptr(ctx context.Context) *string {
	if s := From(ctx); s != "" {
		return &s
	}
	return nil
}
