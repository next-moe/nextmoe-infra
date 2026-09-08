package handler

import (
	"context"
	"testing"

	"api/internal/middleware"
	"api/internal/platform/editing"

	"github.com/stretchr/testify/require"
)

func trustCtx(roles []string, thirdParty bool) context.Context {
	ctx := context.WithValue(context.Background(), ctxRoles, roles)
	return context.WithValue(ctx, ctxThirdParty, thirdParty)
}

// v1's userEditActor is the reference: trusted iff the actor holds
// catalog.edit.trusted AND the token's client is not a developer-owned app.
// v2 kept neither half -- PolicyContext.TrustTier was set by nothing, and the
// mint lane read the permission without the third-party test.
func TestActsAsTrustedNeedsBothHalves(t *testing.T) {
	cases := []struct {
		name       string
		roles      []string
		thirdParty bool
		want       bool
	}{
		{"trusted role, first-party client", []string{"admin"}, false, true},
		{"trusted role, third-party client", []string{"admin"}, true, false},
		{"no trusted role, first-party client", []string{"user"}, false, false},
		{"no roles at all", nil, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := trustCtx(c.roles, c.thirdParty)
			require.Equal(t, c.want, actsAsTrusted(ctx))

			want := int16(0)
			if c.want {
				want = editing.TrustedTier
			}
			require.Equal(t, want, trustTier(ctx))
		})
	}
}

// A ProposeTrusted field is proposable by a trusted actor and by nobody else.
// With TrustTier unset this predicate answered false for everyone, which is
// letmoe's entire edit surface (editspec/work.go:57).
func TestProposeTrustedIsReachable(t *testing.T) {
	p := editing.Policy{Propose: editing.ProposeTrusted}

	trusted := editing.PolicyContext{TrustTier: trustTier(trustCtx([]string{"admin"}, false))}
	require.True(t, p.AllowsPropose(trusted), "a trusted first-party actor must be able to propose")

	for _, ctx := range []context.Context{
		trustCtx([]string{"admin"}, true),
		trustCtx([]string{"user"}, false),
	} {
		require.False(t, editing.Policy{Propose: editing.ProposeTrusted}.
			AllowsPropose(editing.PolicyContext{TrustTier: trustTier(ctx)}))
	}
}

// A site grant is a role. Reading claims.Roles alone made 82 production
// site-moderator grants invisible to /v2.
func TestSiteRolesJoinTheRoleSet(t *testing.T) {
	require.ElementsMatch(t, []string{"user", "moderator"},
		middleware.UnionRoles([]string{"user"}, []string{"moderator"}))
	require.ElementsMatch(t, []string{"user"},
		middleware.UnionRoles([]string{"user"}, []string{"user"}), "the union must not duplicate")
	require.ElementsMatch(t, []string{"user"}, middleware.UnionRoles([]string{"user"}, nil))
}
