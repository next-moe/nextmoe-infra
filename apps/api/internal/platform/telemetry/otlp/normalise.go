package otlp

import (
	"math"
	"regexp"
	"strconv"
	"time"
)

var sessionIDRe = regexp.MustCompile(`^[0-9a-f]{32}$`)

var numericAttrKeys = map[string]bool{
	"app.startup.ttid_ms":       true,
	"app.jank.frame_count":      true,
	"app.jank.frames":           true,
	"app.jank.threshold":        true,
	"app.session.errors":        true,
	"android.exit.reason":       true,
	"http.response.status_code": true,
}

const androidStartReason = "android.start.reason"

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

func DayOf(eventTime, receipt time.Time) time.Time {
	eventTime = eventTime.UTC()
	receipt = receipt.UTC()
	lo := receipt.Add(-30 * 24 * time.Hour)
	hi := receipt.Add(24 * time.Hour)
	if !eventTime.Before(lo) && !eventTime.After(hi) {
		return DateUTC(eventTime)
	}
	return DateUTC(receipt)
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
		res := attributesMap(rl.Resource.Attributes)
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
				if lr.EventName == "" {
					out.Rejected++
					reasons["empty eventName"]++
					continue
				}
				rec := base
				rec.EventName = lr.EventName
				rec.Severity = int16(lr.SeverityNumber)
				rec.EventTime = eventTimeOf(lr, receipt)
				rec.EventDay = DayOf(rec.EventTime, receipt)
				attrs := attributesMap(lr.Attributes)
				delete(attrs, "session.previous_id")
				if sid, ok := attrs["session.id"].(string); ok && sessionIDRe.MatchString(sid) {
					rec.SessionID = sid
				}
				delete(attrs, "session.id")
				rec.Attributes = normaliseNumericAttrs(attrs)
				if lr.Body != nil && lr.Body.StringValue != nil {
					s := *lr.Body.StringValue
					rec.Body = &s
				}
				out.Records = append(out.Records, rec)
			}
		}
	}
	out.Reason = summariseReasons(reasons)
	return out
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
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case int64:
		n := int(x)
		if int64(n) != x {
			return nil
		}
		return &n
	case float64:
		if x != math.Trunc(x) || x < math.MinInt32 || x > math.MaxInt32 {
			return nil
		}
		n := int(x)
		return &n
	case string:
		n, err := strconv.Atoi(x)
		if err != nil {
			return nil
		}
		return &n
	default:
		return nil
	}
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
		if numericAttrKeys[k] {
			if n, ok := coerceFiniteNumber(v); ok {
				out[k] = n
			}
			continue
		}
		out[k] = v
	}
	return out
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
