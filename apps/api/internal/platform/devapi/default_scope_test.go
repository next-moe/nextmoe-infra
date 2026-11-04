package devapi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"
)

func keyScopes(t *testing.T, k *DeveloperAPIKey) []string {
	t.Helper()
	var out []string
	if err := json.Unmarshal(k.Scopes, &out); err != nil {
		t.Fatalf("decode scopes: %v", err)
	}
	return out
}

func TestEveryMintedKeyCarriesLetmoeFiles(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_letmoe_default"
	makeApp(t, clientID, true)
	svc, repo := newService(t)
	ctx := context.Background()

	for name, in := range map[string]MintKeyInput{
		"no scopes named":  {Name: "bare"},
		"store:read named": {Name: "store", Scopes: []string{ScopeStoreRead}},
		"already named":    {Name: "named", Scopes: []string{ScopeCatalogRead, ScopeLetmoeFiles}},
	} {
		key, plaintext, err := svc.MintKey(ctx, clientID, in, 42)
		if err != nil {
			t.Fatalf("%s: mint: %v", name, err)
		}
		got := keyScopes(t, key)
		if n := len(slices.DeleteFunc(slices.Clone(got), func(s string) bool { return s != ScopeLetmoeFiles })); n != 1 {
			t.Errorf("%s: scopes = %v, want letmoe:files exactly once", name, got)
		}
		cred, err := repo.ResolveByHash(ctx, HashKey(plaintext), time.Now())
		if err != nil || cred == nil || !cred.HasScope(ScopeLetmoeFiles) {
			t.Errorf("%s: resolved credential = (%+v, %v), want letmoe:files", name, cred, err)
		}
	}
}

func TestRotationKeepsAKeyThatLostLetmoeFiles(t *testing.T) {
	cleanup(t)
	const clientID = "devapitest_letmoe_revoked"
	makeApp(t, clientID, true)
	svc, _ := newService(t)
	ctx := context.Background()

	key, _, err := svc.MintKey(ctx, clientID, MintKeyInput{Name: "k"}, 42)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if err := testDB.Exec(`UPDATE developer_api_keys SET scopes = scopes - ? WHERE id = ?`, ScopeLetmoeFiles, key.ID).Error; err != nil {
		t.Fatalf("operator revoke: %v", err)
	}
	rotated, _, err := svc.RotateKey(ctx, key.ID, 42)
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if slices.Contains(keyScopes(t, rotated), ScopeLetmoeFiles) {
		t.Errorf("rotated scopes = %v: rotation must not undo an operator's revocation", keyScopes(t, rotated))
	}
}
