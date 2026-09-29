package perm_test

import (
	"testing"

	"api/internal/platform/ai/perm"
	"api/internal/platform/authz"
)

var goldenGrants = map[authz.Permission][]string{
	perm.UsageView:    {"admin", "ren"},
	perm.BudgetManage: {"ren"},
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

func TestModeratorGrantsNothing(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"moderator"}, p) {
			t.Errorf("moderator must grant nothing on the AI usage surface, but grants %q", p)
		}
	}
}

func TestNonBundleRolesGrantNothing(t *testing.T) {
	for _, role := range []string{"user", "creator", "", "legacy_top_tier_alias"} {
		for p := range goldenGrants {
			if perm.Resolver.Can([]string{role}, p) {
				t.Errorf("non-bundle role %q must grant nothing, but grants %q", role, p)
			}
		}
	}
}

func TestManagementAxisContainment(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"admin"}, p) && !perm.Resolver.Can([]string{"ren"}, p) {
			t.Errorf("ren must grant everything admin grants; missing %q", p)
		}
	}
}

func TestBudgetManageIsNonDelegable(t *testing.T) {
	if !perm.NonDelegable.Has(perm.BudgetManage) {
		t.Errorf("%q must be non-delegable", perm.BudgetManage)
	}
	if perm.NonDelegable.Has(perm.UsageView) {
		t.Errorf("%q must stay delegable", perm.UsageView)
	}
}
