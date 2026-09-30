package otlp

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestDecodeTolerantScalars(t *testing.T) {
	raw := []byte(`{
		"resourceLogs":[{
			"scopeLogs":[{
				"logRecords":[
					{"timeUnixNano":"1790699300350000000","severityNumber":21,"eventName":"a"},
					{"timeUnixNano":123,"severityNumber":"SEVERITY_NUMBER_INFO","eventName":"b",
					 "attributes":[
						{"key":"nan","value":{"doubleValue":"NaN"}},
						{"key":"inf","value":{"doubleValue":"Infinity"}},
						{"key":"ninf","value":{"doubleValue":"-Infinity"}},
						{"key":"num","value":{"doubleValue":1.5}}
					 ]}
				]
			}]
		}]
	}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	recs := req.ResourceLogs[0].ScopeLogs[0].LogRecords
	if recs[0].TimeUnixNano != 1790699300350000000 {
		t.Errorf("string int64: got %d", recs[0].TimeUnixNano)
	}
	if recs[0].SeverityNumber != 21 {
		t.Errorf("int severity: got %d", recs[0].SeverityNumber)
	}
	if recs[1].TimeUnixNano != 123 {
		t.Errorf("number int64: got %d", recs[1].TimeUnixNano)
	}
	if recs[1].SeverityNumber != 9 {
		t.Errorf("enum severity: got %d", recs[1].SeverityNumber)
	}
	attrs := attributesMap(recs[1].Attributes)
	if attrs["nan"] != "NaN" {
		t.Errorf("NaN: got %#v", attrs["nan"])
	}
	if attrs["inf"] != "Infinity" {
		t.Errorf("Infinity: got %#v", attrs["inf"])
	}
	if attrs["ninf"] != "-Infinity" {
		t.Errorf("-Infinity: got %#v", attrs["ninf"])
	}
	if attrs["num"] != 1.5 {
		t.Errorf("double 1.5: got %#v", attrs["num"])
	}
}

func TestAnyValueConversion(t *testing.T) {
	raw := []byte(`{
		"stringValue":"hi",
		"unused": true
	}`)
	var s AnyValue
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.GoValue() != "hi" {
		t.Fatalf("string: got %#v", s.GoValue())
	}

	cases := []struct {
		json string
		want any
		nan  bool
	}{
		{`{"boolValue":true}`, true, false},
		{`{"intValue":"42"}`, int64(42), false},
		{`{"intValue":7}`, int64(7), false},
		{`{"doubleValue":1.25}`, 1.25, false},
		{`{"doubleValue":"NaN"}`, "NaN", false},
		{`{"bytesValue":"YWJj"}`, "YWJj", false},
		{`{}`, nil, false},
		{`{"arrayValue":{"values":[{"stringValue":"a"},{"intValue":"1"}]}}`, []any{"a", int64(1)}, false},
		{`{"kvlistValue":{"values":[{"key":"k","value":{"stringValue":"v"}}]}}`, map[string]any{"k": "v"}, false},
	}
	for _, c := range cases {
		var a AnyValue
		if err := json.Unmarshal([]byte(c.json), &a); err != nil {
			t.Fatalf("unmarshal %s: %v", c.json, err)
		}
		got := a.GoValue()
		switch want := c.want.(type) {
		case []any:
			gotS, _ := got.([]any)
			if len(gotS) != len(want) || gotS[0] != want[0] || gotS[1] != want[1] {
				t.Errorf("%s: got %#v want %#v", c.json, got, want)
			}
		case map[string]any:
			gotM, _ := got.(map[string]any)
			if gotM["k"] != want["k"] {
				t.Errorf("%s: got %#v want %#v", c.json, got, want)
			}
		default:
			if got != want {
				t.Errorf("%s: got %#v (%T) want %#v", c.json, got, got, want)
			}
		}
	}
	_ = math.NaN()
}

func resourceJSON(service, extraAttrs, records string) []byte {
	if extraAttrs != "" {
		extraAttrs = "," + extraAttrs
	}
	return []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"` + service + `"}}` + extraAttrs + `
	]},"scopeLogs":[{"logRecords":[` + records + `]}]}]}`)
}

