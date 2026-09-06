package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/devapi"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func authDo(t *testing.T, app *fiber.App, method, path, token, body string) (int, problem.Problem) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p), string(raw))
	return resp.StatusCode, p
}

// Passing the gate reads as 503 in a unit test (folders are unbound), the same
// control the catalog dual-credential cases rely on.
func TestFolderReadsTakeFolderRead(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", devapi.ScopeFolderRead},
	})
	status, p := authGET(t, app, "/v2/me/folders", catalogUserToken)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

func TestFolderReadsTakeFolderWriteAlone(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{devapi.ScopeFolderWrite},
	})
	status, p := authGET(t, app, "/v2/me/folders/7/items", catalogUserToken)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

func TestFolderReadsRefuseAScopelessToken(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid", "playtime:read", devapi.ScopeCatalogRead},
	})
	status, p := authGET(t, app, "/v2/me/folders", catalogUserToken)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
	require.Contains(t, p.Detail, devapi.ScopeFolderRead)
}

func TestFolderWritesRefuseFolderReadAlone(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{devapi.ScopeFolderRead},
	})
	status, p := authDo(t, app, http.MethodPut, "/v2/me/folders/7/items/9", catalogUserToken, "")
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
	require.Contains(t, p.Detail, devapi.ScopeFolderWrite)

	status, p = authDo(t, app, http.MethodPost, "/v2/me/folders", catalogUserToken, `{"name":"x"}`)
	require.Equal(t, 403, status)
	require.Equal(t, problem.CodeScopeRequired, p.Code)
}

func TestFolderWritesTakeFolderWrite(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{devapi.ScopeFolderWrite},
	})
	status, p := authDo(t, app, http.MethodPost, "/v2/me/folders", catalogUserToken, `{"name":"收藏"}`)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

// The folder gate must not leak onto the rest of /v2/me, which stays on the
// "any app may call this" convention.
func TestFolderScopeGateLeavesPlaytimesAlone(t *testing.T) {
	app := testAppDualCredential(t, UserIdentity{
		UID: 42, ClientID: "manager", Scopes: []string{"openid"},
	})
	status, p := authGET(t, app, "/v2/me/playtimes", catalogUserToken)
	require.Equal(t, 503, status)
	require.Equal(t, problem.CodeServiceUnavailable, p.Code)
}

func TestFolderRoutesRequireAUserToken(t *testing.T) {
	app := testApp(t)
	status, _, body := do(t, app, http.MethodGet, "/v2/me/folders")
	require.Equal(t, 401, status)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p), string(body))
	require.Equal(t, problem.CodeMissingCredential, p.Code)
}
