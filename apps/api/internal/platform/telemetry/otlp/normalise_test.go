package otlp

import (
	"encoding/json"
	"math"
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

func TestDayOf(t *testing.T) {
	receipt := time.Date(2026, 9, 29, 15, 0, 0, 0, time.UTC)
	inside := receipt.Add(-10 * 24 * time.Hour)
	if got := DayOf(inside, receipt); !got.Equal(DateUTC(inside)) {
		t.Errorf("inside window: got %s", got)
	}
	old := receipt.Add(-31 * 24 * time.Hour)
	if got := DayOf(old, receipt); !got.Equal(DateUTC(receipt)) {
		t.Errorf("31 days old: got %s want receipt date", got)
	}
	future := receipt.Add(48 * time.Hour)
	if got := DayOf(future, receipt); !got.Equal(DateUTC(receipt)) {
		t.Errorf("2 days future: got %s want receipt date", got)
	}
	lo := receipt.Add(-30 * 24 * time.Hour)
	if got := DayOf(lo, receipt); !got.Equal(DateUTC(lo)) {
		t.Errorf("exact -30d boundary: got %s", got)
	}
	hi := receipt.Add(24 * time.Hour)
	if got := DayOf(hi, receipt); !got.Equal(DateUTC(hi)) {
		t.Errorf("exact +24h boundary: got %s", got)
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
