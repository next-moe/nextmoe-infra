package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestListIssues(t *testing.T) {
	truncateApps(t)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_app (service_name, display_name, ingest_key, enabled, in_app_prefixes, created_at, updated_at)
		VALUES ('kungal-app','KUN','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', true, '[]', now(), now())`).Error)
	var appID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_app`).Scan(&appID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_issue (
		app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
		first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at)
		VALUES (?, 'fp-a', 'exception', 'A', 'c', 'open', '', false, '2026-09-20', '2026-09-29', '1.0', '1.1', now(), now()),
		       (?, 'fp-b', 'java', 'B', 'c', 'open', '', false, '2026-09-21', '2026-09-28', '1.0', '1.0', now(), now()),
		       (?, 'fp-c', 'exception', 'C', 'c', 'resolved', '', false, '2026-09-22', '2026-09-27', '1.0', '1.0', now(), now())`,
		appID, appID, appID).Error)
	var ids []int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_issue ORDER BY title`).Scan(&ids).Error)
	require.Len(t, ids, 3)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_issue_daily (issue_id, day, service_version, events, sessions) VALUES
		(?, '2026-09-29', '1.1', 5, 2),
		(?, '2026-09-28', '1.0', 1, 1),
		(?, '2026-09-28', '1.0', 3, 1),
		(?, '2026-09-27', '1.0', 9, 1)`,
		ids[0], ids[0], ids[1], ids[2]).Error)

	app, _ := buildAdminApp()
	q := "/api/v1/admin/telemetry/issues?app_id=" + strconv.FormatInt(appID, 10) + "&from=2026-09-16&to=2026-09-29"
	stt, raw := doJSON(t, app, "GET", q, "ren", "")
	require.Equal(t, 200, stt, string(raw))
	var env struct {
		Data []struct {
			Title    string `json:"title"`
			Kind     string `json:"kind"`
			Events   int    `json:"events"`
			Sessions int    `json:"sessions"`
			Series   []struct {
				Day    string `json:"day"`
				Events int    `json:"events"`
			} `json:"series"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Len(t, env.Data, 2)
	require.Equal(t, "A", env.Data[0].Title)
	require.Equal(t, 6, env.Data[0].Events)
	require.Equal(t, 3, env.Data[0].Sessions)
	require.Equal(t, "B", env.Data[1].Title)
	require.Equal(t, 3, env.Data[1].Events)
	require.Greater(t, len(env.Data[0].Series), 10)

	stt, raw = doJSON(t, app, "GET", q+"&kind=java", "ren", "")
	require.Equal(t, 200, stt, string(raw))
	require.Contains(t, string(raw), `"title":"B"`)
	require.NotContains(t, string(raw), `"title":"A"`)

	stt, raw = doJSON(t, app, "GET", q+"&status=resolved", "ren", "")
	require.Equal(t, 200, stt, string(raw))
	require.Contains(t, string(raw), `"title":"C"`)

	stt, raw = doJSON(t, app, "GET", q+"&version=1.1", "ren", "")
	require.Equal(t, 200, stt, string(raw))
	require.Contains(t, string(raw), `"title":"A"`)
	require.NotContains(t, string(raw), `"title":"B"`)

	stt, raw = doJSON(t, app, "GET", q+"&sort=last_seen", "ren", "")
	require.Equal(t, 200, stt, string(raw))
	require.NoError(t, json.Unmarshal(raw, &env))
	require.Equal(t, "A", env.Data[0].Title)
}

