package handler

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"api/pkg/wireshape/wireshapetest"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

var (
	s2sOnce, adminOnce sync.Once
	s2sSpec, adminSpec *wireshapetest.Spec
)

func publishedSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	s2sOnce.Do(func() { s2sSpec = wireshapetest.Compile(t, Setup(fiber.New(), nil).OpenAPI(), "HouseError") })
	return s2sSpec
}

func publishedAdminSpec(t *testing.T) *wireshapetest.Spec {
	t.Helper()
	adminOnce.Do(func() {
		adminSpec = wireshapetest.Compile(t, SetupAdmin(fiber.New(), nil, nil).OpenAPI(), "HouseError")
	})
	return adminSpec
}

func send(t *testing.T, app *fiber.App, spec *wireshapetest.Spec, req *http.Request) (int, []byte) {
	t.Helper()
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	spec.Conforms(t, req.Method, req.URL.Path, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	return resp.StatusCode, raw
}

func TestAQuietWindowSendsEmptyLists(t *testing.T) {
	truncateUsage(t)
	app := buildAdminApp()
	for _, path := range []string{"/api/v1/admin/ai/usage/summary", "/api/v1/admin/ai/usage/daily"} {
		req := httptest.NewRequest("GET", path, nil)
		req.Header.Set("Authorization", "Bearer "+adminToken(t, "admin"))
		status, raw := send(t, app, publishedAdminSpec(t), req)
		require.Equal(t, fiber.StatusOK, status, path)
		require.NotContains(t, string(raw), "null", path)
	}
}
