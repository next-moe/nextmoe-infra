package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/app"
	"api/internal/galgameapp"
	v2handler "api/internal/platform/apiv2/handler"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// retiredV1Paths is the census: every family the wave R3 teardown removed, with
// at least one deep path and each face's former openapi.json.
var retiredV1Paths = []string{
	// public /v1/catalog
	"/v1/catalog",
	"/v1/catalog/stats",
	"/v1/catalog/works",
	"/v1/catalog/works/search?q=x",
	"/v1/catalog/works/1",
	"/v1/catalog/works/1/ratings",
	"/v1/catalog/lookup?vndb_id=v17",
	"/v1/catalog/labels/1/relation-graph",
	"/v1/catalog/calendar/pending",
	"/v1/catalog/openapi.json",
	// /v1/news
	"/v1/news",
	"/v1/news/sources",
	"/v1/news/1",
	"/v1/news/openapi.json",
	// /v1/store
	"/v1/store",
	"/v1/store/purchase-links/RJ01000000",
	"/v1/store/me/stats",
	"/v1/store/openapi.json",
	// /v1/playtime
	"/v1/playtime",
	"/v1/playtime/mine",
	"/v1/playtime/works/1",
	"/v1/playtime/by-ref/vndb/v17",
	"/v1/playtime/batch",
	// S2S /api/v1/catalog
	"/api/v1/catalog",
	"/api/v1/catalog/resolve",
	"/api/v1/catalog/redirects",
	"/api/v1/catalog/works/claim",
	"/api/v1/catalog/works/1/credits",
	"/api/v1/catalog/claim-events/feed",
	"/api/v1/catalog/edit-revisions/feed",
	"/api/v1/catalog/edit/proposals",
	"/api/v1/catalog/users/5/claims",
	// user /api/v1/user/catalog
	"/api/v1/user/catalog",
	"/api/v1/user/catalog/claims/mine",
	"/api/v1/user/catalog/edit/proposals",
	"/api/v1/user/catalog/edit/images",
	"/api/v1/user/catalog/works/1/cover-votes",
	// a path no face ever served, inside a retired prefix
	"/api/v1/user/catalog/whatever/deeply/nested",
}

// retiredApp reproduces cmd/catalog's registration ORDER: the admin gate and the
// v2 face first, the tombstones last. Fiber matches in registration order, so a
// tombstone mounted earlier would answer 410 for routes that are still alive.
func retiredApp() *fiber.App {
	f := fiber.New()

	f.Use("/api/v1/admin/catalog", AdminGate(userEditClients()))
	SetupAdmin(f, nil, nil, nil, nil)

	v2handler.SetupWith(f, v2handler.Options{})

	galgameapp.MountRetiredPublic(&app.App{Fiber: f})
	MountRetiredV1(f)
	return f
}

func retiredCall(t *testing.T, f *fiber.App, method, path, auth string) (int, string, string) {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	if auth != "" {
		r.Header.Set("Authorization", auth)
	}
	resp, err := f.Test(r, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Link"), string(raw)
}

func TestRetiredV1FacesAreGone(t *testing.T) {
	f := retiredApp()

	for _, path := range retiredV1Paths {
		for _, method := range []string{"GET", "POST"} {
			for _, auth := range []string{"", "Bearer nmk_live_0000000000000000", "Basic Y2xpZW50OnNlY3JldA=="} {
				status, link, body := retiredCall(t, f, method, path, auth)
				require.Equalf(t, fiber.StatusGone, status, "%s %s (auth %q) must be 410 Gone: %s", method, path, auth, body)
				require.Equalf(t, retiredSuccessorLink, link, "%s %s must carry the successor Link", method, path)

				var env map[string]any
				require.NoErrorf(t, json.Unmarshal([]byte(body), &env), "%s %s did not return JSON: %s", method, path, body)
				require.EqualValuesf(t, errors.ErrGone, env["code"], "%s %s must carry the ErrGone code", method, path)
				require.NotContainsf(t, env, "data", "%s %s must not carry a data block", method, path)
				msg, _ := env["message"].(string)
				require.Containsf(t, msg, "2026-08-27", "%s %s must state the retirement date", method, path)
				require.Containsf(t, msg, "https://api.nextmoe.dev/v2/catalog/openapi.json", "%s %s must name the v2 spec", method, path)
				require.Containsf(t, msg, "https://developer.nextmoe.dev", "%s %s must link the docs", method, path)
			}
		}
	}
}

func TestRetiredV1IgnoresMethod(t *testing.T) {
	f := retiredApp()
	for _, m := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		status, _, _ := retiredCall(t, f, m, "/v1/catalog/works/1", "")
		require.Equalf(t, fiber.StatusGone, status, "%s must also be 410", m)
	}
}

