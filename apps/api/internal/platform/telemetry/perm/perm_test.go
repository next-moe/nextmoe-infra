package perm_test

import (
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/telemetry/perm"
)

var goldenGrants = map[authz.Permission][]string{
	perm.View:   {"ren"},
	perm.Manage: {"ren"},
}

var allRoles = []string{"user", "creator", "moderator", "admin", "ren"}

func TestGoldenBundles(t *testing.T) {
	for p, granted := range goldenGrants {
		grantedSet := make(map[string]bool, len(granted))
		for _, r := range granted {
			grantedSet[r] = true
		}
		for _, role := range allRoles {
			want := grantedSet[role]
			if got := perm.Resolver.Can([]string{role}, p); got != want {
				t.Errorf("Can([%q], %q) = %v, want %v", role, p, got, want)
			}
		}
	}
}

func TestNonBundleRolesGrantNothing(t *testing.T) {
	for _, role := range []string{"user", "creator", "moderator", "admin", "", "legacy_top_tier_alias"} {
		for p := range goldenGrants {
			if perm.Resolver.Can([]string{role}, p) {
				t.Errorf("non-ren role %q must grant nothing, but grants %q", role, p)
			}
		}
	}
}

func TestManageIsNonDelegable(t *testing.T) {
	if !perm.NonDelegable.Has(perm.Manage) {
		t.Errorf("%q must be non-delegable", perm.Manage)
	}
	if perm.NonDelegable.Has(perm.View) {
		t.Errorf("%q must stay delegable", perm.View)
	}
}