func TestNormaliseResource(t *testing.T) {
	receipt := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"resourceLogs":[{
		"resource":{"attributes":[
			{"key":"service.name","value":{"stringValue":"kungal-app"}},
			{"key":"service.version","value":{"stringValue":"0.1.0+9001"}},
			{"key":"deployment.environment.name","value":{"stringValue":"check"}},
			{"key":"os.name","value":{"stringValue":"android"}},
			{"key":"os.version","value":{"stringValue":"17"}},
			{"key":"android.os.api_level","value":{"intValue":"37"}},
			{"key":"device.model.identifier","value":{"stringValue":"Pixel"}},
			{"key":"device.manufacturer","value":{"stringValue":"Google"}},
			{"key":"host.arch","value":{"stringValue":"arm64"}},
			{"key":"telemetry.sdk.version","value":{"stringValue":"0.1.0"}},
			{"key":"telemetry.sdk.name","value":{"stringValue":"nextmoe_telemetry"}}
		]},
		"scopeLogs":[{"logRecords":[{"eventName":"session.start","timeUnixNano":"1000"}]}]
	},{
		"resource":{"attributes":[
			{"key":"service.name","value":{"stringValue":"kungal-app"}},
			{"key":"android.os.api_level","value":{"stringValue":"36"}}
		]},
		"scopeLogs":[{"logRecords":[{"eventName":"session.start","timeUnixNano":"2000"}]}]
	},{
		"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"kungal-app"}}]},
		"scopeLogs":[{"logRecords":[{"eventName":"session.start","timeUnixNano":"3000"}]}]
	}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "kungal-app", receipt)
	if len(batch.Records) != 3 {
		t.Fatalf("records=%d", len(batch.Records))
	}
	r0 := batch.Records[0]
	if r0.ServiceVersion != "0.1.0+9001" || r0.Environment != "check" || r0.OSName != "android" ||
		r0.OSVersion != "17" || r0.DeviceModel != "Pixel" || r0.DeviceManufacturer != "Google" ||
		r0.HostArch != "arm64" || r0.SDKVersion != "0.1.0" {
		t.Errorf("resource columns: %+v", r0)
	}
	if r0.APILevel == nil || *r0.APILevel != 37 {
		t.Errorf("api_level intValue: %#v", r0.APILevel)
	}
	if _, ok := r0.Attributes["telemetry.sdk.name"]; ok {
		t.Error("dropped resource attrs leaked into record attributes")
	}
	if batch.Records[1].APILevel == nil || *batch.Records[1].APILevel != 36 {
		t.Errorf("api_level numeric string: %#v", batch.Records[1].APILevel)
	}
	if batch.Records[2].ServiceVersion != "" || batch.Records[2].Environment != "" ||
		batch.Records[2].OSName != "" || batch.Records[2].APILevel != nil {
		t.Errorf("missing resource should be empty/NULL: %+v api=%v", batch.Records[2], batch.Records[2].APILevel)
	}
}

