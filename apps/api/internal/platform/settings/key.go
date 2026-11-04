package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
)

type Kind string

const (
	KindBool       Kind = "bool"
	KindInt        Kind = "int"
	KindFloat      Kind = "float"
	KindString     Kind = "string"
	KindEnum       Kind = "enum"
	KindStringList Kind = "string_list"
)

type Source string

const (
	SourceDefault Source = "default"
	SourceEnv     Source = "env"
	SourceDB      Source = "db"
	SourceSite    Source = "site"
)

type Meta struct {
	Name       string
	Kind       Kind
	EnvVar     string
	DescEN     string
	DescZH     string
	Enum       []string
	Min        *float64
	Max        *float64
	Pattern    string
	Multiline  bool
	SiteScoped bool
	Public     bool
}

func F(v float64) *float64 { return &v }

type Entry interface {
	Meta() Meta
	Default() any
	Current() any
	CurrentForSite(siteID uint) any
	Source() Source
	Decode(raw json.RawMessage) (any, error)
	ParseEnv(s string) (any, error)
	Validate(v any) error
	apply(v any, src Source)
	applySites(vals map[uint]any)
	siteValues() map[uint]any
}

type snapshot[T any] struct {
	value T
	src   Source
}

type Key[T any] struct {
	meta    Meta
	def     T
	current atomic.Pointer[snapshot[T]]
	sites   atomic.Pointer[map[uint]T]
	pattern *regexp.Regexp
}

func (k *Key[T]) Get() T {
	return k.current.Load().value
}

func (k *Key[T]) ForSite(siteID uint) T {
	if m := k.sites.Load(); m != nil {
		if v, ok := (*m)[siteID]; ok {
			return v
		}
	}
	return k.Get()
}

func (k *Key[T]) Name() string { return k.meta.Name }

func (k *Key[T]) Meta() Meta { return k.meta }

func (k *Key[T]) Default() any { return k.def }

func (k *Key[T]) Current() any {
	return k.current.Load().value
}

func (k *Key[T]) CurrentForSite(siteID uint) any {
	return k.ForSite(siteID)
}

func (k *Key[T]) Source() Source {
	return k.current.Load().src
}

func (k *Key[T]) apply(v any, src Source) {
	typed, ok := v.(T)
	if !ok {
		panic(fmt.Sprintf("settings: %s: apply got %T, want %T", k.meta.Name, v, *new(T)))
	}
	k.current.Store(&snapshot[T]{value: typed, src: src})
}

func (k *Key[T]) applySites(vals map[uint]any) {
	m := make(map[uint]T, len(vals))
	for id, v := range vals {
		typed, ok := v.(T)
		if !ok {
			panic(fmt.Sprintf("settings: %s: applySites got %T, want %T", k.meta.Name, v, *new(T)))
		}
		m[id] = typed
	}
	k.sites.Store(&m)
}

func (k *Key[T]) siteValues() map[uint]any {
	out := map[uint]any{}
	if m := k.sites.Load(); m != nil {
		for id, v := range *m {
			out[id] = v
		}
	}
	return out
}

var keyNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)

func Bool(m Meta, def bool) *Key[bool] {
	m.Kind = KindBool
	return mustKey(m, def)
}

func Int(m Meta, def int64) *Key[int64] {
	m.Kind = KindInt
	return mustKey(m, def)
}

func Float(m Meta, def float64) *Key[float64] {
	m.Kind = KindFloat
	return mustKey(m, def)
}

func String(m Meta, def string) *Key[string] {
	m.Kind = KindString
	return mustKey(m, def)
}

func Enum(m Meta, def string) *Key[string] {
	m.Kind = KindEnum
	return mustKey(m, def)
}

func StringList(m Meta, def []string) *Key[[]string] {
	m.Kind = KindStringList
	return mustKey(m, def)
}

func mustKey[T any](m Meta, def T) *Key[T] {
	re, err := checkMeta(m)
	if err != nil {
		panic("settings: " + err.Error())
	}
	k := &Key[T]{meta: m, def: def, pattern: re}
	k.current.Store(&snapshot[T]{value: def, src: SourceDefault})
	if err := k.Validate(def); err != nil {
		panic(err.Error())
	}
	return k
}

func checkMeta(m Meta) (*regexp.Regexp, error) {
	if m.Name == "" || !keyNameRe.MatchString(m.Name) {
		return nil, fmt.Errorf("invalid key name %q", m.Name)
	}
	if m.DescEN == "" || m.DescZH == "" {
		return nil, fmt.Errorf("%s: descriptions must be non-empty", m.Name)
	}
	if m.Kind == KindEnum && len(m.Enum) == 0 {
		return nil, fmt.Errorf("%s: enum must be non-empty", m.Name)
	}
	if m.Min != nil && m.Max != nil && *m.Min > *m.Max {
		return nil, fmt.Errorf("%s: min %v is greater than max %v", m.Name, *m.Min, *m.Max)
	}
	if m.Pattern == "" {
		return nil, nil
	}
	re, err := regexp.Compile(m.Pattern)
	if err != nil {
		return nil, fmt.Errorf("%s: invalid pattern: %v", m.Name, err)
	}
	return re, nil
}