func TestGetIssue(t *testing.T) {
	truncateApps(t)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_app (service_name, display_name, ingest_key, enabled, in_app_prefixes, created_at, updated_at)
		VALUES ('kungal-app','KUN','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', true, '[]', now(), now())`).Error)
	var appID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_app`).Scan(&appID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_issue (
		app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
		first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at)
		VALUES (?, 'fp-g', 'exception', 'Boom', 'fn', 'open', '', false, '2026-09-29', '2026-09-29', '1.0', '1.0', now(), now())`, appID).Error)
	var issueID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_issue`).Scan(&issueID).Error)
	uid := "cccccccccccccccccccccccccccccccc"
	sess := "dddddddddddddddddddddddddddddddd"
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_event (
		record_uid, event_day, app_id, service_version, environment, event_name, severity, event_time,
		session_id, os_name, os_version, api_level, device_model, device_manufacturer, host_arch, sdk_version, attributes, body)
		VALUES (?, '2026-09-29', ?, '1.0', 'direct', 'exception', 17, '2026-09-29T11:00:00Z',
		        ?, 'android', '17', 37, 'Pixel', 'Google', 'arm64', '0.1',
		        '{"exception.stacktrace":"raw-stack","app.breadcrumbs":["open","tap"]}', NULL)`, uid, appID, sess).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_crash (
		event_day, record_uid, app_id, service_version, session_id, event_name, kind, handled, status, needs, attempts,
		exception_type, message, stack, frames, fingerprint, issue_id)
		VALUES ('2026-09-29', ?, ?, '1.0', ?, 'exception', 'exception', true, 'done', '', 0,
		        'StateError', 'boom', 'decoded-stack', '[]', 'fp-g', ?)`, uid, appID, sess, issueID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_issue_daily (issue_id, day, service_version, events, sessions)
		VALUES (?, '2026-09-29', '1.0', 1, 1)`, issueID).Error)

	app, _ := buildAdminApp()
	stt, raw := doJSON(t, app, "GET", "/api/v1/admin/telemetry/issues/"+strconv.FormatInt(issueID, 10), "ren", "")
	require.Equal(t, 200, stt, string(raw))
	require.NotContains(t, string(raw), "session_id")
	require.NotContains(t, string(raw), "record_uid")
	require.NotContains(t, string(raw), uid)
	require.NotContains(t, string(raw), sess)
	require.Contains(t, string(raw), "decoded-stack")
	require.Contains(t, string(raw), "raw-stack")
	require.Contains(t, string(raw), "Pixel")
	require.Contains(t, string(raw), `"open"`)
}

func TestUpdateIssue(t *testing.T) {
	truncateApps(t)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_app (service_name, display_name, ingest_key, enabled, in_app_prefixes, created_at, updated_at)
		VALUES ('kungal-app','KUN','aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa', true, '[]', now(), now())`).Error)
	var appID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_app`).Scan(&appID).Error)
	require.NoError(t, testDB.Exec(`INSERT INTO telemetry_issue (
		app_id, fingerprint, kind, title, culprit, status, resolved_in_version, regressed,
		first_seen_day, last_seen_day, first_version, last_version, created_at, updated_at)
		VALUES (?, 'fp-u', 'exception', 'T', 'c', 'open', '', true, '2026-09-29', '2026-09-29', '1.0', '1.0', now(), now())`, appID).Error)
	var issueID int64
	require.NoError(t, testDB.Raw(`SELECT id FROM telemetry_issue`).Scan(&issueID).Error)
	path := "/api/v1/admin/telemetry/issues/" + strconv.FormatInt(issueID, 10)
	app, _ := buildAdminApp()
	stt, raw := doJSON(t, app, "PATCH", path, "admin", `{"status":"resolved","resolved_in_version":"1.2"}`)
	require.Equal(t, 403, stt, string(raw))
	stt, raw = doJSON(t, app, "PATCH", path, "ren", `{"status":"resolved","resolved_in_version":"1.2"}`)
	require.Equal(t, 200, stt, string(raw))
	require.Contains(t, string(raw), `"status":"resolved"`)
	require.Contains(t, string(raw), `"resolved_in_version":"1.2"`)
	require.Contains(t, string(raw), `"regressed":false`)
}

func TestInAppPrefixesValidation(t *testing.T) {
	truncateApps(t)
	app, _ := buildAdminApp()
	stt, raw := doJSON(t, app, "POST", "/api/v1/admin/telemetry/apps", "ren", `{"service_name":"kungal-app","display_name":"KUN"}`)
	require.Equal(t, 200, stt, string(raw))
	var created struct {
		Data struct{ ID int64 } `json:"data"`
	}
	require.NoError(t, json.Unmarshal(raw, &created))
	path := "/api/v1/admin/telemetry/apps/" + strconv.FormatInt(created.Data.ID, 10)
	stt, raw = doJSON(t, app, "PATCH", path, "ren", `{"in_app_prefixes":[""]}`)
	require.Equal(t, 422, stt, string(raw))
	tooLong := strings.Repeat("a", 101)
	stt, raw = doJSON(t, app, "PATCH", path, "ren", `{"in_app_prefixes":["`+tooLong+`"]}`)
	require.Equal(t, 422, stt, string(raw))
	many := make([]string, 21)
	for i := range many {
		many[i] = `"p` + strconv.Itoa(i) + `"`
	}
	stt, raw = doJSON(t, app, "PATCH", path, "ren", `{"in_app_prefixes":[`+strings.Join(many, ",")+`]}`)
	require.Equal(t, 422, stt, string(raw))
	stt, raw = doJSON(t, app, "PATCH", path, "ren", `{"in_app_prefixes":["package:kungal/","package:nextmoe_"]}`)
	require.Equal(t, 200, stt, string(raw))
	require.Contains(t, string(raw), "package:kungal/")
}
