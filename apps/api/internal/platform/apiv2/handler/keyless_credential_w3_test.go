package handler

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"api/internal/platform/settings/keys"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func w3RateHeaders(t *testing.T, app *fiber.App, path, token string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := app.Test(req)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header.Get("X-RateLimit-Limit")
}

// The keyless lanes returned before resolving anything, so a key sent to them
// was thrown away and its holder counted into the shared anonymous per-IP
// bucket — the #92 outage shape one layer up, where a first-party backend's
// whole user plane shared one 10k/day quota behind one egress IP. The test key
// is an internal-tier credential, which is unlimited, so "the limiter saw the
// key" and "the limiter saw an anonymous IP" are two different headers rather
// than two numbers that could coincide.
func TestKeylessLanesStillCountTheApplicationKey(t *testing.T) {
	app := testApp(t)
	anonRate := strconv.Itoa(int(keys.APIV2AnonRatePerMinute.Get()))
	for _, path := range []string{
		"/v2/vocabularies", "/v2/problems", "/v2/catalog/stats", "/v2/catalog/schemas/work", "/v2/catalog/works",
	} {
		status, anon := w3RateHeaders(t, app, path, "")
		require.NotEqual(t, 401, status, "%s must stay keyless: %d", path, status)
		require.Equalf(t, anonRate, anon, "%s without a key falls in the anonymous bucket", path)

		_, keyed := w3RateHeaders(t, app, path, testAPIKey)
		require.Emptyf(t, keyed, "%s with an unlimited key must not be metered per IP", path)
	}
	// Control: a bad bearer on a keyless lane stays anonymous rather than
	// becoming a refusal — these lanes demand no credential.
	status, anon := w3RateHeaders(t, app, "/v2/vocabularies", "nmk_not_a_real_key")
	require.Equal(t, 200, status)
	require.Equal(t, anonRate, anon)
}
