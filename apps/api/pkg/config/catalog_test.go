package config

import (
	"bytes"
	"testing"

	"api/pkg/secretbox"
)

func withStore(t *testing.T, values map[string]string) {
	t.Helper()
	mu.Lock()
	prev := store
	store = storeState{scope: "oauth", key: bytes.Repeat([]byte{1}, 32), values: values}
	catalog = map[string]Var{}
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		store = prev
		mu.Unlock()
	})
}

func TestLookupPrecedence(t *testing.T) {
	withStore(t, map[string]string{"KUN_T_STORED": "from-store", "KUN_T_BOTH": "from-store"})
	t.Setenv("KUN_T_BOTH", "from-env")

	cases := []struct {
		key, def, want string
		source         Source
	}{
		{"KUN_T_BOTH", "d", "from-env", SourceEnv},
		{"KUN_T_STORED", "d", "from-store", SourceStore},
		{"KUN_T_NONE", "d", "d", SourceDefault},
	}
	for _, c := range cases {
		if got := lookup(c.key, c.def); got != c.want {
			t.Errorf("lookup(%s) = %q, want %q", c.key, got, c.want)
		}
	}
	vars := map[string]Var{}
	for _, v := range Vars() {
		vars[v.Key] = v
	}
	for _, c := range cases {
		if vars[c.key].Source != c.source {
			t.Errorf("%s recorded source %q, want %q", c.key, vars[c.key].Source, c.source)
		}
	}
}

func TestSnapshotMasksSecrets(t *testing.T) {
	withStore(t, nil)
	t.Setenv("KUN_T_CLIENT_SECRET", "s3cr3t")
	t.Setenv("KUN_T_BUCKET", "bucket")
	lookup("KUN_T_CLIENT_SECRET", "")
	lookup("KUN_T_BUCKET", "dflt")
	for _, e := range Snapshot() {
		switch e.Key {
		case "KUN_T_CLIENT_SECRET":
			if e.Value != "" || e.Default != "" || e.Fingerprint == "" || !e.Set {
				t.Errorf("secret entry leaks or lacks a fingerprint: %+v", e)
			}
		case "KUN_T_BUCKET":
			if e.Value != "bucket" || e.Default != "dflt" || e.Fingerprint != "" {
				t.Errorf("plain entry = %+v", e)
			}
		}
	}
}

func TestUnreadEnv(t *testing.T) {
	withStore(t, nil)
	t.Setenv("KUN_T_READ", "1")
	t.Setenv("KUN_T_ORPHAN", "1")
	t.Setenv("KUN_T_FLOOR", "1")
	t.Setenv("PATH_LIKE_T", "1")
	lookup("KUN_T_READ", "")
	got := map[string]bool{}
	for _, k := range UnreadEnv(map[string]bool{"KUN_T_FLOOR": true}) {
		got[k] = true
	}
	if !got["KUN_T_ORPHAN"] || got["KUN_T_READ"] || got["KUN_T_FLOOR"] || got["PATH_LIKE_T"] {
		t.Errorf("UnreadEnv = %v", got)
	}
}

func TestClassification(t *testing.T) {
	secret := []string{"JWT_SECRET", "KUN_IMAGE_S3_ACCESS_KEY", "KUN_TELEMETRY_S3_ACCESS_KEY_ID",
		"KUN_OIDC_KEY_ENC_KEY", "KUN_LLM_KEK", "KUN_AGENT_POW_KEYS", "KUN_AI_OMNI_TOKEN", "REDIS_PASSWORD"}
	plain := []string{"KUN_IMAGE_CLIENT_ID", "KUN_DEV_PORTAL_CLIENT_IDS", "JWT_COOKIE_NAME", "KUN_IMAGE_S3_BUCKET"}
	for _, k := range secret {
		if !IsSecret(k) {
			t.Errorf("%s must be a secret", k)
		}
	}
	for _, k := range plain {
		if IsSecret(k) {
			t.Errorf("%s must not be a secret", k)
		}
	}
	for _, k := range []string{"KUN_PG_PASSWORD", "KUN_CATALOG_PG_DATABASE", "KUN_PG_MAX_OPEN_CONNS", KEKEnv} {
		if Storable(k) {
			t.Errorf("%s is bootstrap and must not be storable", k)
		}
	}
	for _, k := range []string{"KUN_CATALOG_HOST", "KUN_TRUST_PORT", "REDIS_HOST", "KUN_ENV"} {
		if Storable(k) {
			t.Errorf("%s is topology and must not be storable", k)
		}
	}
	if Storable("KUN_CHAT_CENTRIFUGO_API_KEY") {
		t.Error("a value centrifugo also reads from the panel must not be storable")
	}
	for _, k := range []string{"JWT_SECRET", "KUN_IMAGE_S3_ENDPOINT", "KUN_LLM_KEK"} {
		if !Storable(k) {
			t.Errorf("%s must be storable", k)
		}
	}
}

func TestResolveScopedRowWins(t *testing.T) {
	key := bytes.Repeat([]byte{2}, 32)
	seal := func(v string) []byte {
		b, err := secretbox.Seal(key, v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	rows := []StoredSecret{
		{Scope: "", Key: "K", Cipher: seal("all")},
		{Scope: "chat", Key: "K", Cipher: seal("chat")},
		{Scope: "oauth", Key: "ONLY_OAUTH", Cipher: seal("o")},
	}
	got, err := Resolve(key, "chat", rows)
	if err != nil {
		t.Fatal(err)
	}
	if got["K"] != "chat" || got["ONLY_OAUTH"] != "" {
		t.Errorf("chat resolved %v", got)
	}
	got, _ = Resolve(key, "catalog", rows)
	if got["K"] != "all" {
		t.Errorf("catalog resolved %v", got)
	}
	if _, err := Resolve(bytes.Repeat([]byte{3}, 32), "chat", rows); err == nil {
		t.Error("a wrong KEK must fail the load, not start the service without its secrets")
	}
}