func TestRejectServiceNameMismatch(t *testing.T) {
	raw := []byte(`{"resourceLogs":[
		{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"kungal-app"}}]},
		 "scopeLogs":[{"logRecords":[
			{"eventName":"session.start","timeUnixNano":"1"},
			{"eventName":"session.end","timeUnixNano":"2"}
		 ]}]},
		{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"other-app"}}]},
		 "scopeLogs":[{"logRecords":[
			{"eventName":"app.crash","timeUnixNano":"3"},
			{"eventName":"exception","timeUnixNano":"4"},
			{"eventName":"app.jank","timeUnixNano":"5"}
		 ]}]}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "kungal-app", time.Now())
	if batch.Rejected != 3 {
		t.Errorf("rejected=%d want 3", batch.Rejected)
	}
	if len(batch.Records) != 2 {
		t.Fatalf("kept %d want 2", len(batch.Records))
	}
	for _, r := range batch.Records {
		if r.EventName != "session.start" && r.EventName != "session.end" {
			t.Errorf("unexpected kept event %q", r.EventName)
		}
	}
}

func TestRejectEmptyEventName(t *testing.T) {
	req, err := Decode(resourceJSON("app", "", `{"timeUnixNano":"1"},{"eventName":"ok","timeUnixNano":"2"}`))
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if batch.Rejected != 1 || len(batch.Records) != 1 || batch.Records[0].EventName != "ok" {
		t.Fatalf("rejected=%d records=%d", batch.Rejected, len(batch.Records))
	}
}

func TestUnknownEventNameKept(t *testing.T) {
	req, err := Decode(resourceJSON("app", "", `{"eventName":"app.future","timeUnixNano":"1"}`))
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if batch.Rejected != 0 || len(batch.Records) != 1 || batch.Records[0].EventName != "app.future" {
		t.Fatalf("unknown event dropped: %+v rejected=%d", batch.Records, batch.Rejected)
	}
}

func TestSessionIDValidation(t *testing.T) {
	valid := "0123456789abcdef0123456789abcdef"
	records := `{"eventName":"x","timeUnixNano":"1","attributes":[{"key":"session.id","value":{"stringValue":"` + valid + `"}}]},` +
		`{"eventName":"x","timeUnixNano":"2","attributes":[{"key":"session.id","value":{"stringValue":"0123456789ABCDEF0123456789ABCDEF"}}]},` +
		`{"eventName":"x","timeUnixNano":"3","attributes":[{"key":"session.id","value":{"stringValue":"0123456789abcdef0123456789abcde"}}]},` +
		`{"eventName":"x","timeUnixNano":"4","attributes":[{"key":"session.id","value":{"stringValue":"gggggggggggggggggggggggggggggggg"}}]}`
	req, err := Decode(resourceJSON("app", "", records))
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 4 {
		t.Fatalf("all records should be kept, got %d", len(batch.Records))
	}
	if batch.Records[0].SessionID != valid {
		t.Errorf("valid session id dropped: %q", batch.Records[0].SessionID)
	}
	for i, rec := range batch.Records[1:] {
		if rec.SessionID != "" {
			t.Errorf("record %d should have no session, got %q", i+1, rec.SessionID)
		}
	}
}

func TestPreviousIDDropped(t *testing.T) {
	prev := "97affbfc10fc15705a673ebdc93562cb"
	raw := resourceJSON("app", "", `{"eventName":"session.start","timeUnixNano":"1","attributes":[
		{"key":"session.id","value":{"stringValue":"0123456789abcdef0123456789abcdef"}},
		{"key":"session.previous_id","value":{"stringValue":"`+prev+`"}}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 1 {
		t.Fatal(batch.Records)
	}
	r := batch.Records[0]
	if _, ok := r.Attributes["session.previous_id"]; ok {
		t.Fatal("previous_id stored in attributes")
	}
	if r.SessionID == prev {
		t.Fatal("previous_id used as session id")
	}
	blob, _ := json.Marshal(r)
	if strings.Contains(string(blob), prev) {
		t.Fatalf("previous_id leaked into record: %s", blob)
	}
	blob, _ = json.Marshal(batch)
	if strings.Contains(string(blob), "session.previous_id") || strings.Contains(string(blob), prev) {
		t.Fatalf("previous_id leaked into batch: %s", blob)
	}
}

func TestNumericAttributeNormalisation(t *testing.T) {
	raw := resourceJSON("app", "", `{"eventName":"app.startup","timeUnixNano":"1","attributes":[
		{"key":"app.startup.ttid_ms","value":{"stringValue":"250"}},
		{"key":"app.jank.frame_count","value":{"intValue":"3"}},
		{"key":"app.jank.frames","value":{"doubleValue":100}},
		{"key":"app.jank.threshold","value":{"stringValue":"not-a-number"}},
		{"key":"app.session.errors","value":{"stringValue":"2"}},
		{"key":"android.exit.reason","value":{"intValue":"5"}},
		{"key":"android.start.reason","value":{"stringValue":"START_REASON_ALARM"}},
		{"key":"http.response.status_code","value":{"stringValue":"500"}},
		{"key":"keep","value":{"stringValue":"yes"}}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	a := batch.Records[0].Attributes
	if a["app.startup.ttid_ms"] != int64(250) {
		t.Errorf("ttid: %#v", a["app.startup.ttid_ms"])
	}
	if a["app.jank.frame_count"] != int64(3) {
		t.Errorf("frame_count: %#v", a["app.jank.frame_count"])
	}
	if a["app.jank.frames"] != int64(100) {
		t.Errorf("frames: %#v", a["app.jank.frames"])
	}
	if _, ok := a["app.jank.threshold"]; ok {
		t.Errorf("garbage threshold kept: %#v", a["app.jank.threshold"])
	}
	if a["app.session.errors"] != int64(2) {
		t.Errorf("errors: %#v", a["app.session.errors"])
	}
	if a["android.exit.reason"] != int64(5) {
		t.Errorf("exit.reason: %#v", a["android.exit.reason"])
	}
	if a["android.start.reason"] != "START_REASON_ALARM" {
		t.Errorf("start.reason: %#v", a["android.start.reason"])
	}
	if a["http.response.status_code"] != int64(500) {
		t.Errorf("status: %#v", a["http.response.status_code"])
	}
	if a["keep"] != "yes" {
		t.Errorf("unrelated attr dropped")
	}
}

func TestEventTimeFallbacks(t *testing.T) {
	receipt := time.Date(2026, 9, 29, 12, 34, 56, 789000000, time.UTC)
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"app"}}]},
		"scopeLogs":[{"logRecords":[
			{"eventName":"a","timeUnixNano":"2000000","observedTimeUnixNano":"3000000"},
			{"eventName":"b","timeUnixNano":"0","observedTimeUnixNano":"4000000"},
			{"eventName":"c"}
		]}]}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", receipt)
	if !batch.Records[0].EventTime.Equal(time.Unix(0, 2000000).UTC().Truncate(time.Microsecond)) {
		t.Errorf("timeUnixNano: %s", batch.Records[0].EventTime)
	}
	if !batch.Records[1].EventTime.Equal(time.Unix(0, 4000000).UTC().Truncate(time.Microsecond)) {
		t.Errorf("observed: %s", batch.Records[1].EventTime)
	}
	want := receipt.UTC().Truncate(time.Minute)
	if !batch.Records[2].EventTime.Equal(want) {
		t.Errorf("receipt truncated: got %s want %s", batch.Records[2].EventTime, want)
	}
}

func TestClassifyDay(t *testing.T) {
	receipt := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	floor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	minus30 := receipt.AddDate(0, 0, -30)
	plus24 := receipt.Add(24 * time.Hour)
	inside := receipt.Add(-10 * 24 * time.Hour)

	cases := []struct {
		name    string
		event   time.Time
		wantDay time.Time
		keep    bool
	}{
		{"pre-floor", floor.Add(-time.Second), DateUTC(receipt), true},
		{"future", plus24.Add(time.Second), DateUTC(receipt), true},
		{"expired", minus30.Add(-time.Nanosecond), time.Time{}, false},
		{"older than 31 days", receipt.AddDate(0, 0, -31), time.Time{}, false},
		{"inside window", inside, DateUTC(inside), true},
		{"exact -30d", minus30, DateUTC(minus30), true},
		{"exact +24h", plus24, DateUTC(plus24), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			day, keep := ClassifyDay(c.event, receipt)
			if keep != c.keep {
				t.Errorf("keep=%v want %v", keep, c.keep)
			}
			if !day.Equal(c.wantDay) {
				t.Errorf("day=%s want %s", day, c.wantDay)
			}
		})
	}
}

func TestBodyString(t *testing.T) {
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"app"}}]},
		"scopeLogs":[{"logRecords":[
			{"eventName":"a","timeUnixNano":"1","body":{"stringValue":"hello"}},
			{"eventName":"b","timeUnixNano":"2","body":{"intValue":"1"}},
			{"eventName":"c","timeUnixNano":"3"}
		]}]}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if batch.Records[0].Body == nil || *batch.Records[0].Body != "hello" {
		t.Errorf("string body: %#v", batch.Records[0].Body)
	}
	if batch.Records[1].Body != nil {
		t.Errorf("non-string body kept: %#v", *batch.Records[1].Body)
	}
	if batch.Records[2].Body != nil {
		t.Errorf("missing body: %#v", batch.Records[2].Body)
	}
}

func TestRecordUIDExtracted(t *testing.T) {
	uid := "0123456789abcdef0123456789abcdef"
	raw := resourceJSON("app", "", `{"eventName":"x","timeUnixNano":"1","attributes":[
		{"key":"log.record.uid","value":{"stringValue":"`+uid+`"}},
		{"key":"keep","value":{"stringValue":"yes"}}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 1 {
		t.Fatalf("records=%d", len(batch.Records))
	}
	r := batch.Records[0]
	if r.RecordUID != uid {
		t.Errorf("uid=%q", r.RecordUID)
	}
	if _, ok := r.Attributes["log.record.uid"]; ok {
		t.Fatal("log.record.uid stored in attributes")
	}
}

func TestRecordUIDGenerated(t *testing.T) {
	raw := resourceJSON("app", "", strings.Join([]string{
		`{"eventName":"a","timeUnixNano":"1"}`,
		`{"eventName":"b","timeUnixNano":"2","attributes":[{"key":"log.record.uid","value":{"stringValue":"0123456789ABCDEF0123456789ABCDEF"}}]}`,
		`{"eventName":"c","timeUnixNano":"3","attributes":[{"key":"log.record.uid","value":{"stringValue":"0123456789abcdef0123456789abcde"}}]}`,
		`{"eventName":"d","timeUnixNano":"4","attributes":[{"key":"log.record.uid","value":{"intValue":"1"}}]}`,
	}, ","))
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 4 {
		t.Fatalf("records=%d", len(batch.Records))
	}
	seen := map[string]bool{}
	for i, r := range batch.Records {
		if !recordUIDRe.MatchString(r.RecordUID) {
			t.Errorf("record %d uid %q not 32 lowercase hex", i, r.RecordUID)
		}
		if _, ok := r.Attributes["log.record.uid"]; ok {
			t.Errorf("record %d kept log.record.uid in attributes", i)
		}
		if seen[r.RecordUID] {
			t.Errorf("duplicate generated uid %q", r.RecordUID)
		}
		seen[r.RecordUID] = true
	}
	if batch.Records[0].RecordUID == batch.Records[1].RecordUID {
		t.Fatal("two generated uids were equal")
	}
}

func TestExpiredRecordsRejected(t *testing.T) {
	receipt := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	keptNano := receipt.UnixNano()
	expired := receipt.AddDate(0, 0, -31)
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"app"}}]},
		"scopeLogs":[{"logRecords":[
			{"eventName":"kept","timeUnixNano":"` + strconv.FormatInt(keptNano, 10) + `"},
			{"eventName":"old","timeUnixNano":"` + strconv.FormatInt(expired.UnixNano(), 10) + `"}
		]}]}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", receipt)
	if batch.Rejected != 1 {
		t.Errorf("rejected=%d want 1", batch.Rejected)
	}
	if batch.Reason != "expired" {
		t.Errorf("reason=%q", batch.Reason)
	}
	if len(batch.Records) != 1 || batch.Records[0].EventName != "kept" {
		t.Fatalf("records=%+v", batch.Records)
	}
}

func TestNULStripped(t *testing.T) {
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"app"}},
		{"key":"device.model.identifier","value":{"stringValue":"Pix\u0000el"}}
	]},"scopeLogs":[{"logRecords":[{
		"eventName":"x","timeUnixNano":"1",
		"body":{"stringValue":"hel\u0000lo"},
		"attributes":[
			{"key":"arr","value":{"arrayValue":{"values":[{"stringValue":"a\u0000b"}]}}},
			{"key":"kv","value":{"kvlistValue":{"values":[{"key":"n\u0000k","value":{"stringValue":"v\u0000v"}}]}}},
			{"key":"k\u0000ey","value":{"stringValue":"x"}}
		]
	}]}]}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 1 {
		t.Fatalf("records=%d", len(batch.Records))
	}
	r := batch.Records[0]
	if r.DeviceModel != "Pixel" {
		t.Errorf("device model=%q", r.DeviceModel)
	}
	if r.Body == nil || *r.Body != "hello" {
		t.Errorf("body=%#v", r.Body)
	}
	arr, _ := r.Attributes["arr"].([]any)
	if len(arr) != 1 || arr[0] != "ab" {
		t.Errorf("arr=%#v", r.Attributes["arr"])
	}
	kv, _ := r.Attributes["kv"].(map[string]any)
	if kv["nk"] != "vv" {
		t.Errorf("kv=%#v", kv)
	}
	if r.Attributes["key"] != "x" {
		t.Errorf("map key=%#v", r.Attributes)
	}
	blob, _ := json.Marshal(r)
	if strings.Contains(string(blob), "\x00") {
		t.Fatalf("NUL leaked: %s", blob)
	}
}

