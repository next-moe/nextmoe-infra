package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"testing"

	"api/internal/platform/telemetry/alert"
	"api/internal/platform/telemetry/model"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

type recNotifier struct {
	mu   sync.Mutex
	sent []alert.Notification
}

func (r *recNotifier) Send(_ context.Context, n alert.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sent = append(r.sent, n)
	return nil
}

func TestAlertChannelsCRUD(t *testing.T) {
	truncateApps(t)
	app, _ := buildAdminApp()

	stt, raw := doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"email","target":"not-an-email"}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))

	stt, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"email","target":"Ops <ops@example.com>"}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))

	stt, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"telegram","target":"ops@example.com"}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))

	stt, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"email","target":"Ops.Oncall@Example.COM"}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	var created struct {
		Data dtoChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Equal(t, "ops.oncall@example.com", created.Data.Target)
	require.True(t, created.Data.Enabled)
	id := created.Data.ID

	stt, raw = doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"email","target":"ops.oncall@example.com"}`)
	require.Equal(t, fiber.StatusConflict, stt, string(raw))

	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/alert-channels/"+strconv.FormatInt(id, 10), "ren",
		`{"target":"alerts@example.com","enabled":false}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), "alerts@example.com")
	require.Contains(t, string(raw), `"enabled":false`)

	stt, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/alert-channels", "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), "alerts@example.com")

	stt, raw = doJSON(t, app, "DELETE", "/api/v1/admin/telemetry/alert-channels/"+strconv.FormatInt(id, 10), "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))

	stt, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/alert-channels", "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.NotContains(t, string(raw), "alerts@example.com")

	stt, raw = doJSON(t, app, "DELETE", "/api/v1/admin/telemetry/alert-channels/999999", "ren", "")
	require.Equal(t, fiber.StatusNotFound, stt, string(raw))
}

type dtoChannel struct {
	ID      int64  `json:"id"`
	Target  string `json:"target"`
	Enabled bool   `json:"enabled"`
}

func TestAlertChannelTest(t *testing.T) {
	truncateApps(t)
	note := &recNotifier{}
	app, _ := buildAdminAppNotify(note)

	stt, raw := doJSON(t, app, "POST", "/api/v1/admin/telemetry/alert-channels", "ren",
		`{"kind":"email","target":"ops@example.com"}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	var created struct {
		Data dtoChannel `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	path := "/api/v1/admin/telemetry/alert-channels/" + strconv.FormatInt(created.Data.ID, 10) + "/test"

	stt, raw = doJSON(t, app, "POST", path, "admin", "")
	require.Equal(t, fiber.StatusForbidden, stt, string(raw))

	stt, raw = doJSON(t, app, "POST", path, "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), `"ok":true`)
	note.mu.Lock()
	defer note.mu.Unlock()
	require.Len(t, note.sent, 1)
	require.Equal(t, "ops@example.com", note.sent[0].To)
}

func TestListAlerts(t *testing.T) {
	truncateApps(t)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_alert
		(app_id, rule, subject_key, urgency, title, facts, status, attempts, last_error, created_at)
		VALUES (1, 'crash_rate', 'a', 'immediate', 't', '{}', 'queued', 0, '', '2026-09-29 12:00:00+00'),
		       (1, 'anr_rate', 'b', 'immediate', 't', '{}', 'sent', 0, '', '2026-09-29 11:00:00+00'),
		       (2, 'silent_app', 'c', 'immediate', 't', '{}', 'queued', 0, '', '2026-09-29 13:00:00+00')`).Error)

	app, _ := buildAdminApp()
	stt, raw := doJSON(t, app, "GET", "/api/v1/admin/telemetry/alerts", "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	var env struct {
		Data []struct {
			Rule       string `json:"rule"`
			SubjectKey string `json:"subject_key"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Len(t, env.Data, 3)
	require.Equal(t, "c", env.Data[0].SubjectKey)

	stt, raw = doJSON(t, app, "GET", "/api/v1/admin/telemetry/alerts?app_id=1&status=queued", "ren", "")
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Len(t, env.Data, 1)
	require.Equal(t, "crash_rate", env.Data[0].Rule)
}

func TestAlertSettingsPatch(t *testing.T) {
	truncateApps(t)
	app, _ := buildAdminApp()
	stt, raw := doJSON(t, app, "POST", "/api/v1/admin/telemetry/apps", "ren",
		`{"service_name":"kungal-app","display_name":"KUN"}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), `"crash_rate":0.0109`)
	require.Contains(t, string(raw), `"anr_rate":0.0047`)
	require.Contains(t, string(raw), `"min_sessions":200`)
	var created struct {
		Data struct {
			ID            int64               `json:"id"`
			AlertSettings model.AlertSettings `json:"alert_settings"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	require.Equal(t, 0.0109, created.Data.AlertSettings.CrashRate)
	id := strconv.FormatInt(created.Data.ID, 10)

	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+id, "ren",
		`{"alert_settings":{}}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), `"crash_rate":0.0109`)

	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+id, "ren",
		`{"alert_settings":{"crash_rate":0.02,"min_sessions":500}}`)
	require.Equal(t, fiber.StatusOK, stt, string(raw))
	require.Contains(t, string(raw), `"crash_rate":0.02`)
	require.Contains(t, string(raw), `"min_sessions":500`)
	require.Contains(t, string(raw), `"anr_rate":0.0047`)

	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+id, "ren",
		`{"alert_settings":{"crash_rate":0}}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))
	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+id, "ren",
		`{"alert_settings":{"crash_rate":1}}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))
	stt, raw = doJSON(t, app, "PATCH", "/api/v1/admin/telemetry/apps/"+id, "ren",
		`{"alert_settings":{"min_sessions":0}}`)
	require.Equal(t, fiber.StatusUnprocessableEntity, stt, string(raw))
}
