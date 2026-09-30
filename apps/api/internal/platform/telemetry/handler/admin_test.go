package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"api/internal/middleware"
	telemetryPerm "api/internal/platform/telemetry/perm"
	"api/pkg/oidctoken"
	"api/pkg/utils"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

const adminTestSecret = "telemetry-admin-test-secret"

type recordingInv struct{ n int }

func (r *recordingInv) Invalidate() error { r.n++; return nil }

func buildAdminApp() (*fiber.App, *recordingInv) {
	inv := &recordingInv{}
	app := fiber.New()
	verifier := oidctoken.NewVerifier(adminTestSecret, nil)
	app.Use("/api/v1/admin/telemetry",
		middleware.JWTAuth(verifier),
		middleware.RequirePermission(telemetryPerm.Resolver, telemetryPerm.View))
	SetupAdmin(app, st, inv)
	return app, inv
}

func adminToken(t *testing.T, roles ...string) string {
	t.Helper()
	tok, err := utils.GenerateAccessToken(adminTestSecret, utils.TokenClaims{ID: 1, Roles: roles}, time.Hour)
	require.NoError(t, err)
	return tok
}

func doJSON(t *testing.T, app *fiber.App, method, path, role, body string) (int, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+adminToken(t, role))
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, raw
}

func truncateApps(t *testing.T) {
	t.Helper()
	require.NoError(t, testDB.Exec(`TRUNCATE telemetry_event, telemetry_session, telemetry_daily_metric, telemetry_app RESTART IDENTITY CASCADE`).Error)
}

func TestAppsCRUD(t *testing.T) {
	truncateApps(t)
	app, inv := buildAdminApp()

	st, raw := doJSON(t, app, "POST", "/api/v1/admin/telemetry/apps", "ren", `{"service_name":"kungal-app","display_name":"KUN"}`)
	require.Equal(t, fiber.StatusOK, st, string(raw))
	require.Greater(t, inv.n, 0)

	var created struct {
		Data struct {
			ID          int64  `json:"id"`
			ServiceName string `json:"service_name"`
			DisplayName string `json:"display_name"`
			IngestKey   string `json:"ingest_key"`
			Enabled     bool   `json:"enabled"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Equal(t, "kungal-app", created.Data.ServiceName)
	require.Len(t, created.Data.IngestKey, 32)
	require.True(t, created.Data.Enabled)
	id := created.Data.ID

	st, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/apps", "ren", "")
	require.Equal(t, fiber.StatusOK, st, string(raw))
	require.Contains(t, string(raw), created.Data.IngestKey)

	st, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+strconv.FormatInt(id, 10), "ren", `{"display_name":"KUN Gal","enabled":false}`)
	require.Equal(t, fiber.StatusOK, st, string(raw))
	require.Contains(t, string(raw), `"enabled":false`)
	require.Contains(t, string(raw), "KUN Gal")

	oldKey := created.Data.IngestKey
	st, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/apps/"+strconv.FormatInt(id, 10)+"/rotate-key", "ren", "")
	require.Equal(t, fiber.StatusOK, st, string(raw))
	require.NotContains(t, string(raw), oldKey)

	st, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/apps", "ren", `{"service_name":"kungal-app","display_name":"dup"}`)
	require.Equal(t, fiber.StatusConflict, st, string(raw))

	st, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/999999", "ren", `{"enabled":true}`)
	require.Equal(t, fiber.StatusNotFound, st, string(raw))
}

func TestManageRequired(t *testing.T) {
	truncateApps(t)
	app, _ := buildAdminApp()
	writes := []struct{ method, path, body string }{
		{"POST", "/api/v1/admin/telemetry/apps", `{"service_name":"other-app","display_name":"x"}`},
		{"PATCH", "/api/v1/admin/telemetry/apps/1", `{"enabled":false}`},
		{"POST", "/api/v1/admin/telemetry/apps/1/rotate-key", ""},
	}
	for _, w := range writes {
		st, raw := doJSON(t, app, w.method, w.path, "admin", w.body)
		require.Equal(t, fiber.StatusForbidden, st, "%s %s %s", w.method, w.path, raw)
	}

	inner := fiber.New()
	verifier := oidctoken.NewVerifier(adminTestSecret, nil)
	inner.Use("/api/v1/admin/telemetry", middleware.JWTAuth(verifier))
	SetupAdmin(inner, st, &recordingInv{})
	for _, w := range writes {
		st, raw := doJSON(t, inner, w.method, w.path, "admin", w.body)
		require.Equal(t, fiber.StatusForbidden, st, "inner %s %s %s", w.method, w.path, raw)
	}
}

func TestDailyMetricsValidation(t *testing.T) {
	truncateApps(t)
	app, _ := buildAdminApp()
	st, raw := doJSON(t, app, "GET", "/api/v1/admin/telemetry/metrics/daily?app_id=1&from=2026-09-02&to=2026-09-01", "ren", "")
	require.Equal(t, fiber.StatusUnprocessableEntity, st, string(raw))

	st, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/metrics/daily?app_id=1&from=2020-01-01&to=2021-03-01", "ren", "")
	require.Equal(t, fiber.StatusUnprocessableEntity, st, string(raw))

	st, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/metrics/daily", "ren", "")
	require.Equal(t, fiber.StatusUnprocessableEntity, st, string(raw))
}

func TestDailyMetricsRates(t *testing.T) {
	truncateApps(t)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_app (service_name, display_name, ingest_key, enabled, created_at, updated_at)
		VALUES ('kungal-app','KUN','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', true, now(), now())`).Error)
	var appID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_app`).Scan(&appID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_daily_metric (
		app_id, service_version, environment, day, sessions, crashed_sessions, anr_sessions,
		unhandled_sessions, abnormal_sessions, exceptions, crash_java, crash_native, crash_anr,
		startup_count, jank_frames_over, jank_frames_total, updated_at)
		VALUES (?, '1.0.0', 'direct', '2026-09-29', 4, 1, 1, 1, 0, 2, 1, 0, 0, 3, 10, 100, now()),
		       (?, '1.0.0', 'direct', '2026-09-28', 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, now())`, appID, appID).Error)

	app, _ := buildAdminApp()
	st, raw := doJSON(t, app, "GET", "/api/v1/admin/telemetry/metrics/daily?app_id="+strconv.FormatInt(appID, 10)+"&from=2026-09-01&to=2026-09-29", "ren", "")
	require.Equal(t, fiber.StatusOK, st, string(raw))
	var env struct {
		Data []struct {
			Day           string   `json:"day"`
			CrashRate     *float64 `json:"crash_rate"`
			ANRRate       *float64 `json:"anr_rate"`
			UnhandledRate *float64 `json:"unhandled_rate"`
			JankRatio     *float64 `json:"jank_ratio"`
			Sessions      int      `json:"sessions"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Len(t, env.Data, 2)
	require.Equal(t, "2026-09-29", env.Data[0].Day)
	require.NotNil(t, env.Data[0].CrashRate)
	require.InDelta(t, 0.25, *env.Data[0].CrashRate, 1e-9)
	require.InDelta(t, 0.25, *env.Data[0].ANRRate, 1e-9)
	require.InDelta(t, 0.25, *env.Data[0].UnhandledRate, 1e-9)
	require.InDelta(t, 0.1, *env.Data[0].JankRatio, 1e-9)
	require.Nil(t, env.Data[1].CrashRate)
	require.Nil(t, env.Data[1].JankRatio)
}
