package envconfig

import (
	"bytes"
	"testing"
	"time"

	"api/pkg/config"
	"api/pkg/secretbox"
)

var testKEK = bytes.Repeat([]byte{5}, 32)

func snap(service string, started time.Time, loaded bool, entries ...config.Entry) snapshot {
	sn := snapshot{BootSnapshot: BootSnapshot{Service: service, StartedAt: started, StoreLoaded: loaded}, entries: map[string]config.Entry{}}
	for _, e := range entries {
		sn.entries[e.Key] = e
	}
	return sn
}

func storedRow(t *testing.T, scope, key, value string, updated time.Time) SecretRow {
	t.Helper()
	sealed, err := secretbox.Seal(testKEK, value)
	if err != nil {
		t.Fatal(err)
	}
	return SecretRow{ConfigSecret: ConfigSecret{
		Scope: scope, Key: key, Cipher: sealed, Fingerprint: secretbox.Fingerprint(testKEK, value), UpdatedAt: updated,
	}}
}

func TestBuildVarIssues(t *testing.T) {
	t0 := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	fp := func(v string) string { return secretbox.Fingerprint(testKEK, v) }
	const key = "KUN_X_CLIENT_SECRET"
	snaps := []snapshot{
		snap("oauth", t0, true, config.Entry{Key: key, Source: config.SourceEnv, Set: true, Fingerprint: fp("a")}),
		snap("catalog", t0, true, config.Entry{Key: key, Source: config.SourceEnv, Set: true, Fingerprint: fp("old")}),
		snap("chat", t0, true, config.Entry{Key: key, Source: config.SourceStore, Set: true, Fingerprint: fp("a")}),
		snap("trust", t0, false, config.Entry{Key: key, Source: config.SourceDefault}),
		snap("image", t0, true, config.Entry{Key: key, Source: config.SourceStore, Set: true, Fingerprint: fp("a")}),
	}
	rows := []SecretRow{
		storedRow(t, "", key, "a", t0.Add(-time.Hour)),
		storedRow(t, "image", key, "b", t0.Add(time.Hour)),
	}
	v := buildVar(key, snaps, rows, testKEK)

	want := map[string]string{
		"oauth":   IssueEnvRedundant,
		"catalog": IssueEnvShadows,
		"chat":    "",
		"trust":   IssueKEKMissing,
		"image":   IssueRestart,
	}
	for _, s := range v.Services {
		if s.Issue != want[s.Service] {
			t.Errorf("%s issue = %q, want %q", s.Service, s.Issue, want[s.Service])
		}
	}
	if !v.Diverged {
		t.Error("two different fingerprints across services must read as diverged")
	}
	for _, st := range v.Stored {
		if st.Value != "" {
			t.Errorf("a secret's stored value must never be returned: %+v", st)
		}
	}
}

func TestBuildVarShowsPlainStoredValue(t *testing.T) {
	t0 := time.Now()
	const key = "KUN_X_BUCKET"
	snaps := []snapshot{snap("image", t0, true, config.Entry{Key: key, Source: config.SourceStore, Set: true, Value: "b1"})}
	v := buildVar(key, snaps, []SecretRow{storedRow(t, "", key, "b1", t0.Add(-time.Minute))}, testKEK)
	if len(v.Stored) != 1 || v.Stored[0].Value != "b1" {
		t.Fatalf("stored = %+v", v.Stored)
	}
	if v.Services[0].Issue != "" {
		t.Errorf("an applied value has no issue, got %q", v.Services[0].Issue)
	}
}