func (k *Key[T]) Decode(raw json.RawMessage) (any, error) {
	v, err := decodeJSONValue(raw)
	if err != nil {
		return nil, expectedJSON(k.meta.Kind)
	}
	switch k.meta.Kind {
	case KindBool:
		b, ok := v.(bool)
		if !ok {
			return nil, expectedJSON(KindBool)
		}
		return b, nil
	case KindInt:
		n, ok := v.(json.Number)
		if !ok {
			return nil, expectedJSON(KindInt)
		}
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("expected a JSON number with no fractional part")
		}
		return i, nil
	case KindFloat:
		n, ok := v.(json.Number)
		if !ok {
			return nil, expectedJSON(KindFloat)
		}
		f, err := strconv.ParseFloat(string(n), 64)
		if err != nil {
			return nil, expectedJSON(KindFloat)
		}
		return f, nil
	case KindString, KindEnum:
		s, ok := v.(string)
		if !ok {
			return nil, expectedJSON(k.meta.Kind)
		}
		return s, nil
	case KindStringList:
		arr, ok := v.([]any)
		if !ok {
			return nil, expectedJSON(KindStringList)
		}
		out := make([]string, len(arr))
		for i, item := range arr {
			s, ok := item.(string)
			if !ok {
				return nil, expectedJSON(KindStringList)
			}
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("%s: unknown kind %q", k.meta.Name, k.meta.Kind)
	}
}

func decodeJSONValue(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("empty")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if v == nil {
		return nil, fmt.Errorf("null")
	}
	return v, nil
}

func expectedJSON(kind Kind) error {
	switch kind {
	case KindBool:
		return fmt.Errorf("expected a JSON boolean")
	case KindInt, KindFloat:
		return fmt.Errorf("expected a JSON number")
	case KindString, KindEnum:
		return fmt.Errorf("expected a JSON string")
	case KindStringList:
		return fmt.Errorf("expected a JSON array of strings")
	default:
		return fmt.Errorf("expected JSON for kind %s", kind)
	}
}

func (k *Key[T]) ParseEnv(s string) (any, error) {
	switch k.meta.Kind {
	case KindBool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return nil, err
		}
		return b, nil
	case KindInt:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	case KindFloat:
		n, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	case KindString, KindEnum:
		return s, nil
	case KindStringList:
		return splitCSV(s), nil
	default:
		return nil, fmt.Errorf("%s: unknown kind %q", k.meta.Name, k.meta.Kind)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func (k *Key[T]) Validate(v any) error {
	m := k.meta
	switch m.Kind {
	case KindBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("%s: expected bool", m.Name)
		}
		return nil
	case KindInt:
		n, ok := v.(int64)
		if !ok {
			return fmt.Errorf("%s: expected int", m.Name)
		}
		return checkBounds(m, float64(n), n)
	case KindFloat:
		n, ok := v.(float64)
		if !ok {
			return fmt.Errorf("%s: expected float", m.Name)
		}
		return checkBounds(m, n, n)
	case KindString, KindEnum:
		s, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: expected string", m.Name)
		}
		if m.Kind == KindEnum && !slices.Contains(m.Enum, s) {
			return fmt.Errorf("%s: %q is not one of %s", m.Name, s, strings.Join(m.Enum, ", "))
		}
		return k.matchPattern(s)
	case KindStringList:
		list, ok := v.([]string)
		if !ok {
			return fmt.Errorf("%s: expected string_list", m.Name)
		}
		for _, item := range list {
			if strings.TrimSpace(item) == "" {
				return fmt.Errorf("%s: list items must be non-empty", m.Name)
			}
			if err := k.matchPattern(item); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("%s: unknown kind %q", m.Name, m.Kind)
	}
}

func checkBounds(m Meta, n float64, displayed any) error {
	if m.Min != nil && n < *m.Min {
		return fmt.Errorf("%s: %v is below the minimum %v", m.Name, displayed, *m.Min)
	}
	if m.Max != nil && n > *m.Max {
		return fmt.Errorf("%s: %v is above the maximum %v", m.Name, displayed, *m.Max)
	}
	return nil
}

func (k *Key[T]) matchPattern(s string) error {
	if k.pattern == nil {
		return nil
	}
	if !k.pattern.MatchString(s) {
		return fmt.Errorf("%s: %q does not match %s", k.meta.Name, s, k.meta.Pattern)
	}
	return nil
}
