package handler

import (
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/stretchr/testify/require"
)

// The public lane is a catalog read, not a folder one: folder:read is consent
// to read the bearer's own folders and says nothing about what its holder may
// see of other people. A token without it still reads /v2/folders.
func TestPublicFoldersTakeCatalogReadNotFolderRead(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", devapi.ScopeCatalogRead},
	})
	for _, path := range []string{"/v2/folders?owner_uid=1", "/v2/folders/7", "/v2/folders/7/items"} {
		status, p := authGET(t, app, path, catalogUserToken)
		require.Equal(t, 503, status, path)
		require.Equal(t, problem.CodeServiceUnavailable, p.Code, path)
	}
}

func TestPublicFoldersRefuseATokenWithoutCatalogRead(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", devapi.ScopeFolderRead, devapi.ScopeFolderWrite},
	})
	status, p := authGET(t, app, "/v2/folders?owner_uid=1", catalogUserToken)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
}

// owner_uid is the face's address, so its absence is a refusal rather than a
// platform-wide listing of everybody's public folders.
func TestPublicFolderListDemandsAnOwner(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{devapi.ScopeCatalogRead},
	})
	status, p := authGET(t, app, "/v2/folders", catalogUserToken)
	require.Equal(t, 400, status)
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
	require.NotEmpty(t, p.Errors)
	require.Equal(t, "owner_uid", p.Errors[0].Parameter)
}

// Moderation standing comes from the token's roles, and a folder's own owner
// has none of it — without this the two ops would be "any signed-in user may
// rewrite or delete any folder".
func TestModerationFoldersRefuseAPlainUser(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Roles: []string{"user"},
		Scopes: []string{"openid", devapi.ScopeFolderWrite},
	})
	status, p := authDo(t, app, "PATCH", "/v2/moderation/folders/7", catalogUserToken, `{"name":"x"}`)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodePermissionRequired, p.Code)

	status, p = authDo(t, app, "DELETE", "/v2/moderation/folders/7", catalogUserToken, "")
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodePermissionRequired, p.Code)
}

func TestModerationFoldersAdmitAModerator(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Roles: []string{"moderator"},
		Scopes: []string{"openid"},
	})
	status, p := authDo(t, app, "DELETE", "/v2/moderation/folders/7", catalogUserToken, "")
	require.Equal(t, 503, status, "past the gate; folders are unbound in a unit test")
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}
