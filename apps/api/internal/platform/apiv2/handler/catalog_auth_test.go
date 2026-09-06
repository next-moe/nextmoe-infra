package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func testAppLookup(t *testing.T, lookup func(context.Context, string) (*devapi.Credential, error)) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{LookupCredential: lookup})
	return app
}

func authGET(t *testing.T, app *fiber.App, path, token string) (int, problem.Problem) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	return resp.StatusCode, p
}

func mustV2Key(t *testing.T) string {
	t.Helper()
	k, err := devapi.GenerateV2Key(true)
	require.NoError(t, err)
	return k
}

func TestCatalogAuthLookupRejectsUnknownAndUnscopedKeys(t *testing.T) {
	sfw := mustV2Key(t)
	unscoped := mustV2Key(t)
	missing := mustV2Key(t)
	lookup := func(_ context.Context, raw string) (*devapi.Credential, error) {
		switch raw {
		case sfw:
			return &devapi.Credential{KeyID: 1, Scopes: []string{devapi.ScopeCatalogRead}}, nil
		case unscoped:
			return &devapi.Credential{KeyID: 2}, nil
		default:
			return nil, nil
		}
	}
	app := testAppLookup(t, lookup)

	status, p := authGET(t, app, "/v2/catalog/works", "not-a-key")
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works", "nm_live_legacy")
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works", missing)
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works", unscoped)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works?nsfw=true", sfw)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

func TestCatalogAuthLookupAllowsNSFWForAnyKey(t *testing.T) {
	plain := mustV2Key(t)
	lookup := func(_ context.Context, raw string) (*devapi.Credential, error) {
		if raw == plain {
			return &devapi.Credential{KeyID: 3, Scopes: []string{devapi.ScopeCatalogRead}}, nil
		}
		return nil, nil
	}
	app := testAppLookup(t, lookup)

	status, p := authGET(t, app, "/v2/catalog/works?nsfw=true", plain)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works/1?nsfw=true", plain)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works?nsfw=yes", plain)
	require.Equal(t, 400, status)
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
}

func TestCatalogAuthLookupStoreFailureIs503(t *testing.T) {
	app := testAppLookup(t, func(context.Context, string) (*devapi.Credential, error) {
		return nil, errors.New("redis down")
	})
	status, p := authGET(t, app, "/v2/catalog/works", mustV2Key(t))
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

func TestCatalogAuthStubServesNSFWWithoutACapability(t *testing.T) {
	app := testApp(t)
	status, p := authGET(t, app, "/v2/catalog/works?nsfw=true", testAPIKey)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

const catalogUserToken = "eyJ.user.access.token"

func testAppDualCredential(t *testing.T, ident UserIdentity) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	SetupWith(app, Options{
		LookupCredential: func(context.Context, string) (*devapi.Credential, error) { return nil, nil },
		LookupUser: func(_ context.Context, raw string) (UserIdentity, error) {
			if raw != catalogUserToken {
				return UserIdentity{}, os.ErrPermission
			}
			return ident, nil
		},
	})
	return app
}

// Passing the gate reads as 503 here, not 200: every face is unbound in a unit
// test, so the only safe signal is that the answer stopped being the gate's own
// — the same control the application-key cases above rely on.
func TestCatalogAuthTakesAUserTokenHoldingCatalogRead(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", devapi.ScopeCatalogRead},
	})

	status, p := authGET(t, app, "/v2/catalog/works", catalogUserToken)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works/1", catalogUserToken)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)

	status, p = authGET(t, app, "/v2/catalog/works", "some.other.token")
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)
}

func TestCatalogAuthRefusesAUserTokenWithoutCatalogRead(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", "profile", "playtime:read"},
	})
	status, p := authGET(t, app, "/v2/catalog/works", catalogUserToken)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
	require.Contains(t, p.Detail, devapi.ScopeCatalogRead)
}

// claim_events:read is operator-granted to an application, so the one catalog
// read that demands it takes no user token at all — not even one that somehow
// carries the string.
func TestCatalogAuthKeepsClaimEventsOnApplicationKeys(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager",
		Scopes: []string{devapi.ScopeCatalogRead, devapi.ScopeClaimEventsRead},
	})
	status, p := authGET(t, app, "/v2/catalog/claim-events", catalogUserToken)
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)

	// Positive control: the same token still reads the ordinary catalog faces,
	// so the refusal above is about that one path and not about user tokens.
	status, _ = authGET(t, app, "/v2/catalog/works", catalogUserToken)
	require.Equal(t, 503, status)
}

// The moderation claim states are an application's own per-site queue, resolved
// from catalogAuthz, which the user lane never sets — so they stay out of the
// vocabulary for a person's token rather than silently widening to every site.
func TestCatalogAuthUserTokenGetsNoModerationClaimStates(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{devapi.ScopeCatalogRead},
	})
	status, p := authGET(t, app, "/v2/catalog/works?claim_state=pending&site=kungal", catalogUserToken)
	require.Equal(t, 400, status)
	require.Equal(t, problem.CodeUnknownEnumValue, p.Code)
}

// /v2/store keeps the application key: nothing in the consent vocabulary grants
// store:read, so a user token there is not a credential at all.
func TestCatalogAuthStoreStaysApplicationKeyOnly(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager",
		Scopes: []string{devapi.ScopeCatalogRead, devapi.ScopeStoreRead},
	})
	status, p := authGET(t, app, "/v2/store/stats", catalogUserToken)
	require.Equal(t, 401, status)
	require.Equal(t, problem.CodeInvalidCredential, p.Code)
}
