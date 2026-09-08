package handler

import (
	"net/http"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/stretchr/testify/require"
)

// Wave R3 deleted v1's UserGate, which demanded catalog:edit over the whole
// /api/v1/user/catalog prefix, and carried nothing to v2. From that day a token
// holding only `openid profile` reached POST /v2/me/proposals and its edit
// merged, while pkce.ts on both sites had been asking for the scope all along.
func TestEditingPlaneRefusesATokenWithoutCatalogEdit(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app",
		Scopes: []string{"openid", "profile", devapi.ScopeFolderWrite},
	})
	for _, path := range []string{
		"/v2/me/proposals",
		"/v2/me/proposals/7",
		"/v2/me/claims",
		"/v2/me/claims/7",
		"/v2/me/cover-votes",
		"/v2/moderation/proposals",
		"/v2/moderation/proposals/7",
		"/v2/moderation/claims",
		"/v2/moderation/snapshots/work/7",
	} {
		status, p := authGET(t, app, path, catalogUserToken)
		require.Equal(t, http.StatusForbidden, status, path)
		require.Equal(t, problem.CodeScopeRequired, p.Code, path)
	}
}

// The same trailing-slash and case variants that once walked around the
// claim-events and folder gates.
func TestEditingPlaneScopeGateSurvivesPathVariants(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app", Scopes: []string{"openid", "profile"},
	})
	for _, path := range []string{
		"/v2/me/proposals/",
		"/v2/me/proposals//",
		"/v2/Me/proposals",
		"/v2/moderation/reverts/",
	} {
		_, p := authGET(t, app, path, catalogUserToken)
		require.Equal(t, problem.CodeScopeRequired, p.Code, path)
	}
}

// Positive control: with the scope the gate is transparent, so a green run
// above cannot mean "everything on this plane 403s".
func TestEditingPlaneAdmitsCatalogEdit(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app",
		Scopes: []string{"openid", "profile", devapi.ScopeCatalogEdit},
	})
	for _, path := range []string{"/v2/me/proposals", "/v2/moderation/proposals"} {
		_, p := authGET(t, app, path, catalogUserToken)
		require.NotEqual(t, problem.CodeScopeRequired, p.Code, path)
	}
}

// Negative control: catalog:edit must not leak onto the neighbouring faces.
// Folder moderation in particular belongs to the folder domain -- gating it
// here would demand a consent that has nothing to do with folders.
func TestEditingPlaneScopeLeavesItsNeighboursAlone(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app", Scopes: []string{"openid", "profile"},
	})
	for _, path := range []string{
		"/v2/me/playtimes",
		"/v2/me/news",
		"/v2/moderation/folders/7",
		"/v2/moderation/users/7/folders",
	} {
		_, p := authGET(t, app, path, catalogUserToken)
		require.NotEqual(t, problem.CodeScopeRequired, p.Code, path)
	}
}

// Person-gated by design: /v2/me and /v2/moderation gate on who you are, not on
// what the app was consented for. Folders and the editing plane are the two
// exceptions. This list is the other half of that statement, so a path added to
// the surface has to be classified deliberately instead of defaulting into the
// ungated majority.
var personGatedMePrefixes = []string{
	"/v2/me/news",
	"/v2/me/playtimes",
	"/v2/moderation/folders",
	"/v2/moderation/users",
}

func TestEveryMeAndModerationPathDeclaresItsScope(t *testing.T) {
	classified := 0
	for _, specPath := range specV2Paths(t) {
		p := strings.ToLower(specPath)
		if !strings.HasPrefix(p, "/v2/me/") && !strings.HasPrefix(p, "/v2/moderation/") {
			continue
		}
		classified++

		hits := 0
		if underPrefix(p, "/v2/me/folders") {
			hits++
		}
		for _, prefix := range editingPlanePrefixes {
			if underPrefix(p, prefix) {
				hits++
			}
		}
		for _, prefix := range personGatedMePrefixes {
			if underPrefix(p, prefix) {
				hits++
			}
		}
		require.Equalf(t, 1, hits,
			"%s matches %d scope classes; every /v2/me and /v2/moderation path must match exactly one "+
				"(folders, editingPlanePrefixes, or personGatedMePrefixes)", specPath, hits)
	}
	require.Greater(t, classified, 20, "the walk classified too few paths to mean anything")
}
