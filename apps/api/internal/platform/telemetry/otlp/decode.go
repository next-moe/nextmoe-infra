package otlp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type ExportLogsServiceRequest struct {
	ResourceLogs []ResourceLogs `json:"resourceLogs"`
}

type ResourceLogs struct {
	Resource  Resource    `json:"resource"`
	ScopeLogs []ScopeLogs `json:"scopeLogs"`
}

type Resource struct {
	Attributes []KeyValue `json:"attributes"`
}

type ScopeLogs struct {
	Scope      Scope       `json:"scope"`
	LogRecords []LogRecord `json:"logRecords"`
}

type Scope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type LogRecord struct {
	TimeUnixNano         FlexibleInt64 `json:"timeUnixNano"`
	ObservedTimeUnixNano FlexibleInt64 `json:"observedTimeUnixNano"`
	SeverityNumber       Severity      `json:"severityNumber"`
	SeverityText         string        `json:"severityText"`
	EventName            string        `json:"eventName"`
	Attributes           []KeyValue    `json:"attributes"`
	Body                 *AnyValue     `json:"body"`
}

type KeyValue struct {
	Key   string    `json:"key"`
	Value *AnyValue `json:"value"`
}

type ArrayValue struct {
	Values []*AnyValue `json:"values"`
}

type KeyValueList struct {
	Values []KeyValue `json:"values"`
}

type AnyValue struct {
	StringValue *string          `json:"stringValue"`
	BoolValue   *bool            `json:"boolValue"`
	IntValue    *FlexibleInt64   `json:"intValue"`
	DoubleValue *FlexibleFloat64 `json:"doubleValue"`
	BytesValue  *string          `json:"bytesValue"`
	ArrayValue  *ArrayValue      `json:"arrayValue"`
	KvlistValue *KeyValueList    `json:"kvlistValue"`
}

type FlexibleInt64 int64

func (v *FlexibleInt64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*v = FlexibleInt64(n)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	i, err := n.Int64()
	if err != nil {
		return err
	}
	*v = FlexibleInt64(i)
	return nil
}

type FlexibleFloat64 struct {
	Set bool
	Val float64
}

func (f *FlexibleFloat64) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	f.Set = true
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		switch s {
		case "NaN":
			f.Val = math.NaN()
			return nil
		case "Infinity":
			f.Val = math.Inf(1)
			return nil
		case "-Infinity":
			f.Val = math.Inf(-1)
			return nil
		default:
			v, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return err
			}
			f.Val = v
			return nil
		}
	}
	return json.Unmarshal(b, &f.Val)
}

type Severity int16

var severityNames = map[string]int16{
	"SEVERITY_NUMBER_UNSPECIFIED": 0,
	"SEVERITY_NUMBER_TRACE":       1,
	"SEVERITY_NUMBER_TRACE2":      2,
	"SEVERITY_NUMBER_TRACE3":      3,
	"SEVERITY_NUMBER_TRACE4":      4,
	"SEVERITY_NUMBER_DEBUG":       5,
	"SEVERITY_NUMBER_DEBUG2":      6,
	"SEVERITY_NUMBER_DEBUG3":      7,
	"SEVERITY_NUMBER_DEBUG4":      8,
	"SEVERITY_NUMBER_INFO":        9,
	"SEVERITY_NUMBER_INFO2":       10,
	"SEVERITY_NUMBER_INFO3":       11,
	"SEVERITY_NUMBER_INFO4":       12,
	"SEVERITY_NUMBER_WARN":        13,
	"SEVERITY_NUMBER_WARN2":       14,
	"SEVERITY_NUMBER_WARN3":       15,
	"SEVERITY_NUMBER_WARN4":       16,
	"SEVERITY_NUMBER_ERROR":       17,
	"SEVERITY_NUMBER_ERROR2":      18,
	"SEVERITY_NUMBER_ERROR3":      19,
	"SEVERITY_NUMBER_ERROR4":      20,
	"SEVERITY_NUMBER_FATAL":       21,
	"SEVERITY_NUMBER_FATAL2":      22,
	"SEVERITY_NUMBER_FATAL3":      23,
	"SEVERITY_NUMBER_FATAL4":      24,
}

func (s *Severity) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		var name string
		if err := json.Unmarshal(b, &name); err != nil {
			return err
		}
		if n, ok := severityNames[name]; ok {
			*s = Severity(n)
			return nil
		}
		if n, err := strconv.ParseInt(name, 10, 16); err == nil {
			*s = Severity(n)
			return nil
		}
		*s = 0
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	i, err := n.Int64()
	if err != nil {
		return err
	}
	*s = Severity(i)
	return nil
}

func Decode(data []byte) (*ExportLogsServiceRequest, error) {
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 || trim[0] != '{' {
		return nil, fmt.Errorf("body is not a JSON object")
	}
	var req ExportLogsServiceRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (a *AnyValue) GoValue() any {
	if a == nil {
		return nil
	}
	switch {
	case a.StringValue != nil:
		return *a.StringValue
	case a.BoolValue != nil:
		return *a.BoolValue
	case a.IntValue != nil:
		return int64(*a.IntValue)
	case a.DoubleValue != nil && a.DoubleValue.Set:
		v := a.DoubleValue.Val
		if math.IsNaN(v) {
			return "NaN"
		}
		if math.IsInf(v, 1) {
			return "Infinity"
		}
		if math.IsInf(v, -1) {
			return "-Infinity"
		}
		return v
	case a.BytesValue != nil:
		return *a.BytesValue
	case a.ArrayValue != nil:
		out := make([]any, 0, len(a.ArrayValue.Values))
		for _, item := range a.ArrayValue.Values {
			out = append(out, item.GoValue())
		}
		return out
	case a.KvlistValue != nil:
		out := make(map[string]any, len(a.KvlistValue.Values))
		for _, kv := range a.KvlistValue.Values {
			out[kv.Key] = kv.Value.GoValue()
		}
		return out
	default:
		return nil
	}
}

func attributesMap(kvs []KeyValue) map[string]any {
	if len(kvs) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		out[kv.Key] = kv.Value.GoValue()
	}
	return out
}

func attrString(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok || v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return strings.TrimSpace(fmt.Sprint(x))
	}
}
