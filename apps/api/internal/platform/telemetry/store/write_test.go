package store

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWriteStoresEvents(t *testing.T) {
	truncate(t)
	ts := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	prev := "97affbfc10fc15705a673ebdc93562cb"
	r1 := rec("session.start", sid(1), "0.1.0+9001", "check", ts, map[string]any{
		"session.previous_id": prev,
		"keep":                "yes",
	})
	r1.OSName = "android"
	r1.OSVersion = "17"
	lvl := 37
	r1.APILevel = &lvl
	r1.DeviceModel = "Pixel"
	r1.DeviceManufacturer = "Google"
	r1.HostArch = "arm64"
	r1.SDKVersion = "0.1.0"
	r1.Severity = 9
	body := "hello"
	r1.Body = &body
	r2 := rec("exception", sid(1), "0.1.0+9001", "check", ts.Add(time.Second), map[string]any{
		"exception.type": "x",
	})
	r2.OSName = "android"
	write(t, 1, r1, r2)

	var rows []struct {
		ID                 string
		ReceivedOn         time.Time
		EventDay           time.Time
		AppID              int64
		ServiceVersion     string
		Environment        string
		EventName          string
		Severity           int16
		EventTime          time.Time
		SessionID          *string
		OSName             string
		OSVersion          string
		APILevel           *int
		DeviceModel        string
		DeviceManufacturer string
		HostArch           string
		SDKVersion         string
		Attributes         []byte
		Body               *string
	}
	if err := testDB.Raw(`SELECT id::text, received_on, event_day, app_id, service_version, environment,
		event_name, severity, event_time, session_id, os_name, os_version, api_level,
		device_model, device_manufacturer, host_arch, sdk_version, attributes, body
		FROM telemetry_event ORDER BY event_time`).Scan(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0].AppID != 1 || rows[0].ServiceVersion != "0.1.0+9001" || rows[0].Environment != "check" ||
		rows[0].EventName != "session.start" || rows[0].Severity != 9 || rows[0].OSName != "android" ||
		rows[0].OSVersion != "17" || rows[0].APILevel == nil || *rows[0].APILevel != 37 ||
		rows[0].DeviceModel != "Pixel" || rows[0].DeviceManufacturer != "Google" ||
		rows[0].HostArch != "arm64" || rows[0].SDKVersion != "0.1.0" {
		t.Errorf("column mismatch: %+v", rows[0])
	}
	if rows[0].SessionID == nil || *rows[0].SessionID != sid(1) {
		t.Errorf("session_id: %#v", rows[0].SessionID)
	}
	if rows[0].Body == nil || *rows[0].Body != "hello" {
		t.Errorf("body: %#v", rows[0].Body)
	}
	var attrs map[string]any
	if err := json.Unmarshal(rows[0].Attributes, &attrs); err != nil {
		t.Fatal(err)
	}
	if _, ok := attrs["session.previous_id"]; ok {
		t.Fatal("previous_id stored")
	}
	if string(rows[0].Attributes) != "" && contains(string(rows[0].Attributes), prev) {
		t.Fatal("previous_id value stored")
	}
	id0, err := uuid.Parse(rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	id1, err := uuid.Parse(rows[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if id0 == id1 {
		t.Fatal("duplicate uuid")
	}
	if id0.Version() != 4 || id1.Version() != 4 {
		t.Errorf("expected random v4 uuids, got %d and %d", id0.Version(), id1.Version())
	}
	if id0 == uuid.Nil || id1 == uuid.Nil {
		t.Fatal("nil uuid")
	}
}

func contains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && (stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestNoSharedWriteInstant(t *testing.T) {
	truncate(t)
	t1 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	s1, s2 := sid(1), sid(2)
	write(t, 3,
		rec("session.start", s1, "1.0.0", "direct", t1, nil),
		rec("session.start", s2, "1.0.0", "direct", t2, nil),
	)
	type sessRow map[string]any
	var sessions []struct {
		AppID          int64
		SessionID      string
		ServiceVersion string
		Environment    string
		Day            time.Time
		StartedAt      *time.Time
		EndedAt        *time.Time
		Status         *string
		Errors         *int
		ANR            bool
	}
	if err := testDB.Raw(`SELECT app_id, session_id, service_version, environment, day, started_at, ended_at, status, errors, anr FROM telemetry_session ORDER BY session_id`).Scan(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions=%d", len(sessions))
	}
	if sessions[0].SessionID == sessions[1].SessionID {
		t.Fatal("session ids equal")
	}
	if sessions[0].StartedAt == nil || sessions[1].StartedAt == nil || sessions[0].StartedAt.Equal(*sessions[1].StartedAt) {
		t.Fatal("started_at shared or missing")
	}
	var hasUpdatedAt bool
	if err := testDB.Raw(`SELECT EXISTS (
		SELECT 1 FROM information_schema.columns
		 WHERE table_name='telemetry_session' AND column_name IN ('updated_at','created_at','batch_id','request_id','received_at')
	)`).Scan(&hasUpdatedAt).Error; err != nil {
		t.Fatal(err)
	}
	if hasUpdatedAt {
		t.Fatal("session table has a receipt-granularity or batch column")
	}

	var events []struct {
		ID        string
		SessionID string
		EventTime time.Time
	}
	if err := testDB.Raw(`SELECT id::text, session_id, event_time FROM telemetry_event ORDER BY session_id`).Scan(&events).Error; err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].ID == events[1].ID {
		t.Fatalf("event ids: %+v", events)
	}
	if events[0].EventTime.Equal(events[1].EventTime) {
		t.Fatal("event_time shared")
	}

	sessionAllow := map[string]bool{
		"app_id": true, "service_version": true, "environment": true, "day": true, "anr": true,
		"ended_at": true, "status": true, "errors": true,
	}
	eventAllow := map[string]bool{
		"received_on": true, "app_id": true, "service_version": true, "environment": true,
		"event_name": true, "severity": true, "event_day": true,
		"os_name": true, "os_version": true, "api_level": true, "device_model": true,
		"device_manufacturer": true, "host_arch": true, "sdk_version": true, "attributes": true, "body": true,
	}
	assertNoShared(t, `SELECT * FROM telemetry_session ORDER BY session_id`, sessionAllow)
	assertNoShared(t, `SELECT * FROM telemetry_event ORDER BY session_id`, eventAllow)
	_ = sessRow{}
}

func assertNoShared(t *testing.T, q string, allow map[string]bool) {
	t.Helper()
	rows, err := testDB.Raw(q).Rows()
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	var dumped [][]any
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			t.Fatal(err)
		}
		dumped = append(dumped, vals)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(dumped) != 2 {
		t.Fatalf("rows=%d query=%s", len(dumped), q)
	}
	for i, col := range cols {
		if allow[col] {
			continue
		}
		if eqAny(dumped[0][i], dumped[1][i]) {
			t.Errorf("column %s is equal across the two rows of one request: %#v", col, dumped[0][i])
		}
	}
}

func eqAny(a, b any) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	switch x := a.(type) {
	case []byte:
		y, ok := b.([]byte)
		return ok && string(x) == string(y)
	default:
		return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
	}
}

func TestMultiVersionRequest(t *testing.T) {
	truncate(t)
	oldSID, newSID := sid(1), sid(2)
	tOld := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	tNew := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	write(t, 1,
		rec("app.crash", oldSID, "0.1.0", "direct", tOld, map[string]any{"app.exit.kind": "java"}),
		rec("session.end", oldSID, "0.1.0", "direct", tOld.Add(time.Second), map[string]any{"app.session.status": "crashed"}),
		rec("session.start", newSID, "0.1.1", "direct", tNew, nil),
	)
	var sessions []struct {
		SessionID      string
		ServiceVersion string
		Status         *string
	}
	if err := testDB.Raw(`SELECT session_id, service_version, status FROM telemetry_session ORDER BY session_id`).Scan(&sessions).Error; err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions=%d", len(sessions))
	}
	by := map[string]struct {
		ServiceVersion string
		Status         *string
	}{}
	for _, s := range sessions {
		by[s.SessionID] = struct {
			ServiceVersion string
			Status         *string
		}{s.ServiceVersion, s.Status}
	}
	if by[oldSID].ServiceVersion != "0.1.0" {
		t.Errorf("old session version %s", by[oldSID].ServiceVersion)
	}
	if by[oldSID].Status == nil || *by[oldSID].Status != "crashed" {
		t.Errorf("old status %#v", by[oldSID].Status)
	}
	if by[newSID].ServiceVersion != "0.1.1" {
		t.Errorf("new session version %s", by[newSID].ServiceVersion)
	}
	var crashes []struct {
		ServiceVersion string
		EventName      string
	}
	if err := testDB.Raw(`SELECT service_version, event_name FROM telemetry_event WHERE event_name IN ('app.crash','session.start','session.end')`).Scan(&crashes).Error; err != nil {
		t.Fatal(err)
	}
	for _, c := range crashes {
		switch c.EventName {
		case "app.crash", "session.end":
			if c.ServiceVersion != "0.1.0" {
				t.Errorf("%s charged to %s", c.EventName, c.ServiceVersion)
			}
		case "session.start":
			if c.ServiceVersion != "0.1.1" {
				t.Errorf("start charged to %s", c.ServiceVersion)
			}
		}
	}
	if err := st.Rollup(context.Background(), receiptNow().AddDate(0, 0, -1), receiptNow()); err != nil {
		t.Fatal(err)
	}
	var metrics []struct {
		ServiceVersion string
		CrashJava      int
		Sessions       int
	}
	if err := testDB.Raw(`SELECT service_version, crash_java, sessions FROM telemetry_daily_metric`).Scan(&metrics).Error; err != nil {
		t.Fatal(err)
	}
	foundOld, foundNew := false, false
	for _, m := range metrics {
		if m.ServiceVersion == "0.1.0" {
			foundOld = true
			if m.CrashJava != 1 {
				t.Errorf("0.1.0 crash_java=%d", m.CrashJava)
			}
		}
		if m.ServiceVersion == "0.1.1" {
			foundNew = true
			if m.CrashJava != 0 {
				t.Errorf("0.1.1 crash_java=%d", m.CrashJava)
			}
		}
	}
	if !foundOld {
		t.Fatal("missing 0.1.0 metric")
	}
	_ = foundNew
}
