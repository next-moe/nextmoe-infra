package model

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestEmptyAllowedScopesFallbackStaysCore pins the fallback list against silent
// widening. A client row with no allowed_scopes gets oidcCoreScopes, so every
// scope added there is granted retroactively to every legacy client that was
// never reviewed for it — `preferences` writes to the account's stored data and
// must be ticked per client instead.
func TestEmptyAllowedScopesFallbackStaysCore(t *testing.T) {
	fallback := (&OAuthClient{}).allowedScopeList()
	if !slices.Equal(fallback, []string{"openid", "profile", "email"}) {
		t.Fatalf("empty allowed_scopes falls back to %v, want the three OIDC core scopes only", fallback)
	}

	if _, ok := (&OAuthClient{}).CheckScope("preferences"); ok {
		t.Fatal("a client with no allowed_scopes must not be able to ask for preferences")
	}

	granted, _ := json.Marshal([]string{"openid", "preferences"})
	if disallowed, ok := (&OAuthClient{AllowedScopes: granted}).CheckScope("openid preferences"); !ok {
		t.Fatalf("an explicitly granted preferences scope was refused at %q", disallowed)
	}
}
