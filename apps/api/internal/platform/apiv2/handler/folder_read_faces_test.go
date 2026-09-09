package handler

import (
	"context"
	"net/http"
	"os"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// One app whose key holds folder_holders:read and one whose key does not, plus
// a user token, so every arm of the holders gate is reachable from one fixture.
func holdersGateApp(t *testing.T, ident UserIdentity) (*fiber.App, string, string) {
	t.Helper()
	granted, plain := mustV2Key(t), mustV2Key(t)
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{
		Store: &liveUnlimitedStore{},
		LookupCredential: func(_ context.Context, raw string) (*devapi.Credential, error) {
			switch raw {
			case granted:
				return &devapi.Credential{KeyID: 1, ClientID: "fan-out", Tier: devapi.TierInternal,
					Scopes: []string{devapi.ScopeCatalogRead, devapi.ScopeFolderHoldersRead}}, nil
			case plain:
				return &devapi.Credential{KeyID: 2, ClientID: "some-app", Tier: devapi.TierInternal,
					Scopes: []string{devapi.ScopeCatalogRead}}, nil
			}
			return nil, nil
		},
		LookupUser: func(_ context.Context, raw string) (UserIdentity, error) {
			if raw != catalogUserToken {
				return UserIdentity{}, os.ErrPermission
			}
			return ident, nil
		},
	})
	return app, granted, plain
}

func TestFolderHoldersTakesOnlyAKeyHoldingTheScope(t *testing.T) {
	app, granted, plain := holdersGateApp(t, UserIdentity{
		UID: 42, ClientID: "manager",
		// The literal string on a user token buys nothing: folder_holders:read is
		// granted to an application, and there is no consent form carrying it.
		Scopes: []string{devapi.ScopeCatalogRead, devapi.ScopeFolderHoldersRead,
			devapi.ScopeFolderRead, devapi.ScopeFolderWrite},
	})

	status, p := authGET(t, app, "/v2/folders/holders?work_id=1", plain)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
	require.Contains(t, p.Detail, devapi.ScopeFolderHoldersRead)

	status, p = authGET(t, app, "/v2/folders/holders?work_id=1", catalogUserToken)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)

	// Positive control: the same fixture lets the granted key past the gate, so
	// the two refusals above are about the credential and not about the path.
	status, p = authGET(t, app, "/v2/folders/holders?work_id=1", granted)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	// And the user token still reads the public folder faces beside it.
	status, _ = authGET(t, app, "/v2/folders?owner_uid=1", catalogUserToken)
	require.Equal(t, http.StatusServiceUnavailable, status)
}

func TestFolderHoldersRefusesAnonymousAndPathVariants(t *testing.T) {
	app, granted, _ := holdersGateApp(t, UserIdentity{UID: 42, ClientID: "manager"})

	status, code := walkGet(t, app, "/v2/folders/holders?work_id=1", "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeMissingCredential, code)

	// The trailing-slash and case walk-arounds that once skipped the
	// claim-events scope check: the extra scope is keyed on the normalised path.
	for _, variant := range []string{
		"/v2/folders/holders/?work_id=1",
		"/v2/folders/holders//?work_id=1",
		"/v2/Folders/holders?work_id=1",
	} {
		status, p := authGET(t, app, variant, mustV2Key(t))
		require.Equal(t, http.StatusUnauthorized, status, variant)
		require.Equal(t, problem.CodeInvalidCredential, p.Code, variant)
	}
	status, _ = authGET(t, app, "/v2/folders/holders", granted)
	require.Equal(t, http.StatusBadRequest, status, "work_id is required")
}

func TestFolderHoldingsNeedsAUserTokenWithTheFolderScope(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app", Scopes: []string{"openid", "profile"},
	})
	status, p := authGET(t, app, "/v2/me/folders/holdings?work_ids=1", catalogUserToken)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)

	status, code := walkGet(t, app, "/v2/me/folders/holdings?work_ids=1", "")
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeMissingCredential, code)

	// An application key is not a person, and this face answers a person's own
	// private collections.
	status, p = authGET(t, app, "/v2/me/folders/holdings?work_ids=1", mustV2Key(t))
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)
}

func TestFolderHoldingsAdmitsEitherFolderScope(t *testing.T) {
	for _, scope := range []string{devapi.ScopeFolderRead, devapi.ScopeFolderWrite} {
		app := testAppDualCredential(t, UserIdentity{
			UID: 42, ClientID: "some-app", Scopes: []string{"openid", scope},
		})
		status, p := authGET(t, app, "/v2/me/folders/holdings?work_ids=1", catalogUserToken)
		require.Equal(t, http.StatusServiceUnavailable, status, scope)
		require.Equal(t, problem.CodeServiceUnavailable, p.Code, scope)
	}
}

// The preview and the purge stand on the same permission, and neither is on a
// scope plane: an app that never asked for a folder scope still cannot reach
// them, because they take a user token and the standing is the person's.
func TestModerationUserFoldersNeedsModerationStanding(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app", Roles: []string{"user"},
	})
	status, p := authGET(t, app, "/v2/moderation/users/7/folders", catalogUserToken)
	require.Equal(t, http.StatusForbidden, status)
	require.Equal(t, problem.CodePermissionRequired, p.Code)

	status, p = authGET(t, app, "/v2/moderation/users/7/folders", mustV2Key(t))
	require.Equal(t, http.StatusUnauthorized, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)

	moderator := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "some-app", Roles: []string{"moderator"},
	})
	status, p = authGET(t, moderator, "/v2/moderation/users/7/folders", catalogUserToken)
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}
