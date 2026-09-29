package perm_test

import (
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/shop/perm"
)

var goldenGrants = map[authz.Permission][]string{
	perm.Manage:  {"ren"},
	perm.Publish: {"ren"},
	perm.Grant:   {"ren"},
}

var allRoles = []string{"user", "creator", "moderator", "admin", "ren"}

func TestGoldenBundles(t *testing.T) {
	for p, granted := range goldenGrants {
		grantedSet := make(map[string]bool, len(granted))
		for _, r := range granted {
			grantedSet[r] = true
		}
		for _, role := range allRoles {
			if got := perm.Resolver.Can([]string{role}, p); got != grantedSet[role] {
				t.Errorf("Can([%q], %q) = %v, want %v", role, p, got, grantedSet[role])
			}
		}
	}
}

func TestEveryShopKeyIsNonDelegable(t *testing.T) {
	for p := range goldenGrants {
		if !perm.NonDelegable.Has(p) {
			t.Errorf("%q must be non-delegable: the console must not hand coupon codes to a role below ren", p)
		}
	}
}