func TestAPILevelBounds(t *testing.T) {
	receipt := time.Now()
	cases := []struct {
		extra string
		want  *int
	}{
		{`{"key":"android.os.api_level","value":{"intValue":"0"}}`, intPtr(0)},
		{`{"key":"android.os.api_level","value":{"intValue":"2147483647"}}`, intPtr(2147483647)},
		{`{"key":"android.os.api_level","value":{"intValue":"-1"}}`, nil},
		{`{"key":"android.os.api_level","value":{"intValue":"2147483648"}}`, nil},
		{`{"key":"android.os.api_level","value":{"doubleValue":3.5}}`, nil},
	}
	for i, c := range cases {
		req, err := Decode(resourceJSON("app", c.extra, `{"eventName":"x","timeUnixNano":"1"}`))
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		batch := Normalise(req, "app", receipt)
		got := batch.Records[0].APILevel
		if (got == nil) != (c.want == nil) || (got != nil && *got != *c.want) {
			t.Errorf("case %d api_level=%v want %v", i, got, c.want)
		}
	}
}

func intPtr(n int) *int { return &n }

func TestIntegerAttributeBounds(t *testing.T) {
	raw := resourceJSON("app", "", `{"eventName":"x","timeUnixNano":"1","attributes":[
		{"key":"app.startup.ttid_ms","value":{"doubleValue":3.5}},
		{"key":"app.jank.frame_count","value":{"intValue":"-1"}},
		{"key":"app.jank.frames","value":{"intValue":"2147483648"}},
		{"key":"app.session.errors","value":{"stringValue":"12"}},
		{"key":"http.response.status_code","value":{"doubleValue":12.0}}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	a := Normalise(req, "app", time.Now()).Records[0].Attributes
	if _, ok := a["app.startup.ttid_ms"]; ok {
		t.Errorf("3.5 kept: %#v", a["app.startup.ttid_ms"])
	}
	if _, ok := a["app.jank.frame_count"]; ok {
		t.Errorf("-1 kept: %#v", a["app.jank.frame_count"])
	}
	if _, ok := a["app.jank.frames"]; ok {
		t.Errorf("2147483648 kept: %#v", a["app.jank.frames"])
	}
	if a["app.session.errors"] != int64(12) {
		t.Errorf(`"12": %#v`, a["app.session.errors"])
	}
	if a["http.response.status_code"] != int64(12) {
		t.Errorf("12.0: %#v", a["http.response.status_code"])
	}
}

