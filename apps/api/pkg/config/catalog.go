package config

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	"api/pkg/secretbox"
)

type Source string

const (
	SourceEnv     Source = "env"
	SourceStore   Source = "store"
	SourceDefault Source = "default"
)

type Var struct {
	Key     string
	Default string
	Value   string
	Source  Source
}

type Entry struct {
	Key         string `json:"key"`
	Source      Source `json:"source"`
	Set         bool   `json:"set"`
	Value       string `json:"value,omitempty"`
	Default     string `json:"default,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

var (
	mu      sync.Mutex
	catalog = map[string]Var{}
	store   storeState
)

// Env reads a variable the way Load does — environment, then the config
// store, then the default — and records it, so it shows up in the console.
func Env(key, def string) string {
	return lookup(key, def)
}

func lookup(key, def string) string {
	value, source := os.Getenv(key), SourceEnv
	mu.Lock()
	defer mu.Unlock()
	if value == "" {
		if v, ok := store.values[key]; ok {
			value, source = v, SourceStore
		}
	}
	if value == "" {
		value, source = def, SourceDefault
	}
	catalog[key] = Var{Key: key, Default: def, Value: value, Source: source}
	return value
}

func Vars() []Var {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Var, 0, len(catalog))
	for _, v := range catalog {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func Snapshot() []Entry {
	vars := Vars()
	out := make([]Entry, 0, len(vars))
	for _, v := range vars {
		e := Entry{Key: v.Key, Source: v.Source, Set: v.Value != ""}
		if IsSecret(v.Key) {
			e.Fingerprint = Fingerprint(v.Value)
		} else {
			e.Value, e.Default = v.Value, v.Default
		}
		out = append(out, e)
	}
	return out
}

func Fingerprint(value string) string {
	mu.Lock()
	key := store.key
	mu.Unlock()
	return secretbox.Fingerprint(key, value)
}

func Scope() string {
	mu.Lock()
	defer mu.Unlock()
	return store.scope
}

func StoreKey() []byte {
	mu.Lock()
	defer mu.Unlock()
	return store.key
}

var ownPrefixes = []string{"KUN_", "JWT_", "REDIS_"}

// UnreadEnv lists the variables this process was handed that nothing read:
// a panel or compose line whose consumer is gone, or a typo of a real name.
func UnreadEnv(known map[string]bool) []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, read := catalog[name]; read || known[name] || !hasOwnPrefix(name) {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func hasOwnPrefix(name string) bool {
	for _, p := range ownPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

var secretName = regexp.MustCompile(`SECRET|PASSWORD|TOKEN|KEK|_KEYS?$|_KEY_`)

// IsSecret is decided by name and fails closed: a new variable whose name
// looks like a credential is masked everywhere without anyone listing it.
func IsSecret(key string) bool { return secretName.MatchString(key) }

const KEKEnv = "KUN_CONFIG_KEK"

// IsBootstrap marks what has to be known before the store can be read: the
// database the store lives in and the key that opens it.
func IsBootstrap(key string) bool {
	return key == KEKEnv || strings.HasPrefix(key, "KUN_PG_") || strings.Contains(key, "_PG_")
}

var topologyName = regexp.MustCompile(`_(HOST|PORT)$|^KUN_ENV$`)

// IsTopology marks how containers listen and find each other. It stays in the
// compose file, where a change is reviewed with the deployment it belongs to.
func IsTopology(key string) bool { return topologyName.MatchString(key) }

// The centrifugo container reads these two from the Dokploy panel under its
// own names; stored here, the console would advise deleting the panel copy
// and realtime would stop authenticating.
var panelOnly = map[string]bool{
	"KUN_CHAT_CENTRIFUGO_API_KEY":    true,
	"KUN_CHAT_REALTIME_TOKEN_SECRET": true,
}

func IsPanelOnly(key string) bool { return panelOnly[key] }

func Storable(key string) bool {
	return !IsBootstrap(key) && !IsTopology(key) && !IsPanelOnly(key)
}
