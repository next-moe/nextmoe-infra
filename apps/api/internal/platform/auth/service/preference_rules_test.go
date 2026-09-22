package service

import (
	"strings"
	"testing"
	"time"

	"api/internal/platform/auth/model"
	"api/pkg/errors"
)

func TestScopeHoldsHasNoEmptyScopeAmnesty(t *testing.T) {
	if !ScopeGrants("", "preferences") {
		t.Fatal("ScopeGrants still has to read an empty scope as everything")
	}
	if ScopeHolds("", "preferences") {
		t.Fatal("an OAuth token that negotiated no scope must not hold preferences")
	}
	if !ScopeHolds("openid profile preferences", "preferences") {
		t.Fatal("an explicit preferences scope must hold")
	}
	if ScopeHolds("openid profile email", "preferences") {
		t.Fatal("profile+email must not imply preferences")
	}
}

func TestPreferenceNamespaceBinding(t *testing.T) {
	cases := []struct {
		clientID  string
		namespace string
		want      bool
	}{
		{"", "anything-at-all", true},
		{"", "global", true},
		{"client-a", "client-a", true},
		{"client-a", "global", true},
		{"client-a", "client-b", false},
		{"client-a", "", false},
	}
	for _, c := range cases {
		if got := PreferenceNamespaceAllowed(c.clientID, c.namespace); got != c.want {
			t.Fatalf("PreferenceNamespaceAllowed(%q, %q) = %v, want %v",
				c.clientID, c.namespace, got, c.want)
		}
	}
}

func TestPreferenceNamespacePattern(t *testing.T) {
	ok := []string{"global", "a", "client-a", "kun_gal_0", strings.Repeat("a", 64)}
	bad := []string{"", "Global", "客户端", "a b", "a/b", "a.b", strings.Repeat("a", 65)}
	for _, ns := range ok {
		if !model.IsPreferenceNamespace(ns) {
			t.Fatalf("%q should be a valid namespace", ns)
		}
	}
	for _, ns := range bad {
		if model.IsPreferenceNamespace(ns) {
			t.Fatalf("%q should not be a valid namespace", ns)
		}
	}
}

func TestValidatePreferenceDoc(t *testing.T) {
	if _, err := validatePreferenceDoc([]byte(`{"a":1}`)); err != nil {
		t.Fatalf("a JSON object must pass: %v", err)
	}
	for _, body := range []string{``, `null`, `[]`, `"str"`, `7`, `{`} {
		_, err := validatePreferenceDoc([]byte(body))
		if !errors.Is(err, errors.ErrPrefDocInvalid) {
			t.Fatalf("doc %q should be rejected as not-an-object, got %v", body, err)
		}
	}

	// 64 KB is measured on the compacted document, so the padding is sized
	// against the exact envelope the compactor emits.
	const envelope = len(`{"k":""}`)
	underSized := []byte(`{"k":"` + strings.Repeat("x", model.PreferenceMaxDocBytes-envelope) + `"}`)
	if got, err := validatePreferenceDoc(underSized); err != nil {
		t.Fatalf("a doc of exactly %d bytes must pass, got %v (len=%d)", model.PreferenceMaxDocBytes, err, len(got))
	}
	overSized := []byte(`{"k":"` + strings.Repeat("x", model.PreferenceMaxDocBytes) + `"}`)
	if _, err := validatePreferenceDoc(overSized); !errors.Is(err, errors.ErrPrefDocTooLarge) {
		t.Fatalf("an oversized doc must be rejected with ErrPrefDocTooLarge, got %v", err)
	}

	// Whitespace is not payload: a pretty-printed document is measured after
	// compaction, so indentation alone can never push a caller over the limit.
	padded := []byte("{\n" + strings.Repeat(" ", 200) + "\"k\" : \"" +
		strings.Repeat("x", model.PreferenceMaxDocBytes-envelope) + "\"\n}")
	if len(padded) <= model.PreferenceMaxDocBytes {
		t.Fatalf("test fixture is %d bytes raw, it has to exceed the limit", len(padded))
	}
	if _, err := validatePreferenceDoc(padded); err != nil {
		t.Fatalf("pretty-printed doc under the compacted limit must pass: %v", err)
	}
}

func TestEffectiveNSFWDisplay(t *testing.T) {
	confirmed := time.Now()
	cases := []struct {
		at    *time.Time
		store string
		want  string
	}{
		{nil, model.NSFWDisplayBlur, model.NSFWDisplayHide},
		{nil, model.NSFWDisplayShow, model.NSFWDisplayHide},
		{nil, model.NSFWDisplayHide, model.NSFWDisplayHide},
		{&confirmed, model.NSFWDisplayBlur, model.NSFWDisplayBlur},
		{&confirmed, model.NSFWDisplayShow, model.NSFWDisplayShow},
		{&confirmed, model.NSFWDisplayHide, model.NSFWDisplayHide},
		{&confirmed, "", model.NSFWDisplayHide},
	}
	for _, c := range cases {
		if got := model.EffectiveNSFWDisplay(c.at, c.store); got != c.want {
			t.Fatalf("EffectiveNSFWDisplay(%v, %q) = %q, want %q", c.at != nil, c.store, got, c.want)
		}
	}
}
