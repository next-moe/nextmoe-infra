package otlp

import (
	"crypto/rand"
	"encoding/hex"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var recordUIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

var sessionIDRe = recordUIDRe

const recordUIDKey = "log.record.uid"

var integerAttrKeys = map[string]bool{
	"app.startup.ttid_ms":       true,
	"app.jank.frame_count":      true,
	"app.jank.frames":           true,
	"app.session.errors":        true,
	"android.exit.reason":       true,
	"http.response.status_code": true,
}

const (
	androidStartReason = "android.start.reason"
	jankThresholdKey   = "app.jank.threshold"
	maxIntAttr         = 2147483647
)

var clockFloor = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

type Record struct {
	ServiceVersion     string
	Environment        string
	OSName             string
	OSVersion          string
	APILevel           *int
	DeviceModel        string
	DeviceManufacturer string
	HostArch           string
	SDKVersion         string
	EventName          string
	Severity           int16
	EventTime          time.Time
	EventDay           time.Time
	SessionID          string
	RecordUID          string
	Attributes         map[string]any
	Body               *string
}

type Batch struct {
	Records  []Record
	Rejected int
	Reason   string
}

func DateUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func ClassifyDay(eventTime, receipt time.Time) (day time.Time, keep bool) {
	eventTime = eventTime.UTC()
	receipt = receipt.UTC()
	if eventTime.Before(clockFloor) {
		return DateUTC(receipt), true
	}
	if eventTime.After(receipt.Add(24 * time.Hour)) {
		return DateUTC(receipt), true
	}
	if eventTime.Before(receipt.AddDate(0, 0, -30)) {
		return time.Time{}, false
	}
	return DateUTC(eventTime), true
}

func eventTimeOf(rec LogRecord, receipt time.Time) time.Time {
	if rec.TimeUnixNano != 0 {
		return time.Unix(0, int64(rec.TimeUnixNano)).UTC().Truncate(time.Microsecond)
	}
	if rec.ObservedTimeUnixNano != 0 {
		return time.Unix(0, int64(rec.ObservedTimeUnixNano)).UTC().Truncate(time.Microsecond)
	}
	return receipt.UTC().Truncate(time.Minute)
}

func Normalise(req *ExportLogsServiceRequest, serviceName string, receipt time.Time) Batch {
	out := Batch{Records: make([]Record, 0)}
	if req == nil {
		return out
	}
	reasons := map[string]int{}
	for _, rl := range req.ResourceLogs {
		res := stripNULMap(attributesMap(rl.Resource.Attributes))
		if attrString(res, "service.name") != serviceName {
			n := countLogRecords(rl)
			out.Rejected += n
			if n > 0 {
				reasons["service.name mismatch"] += n
			}
			continue
		}
		apiLevel := parseAPILevel(res["android.os.api_level"])
		base := Record{
			ServiceVersion:     attrString(res, "service.version"),
			Environment:        attrString(res, "deployment.environment.name"),
			OSName:             attrString(res, "os.name"),
			OSVersion:          attrString(res, "os.version"),
			APILevel:           apiLevel,
			DeviceModel:        attrString(res, "device.model.identifier"),
			DeviceManufacturer: attrString(res, "device.manufacturer"),
			HostArch:           attrString(res, "host.arch"),
			SDKVersion:         attrString(res, "telemetry.sdk.version"),
		}
		for _, sl := range rl.ScopeLogs {
			for _, lr := range sl.LogRecords {
				eventName := stripNUL(lr.EventName)
				if eventName == "" {
					out.Rejected++
					reasons["empty eventName"]++
					continue
				}
				eventTime := eventTimeOf(lr, receipt)
				day, keep := ClassifyDay(eventTime, receipt)
				if !keep {
					out.Rejected++
					reasons["expired"]++
					continue
				}
				rec := base
				rec.EventName = eventName
				rec.Severity = clampSeverity(int16(lr.SeverityNumber))
				rec.EventTime = eventTime
				rec.EventDay = day
				attrs := stripNULMap(attributesMap(lr.Attributes))
				delete(attrs, "session.previous_id")
				rec.RecordUID = takeRecordUID(attrs)
				if sid, ok := attrs["session.id"].(string); ok && sessionIDRe.MatchString(sid) {
					rec.SessionID = sid
				}
				delete(attrs, "session.id")
				rec.Attributes = normaliseNumericAttrs(attrs)
				if lr.Body != nil && lr.Body.StringValue != nil {
					s := stripNUL(*lr.Body.StringValue)
					rec.Body = &s
				}
				out.Records = append(out.Records, rec)
			}
		}
	}
	out.Reason = summariseReasons(reasons)
	return out
}

func clampSeverity(n int16) int16 {
	if n < 0 || n > 24 {
		return 0
	}
	return n
}

func takeRecordUID(attrs map[string]any) string {
	raw, ok := attrs[recordUIDKey]
	delete(attrs, recordUIDKey)
	if ok {
		if s, isStr := raw.(string); isStr && recordUIDRe.MatchString(s) {
			return s
		}
	}
	return newRecordUID()
}

func newRecordUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("otlp: record uid: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

func countLogRecords(rl ResourceLogs) int {
	n := 0
	for _, sl := range rl.ScopeLogs {
		n += len(sl.LogRecords)
	}
	return n
}

func summariseReasons(reasons map[string]int) string {
	if len(reasons) == 0 {
		return ""
	}
	if len(reasons) == 1 {
		for k := range reasons {
			return k
		}
	}
	return "records rejected"
}

func parseAPILevel(v any) *int {
	n, ok := coerceBoundedInt(v)
	if !ok {
		return nil
	}
	i := int(n)
	return &i
}

func normaliseNumericAttrs(attrs map[string]any) map[string]any {
	out := make(map[string]any, len(attrs))
	for k, v := range attrs {
		if k == androidStartReason {
			if n, ok := coerceFiniteNumber(v); ok {
				out[k] = n
			} else if s, ok := v.(string); ok {
				out[k] = s
			}
			continue
		}
		if k == jankThresholdKey {
			if n, ok := coerceFiniteNumber(v); ok && numberAtLeastZero(n) {
				out[k] = n
			}
			continue
		}
		if integerAttrKeys[k] {
			if n, ok := coerceBoundedInt(v); ok {
				out[k] = n
			}
			continue
		}
		out[k] = v
	}
	return out
}

func numberAtLeastZero(v any) bool {
	switch x := v.(type) {
	case int64:
		return x >= 0
	case int:
		return x >= 0
	case float64:
		return x >= 0
	default:
		return false
	}
}

func coerceBoundedInt(v any) (int64, bool) {
	n, ok := coerceIntegral(v)
	if !ok || n < 0 || n > maxIntAttr {
		return 0, false
	}
	return n, true
}

func coerceIntegral(v any) (int64, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || x != math.Trunc(x) {
			return 0, false
		}
		if x < float64(math.MinInt64) || x > float64(math.MaxInt64) {
			return 0, false
		}
		return int64(x), true
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n, true
		}
		f, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) {
			return 0, false
		}
		if f < float64(math.MinInt64) || f > float64(math.MaxInt64) {
			return 0, false
		}
		return int64(f), true
	default:
		return 0, false
	}
}

func coerceFiniteNumber(v any) (any, bool) {
	switch x := v.(type) {
	case int64:
		return x, true
	case int:
		return int64(x), true
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return nil, false
		}
		if x == math.Trunc(x) && x >= float64(math.MinInt64) && x <= float64(math.MaxInt64) {
			return int64(x), true
		}
		return x, true
	case string:
		if n, err := strconv.ParseInt(x, 10, 64); err == nil {
			return n, true
		}
		f, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		if f == math.Trunc(f) && f >= float64(math.MinInt64) && f <= float64(math.MaxInt64) {
			return int64(f), true
		}
		return f, true
	default:
		return nil, false
	}
}

func stripNUL(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	return strings.ReplaceAll(s, "\x00", "")
}

func stripNULAny(v any) any {
	switch x := v.(type) {
	case string:
		return stripNUL(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = stripNULAny(item)
		}
		return out
	case map[string]any:
		return stripNULMap(x)
	default:
		return v
	}
}

func stripNULMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[stripNUL(k)] = stripNULAny(v)
	}
	return out
}