func TestRetiredV1AnnouncesNoPendingDeprecation(t *testing.T) {
	f := retiredApp()
	r := httptest.NewRequest("GET", "/v1/catalog/works/1", nil)
	resp, err := f.Test(r, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)
	require.Empty(t, resp.Header.Get("Deprecation"), "the face is gone, not deprecating")
	require.Empty(t, resp.Header.Get("Sunset"), "the face is gone, not sunsetting")
}

// TestRetiredV1DoesNotCaptureLiveSiblings is the reason the tombstones are
// mounted last: /api/v1/catalog and /api/v1/admin/catalog share a prefix root,
// and /v2 is registered by the same process.
func TestRetiredV1DoesNotCaptureLiveSiblings(t *testing.T) {
	f := retiredApp()

	for _, tc := range []struct {
		path string
		want int
	}{
		{"/v2/catalog/claim-events", fiber.StatusUnauthorized},
		{"/v2/store/stats", fiber.StatusUnauthorized},
		// Anonymous since spec 2.39.0: past the gate to its unbound handler, not a tombstone.
		{"/v2/catalog/works", fiber.StatusServiceUnavailable},
	} {
		status, _, body := retiredCall(t, f, "GET", tc.path, "")
		require.Equalf(t, tc.want, status, "GET %s must still reach the v2 key gate: %s", tc.path, body)
		require.Containsf(t, body, "\"type\"", "GET %s must answer RFC 9457 problem+json, not the house envelope", tc.path)
	}

	// The admin console is a live consumer; its gate, not a tombstone, must answer.
	status, _, body := retiredCall(t, f, "GET", "/api/v1/admin/catalog/candidates", "")
	require.NotEqual(t, fiber.StatusGone, status, "the admin face must not be captured by the /api/v1/catalog tombstone")
	require.Equal(t, fiber.StatusForbidden, status, "an unauthenticated admin call reaches the permission gate: "+body)

	status, _, _ = retiredCall(t, f, "GET", "/api/v1/admin/catalog/claims/pending", "")
	require.Equal(t, fiber.StatusForbidden, status)
}

func TestRetiredV1StaysInsideItsPrefixes(t *testing.T) {
	f := fiber.New()
	MountRetiredV1(f)
	ok := func(c fiber.Ctx) error { return c.SendString("survivor") }
	survivors := []string{
		"/v1/catalogue",
		"/v1/newsroom",
		"/v1/stores",
		"/v1/playtimes",
		"/api/v1/admin/catalog/candidates",
		"/api/v1/user/catalogue",
		"/api/v1/catalogs",
		"/v2/catalog/works",
	}
	for _, p := range survivors {
		f.Get(p, ok)
	}
	for _, p := range survivors {
		r := httptest.NewRequest("GET", p, nil)
		resp, err := f.Test(r, fiber.TestConfig{Timeout: 5 * time.Second})
		require.NoError(t, err)
		raw, _ := io.ReadAll(resp.Body)
		require.Equalf(t, fiber.StatusOK, resp.StatusCode, "surviving route %s must not be captured by the 410 catch-all", p)
		require.Equalf(t, "survivor", strings.TrimSpace(string(raw)), "surviving route %s must reach its own handler", p)
	}
}

// The galgame tombstone predates this wave and keeps its own message; wave R3
// only re-pointed its successor away from /v1/catalog, which is itself a
// tombstone now.
func TestGalgameTombstoneStillAnswersWithItsOwnMessage(t *testing.T) {
	f := retiredApp()
	status, link, body := retiredCall(t, f, "GET", "/v1/galgame", "")
	require.Equal(t, fiber.StatusGone, status)
	require.Equal(t, `<https://api.nextmoe.dev/v2>; rel="successor-version"`, link)
	require.Contains(t, body, "/v1/galgame")
	require.Contains(t, body, "2026-07-30")
	require.NotContains(t, body, "2026-08-27", "the galgame face kept its own retirement date")
}