func TestJankThreshold(t *testing.T) {
	raw := resourceJSON("app", "", strings.Join([]string{
		`{"eventName":"a","timeUnixNano":"1","attributes":[{"key":"app.jank.threshold","value":{"doubleValue":0.25}}]}`,
		`{"eventName":"b","timeUnixNano":"2","attributes":[{"key":"app.jank.threshold","value":{"intValue":"-1"}}]}`,
		`{"eventName":"c","timeUnixNano":"3","attributes":[{"key":"app.jank.threshold","value":{"stringValue":"NaN"}}]}`,
	}, ","))
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if batch.Records[0].Attributes["app.jank.threshold"] != 0.25 {
		t.Errorf("0.25: %#v", batch.Records[0].Attributes["app.jank.threshold"])
	}
	if _, ok := batch.Records[1].Attributes["app.jank.threshold"]; ok {
		t.Errorf("-1 kept: %#v", batch.Records[1].Attributes["app.jank.threshold"])
	}
	if _, ok := batch.Records[2].Attributes["app.jank.threshold"]; ok {
		t.Errorf("NaN kept: %#v", batch.Records[2].Attributes["app.jank.threshold"])
	}
}

func TestSeverityBounds(t *testing.T) {
	raw := []byte(`{"resourceLogs":[{"resource":{"attributes":[
		{"key":"service.name","value":{"stringValue":"app"}}]},
		"scopeLogs":[{"logRecords":[
			{"eventName":"a","timeUnixNano":"1","severityNumber":25},
			{"eventName":"b","timeUnixNano":"2","severityNumber":-1},
			{"eventName":"c","timeUnixNano":"3","severityNumber":0},
			{"eventName":"d","timeUnixNano":"4","severityNumber":24},
			{"eventName":"e","timeUnixNano":"5","severityNumber":9}
		]}]}]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	want := []int16{0, 0, 0, 24, 9}
	if len(batch.Records) != len(want) {
		t.Fatalf("records=%d", len(batch.Records))
	}
	for i, w := range want {
		if batch.Records[i].Severity != w {
			t.Errorf("record %d severity=%d want %d", i, batch.Records[i].Severity, w)
		}
	}
}

func TestPerResourceVersion(t *testing.T) {
	raw := []byte(`{"resourceLogs":[
		{"resource":{"attributes":[
			{"key":"service.name","value":{"stringValue":"app"}},
			{"key":"service.version","value":{"stringValue":"0.1.0"}}
		]},"scopeLogs":[{"logRecords":[{"eventName":"old","timeUnixNano":"1"}]}]},
		{"resource":{"attributes":[
			{"key":"service.name","value":{"stringValue":"app"}},
			{"key":"service.version","value":{"stringValue":"0.1.1"}}
		]},"scopeLogs":[{"logRecords":[{"eventName":"new","timeUnixNano":"2"}]}]}
	]}`)
	req, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	batch := Normalise(req, "app", time.Now())
	if len(batch.Records) != 2 {
		t.Fatalf("records=%d", len(batch.Records))
	}
	if batch.Records[0].EventName != "old" || batch.Records[0].ServiceVersion != "0.1.0" {
		t.Errorf("first=%+v", batch.Records[0])
	}
	if batch.Records[1].EventName != "new" || batch.Records[1].ServiceVersion != "0.1.1" {
		t.Errorf("second=%+v", batch.Records[1])
	}
}
