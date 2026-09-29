package perm_test

import (
	"testing"

	"api/internal/platform/authz"
	"api/internal/platform/site/perm"
)

var goldenGrants = map[authz.Permission][]string{
	perm.AdminAccess:             {"admin", "ren"},
	perm.RolesGrantBasic:         {"admin", "ren"},
	perm.RolesGrantSite:          {"admin", "ren"},
	perm.UsersPIIView:            {"ren"},
	perm.RolesGrantAdmin:         {"ren"},
	perm.ClientsStorageConfig:    {"ren"},
	perm.ClientsPrivilegedConfig: {"ren"},
	perm.SitesManageAll:          {"ren"},
	perm.PermissionsManage:       {"ren"},
	perm.MoemoepointAdjust:       {"ren"},
	perm.UsersAnonymize:          {"ren"},
	perm.SitesCreate:             {"admin", "ren"},
	perm.SitesUpdate:             {"admin", "ren"},
	perm.SitesDelete:             {"admin", "ren"},
	perm.ClientsCreate:           {"admin", "ren"},
	perm.ClientsUpdate:           {"admin", "ren"},
	perm.ClientsDelete:           {"admin", "ren"},
}

func TestNonDelegableAreDeclaredKeys(t *testing.T) {
	for p := range perm.NonDelegable {
		if _, ok := goldenGrants[p]; !ok {
			t.Errorf("non-delegable %q is not a declared console permission", p)
		}
	}
	for _, want := range []authz.Permission{
		perm.RolesGrantAdmin, perm.PermissionsManage, perm.SitesManageAll,
		perm.MoemoepointAdjust, perm.UsersAnonymize,
	} {
		if !perm.NonDelegable.Has(want) {
			t.Errorf("%q must be non-delegable", want)
		}
	}
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
	for _, role := range []string{"user", "moderator", "", "legacy_top_tier_alias"} {
		for p := range goldenGrants {
			if perm.Resolver.Can([]string{role}, p) {
				t.Errorf("non-bundle role %q must grant nothing, but grants %q", role, p)
			}
		}
	}
}

func TestManagementAxisContainment(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"moderator"}, p) && !perm.Resolver.Can([]string{"admin"}, p) {
			t.Errorf("admin must grant everything moderator grants; missing %q", p)
		}
		if perm.Resolver.Can([]string{"admin"}, p) && !perm.Resolver.Can([]string{"ren"}, p) {
			t.Errorf("ren must grant everything admin grants; missing %q", p)
		}
	}
}

func TestCreatorGrantsNothing(t *testing.T) {
	for p := range goldenGrants {
		if perm.Resolver.Can([]string{"creator"}, p) {
			t.Errorf("creator must grant nothing on the console, but grants %q", p)
		}
	}
}
