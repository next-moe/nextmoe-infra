package permissions_test

import (
	"strings"
	"testing"

	aiPerm "api/internal/platform/ai/perm"
	"api/internal/platform/authz"
	devapiPerm "api/internal/platform/devapi/perm"
	"api/internal/platform/permissions"
	shopPerm "api/internal/platform/shop/perm"
	sitePerm "api/internal/platform/site/perm"
)

func TestRegistryDescribesExactlyTheBundledKeys(t *testing.T) {
	for _, d := range permissions.Live().Domains() {
		described := make(map[authz.Permission]bool, len(d.Keys))
		for _, k := range d.Keys {
			if described[k.Permission] {
				t.Errorf("%s: duplicate registry entry for %q", d.Name, k.Permission)
			}
			described[k.Permission] = true
			if k.DescEN == "" || k.DescZH == "" {
				t.Errorf("%s: %q is missing a description", d.Name, k.Permission)
			}
		}

		bundled := make(map[authz.Permission]bool)
		for _, perms := range d.Bundles {
			for _, p := range perms {
				bundled[p] = true
			}
		}

		for p := range bundled {
			if !described[p] {
				t.Errorf("%s: %q is in a code bundle but not described in the registry", d.Name, p)
			}
		}
		for p := range described {
			if !bundled[p] {
				t.Errorf("%s: %q is described in the registry but in no code bundle", d.Name, p)
			}
		}
	}
}

func TestRegistryExcludesRetiredGalgame(t *testing.T) {
	reg := permissions.Live()
	for _, p := range []authz.Permission{
		"galgame.publish_direct", "galgame.review", "galgame.admin_access",
	} {
		if _, ok := reg.Lookup(p); ok {
			t.Errorf("retired galgame key %q must not be in the console registry", p)
		}
	}
	for _, d := range reg.Domains() {
		for _, k := range d.Keys {
			if strings.HasPrefix(string(k.Permission), "galgame.") {
				t.Errorf("retired galgame key %q leaked into domain %s", k.Permission, d.Name)
			}
		}
	}
}

func TestNonDelegableKeysAreRegistered(t *testing.T) {
	reg := permissions.Live()
	for _, p := range []authz.Permission{
		sitePerm.RolesGrantAdmin, sitePerm.PermissionsManage, sitePerm.SitesManageAll,
		sitePerm.MoemoepointAdjust, sitePerm.UsersAnonymize,
		devapiPerm.PolicyManage,
		shopPerm.Manage, shopPerm.Publish, shopPerm.Grant,
		aiPerm.BudgetManage,
	} {
		if !reg.IsNonDelegable(p) {
			t.Errorf("%q must be non-delegable", p)
		}
	}
	if reg.IsNonDelegable(sitePerm.UsersPIIView) {
		t.Errorf("%q must be delegable", sitePerm.UsersPIIView)
	}
	if reg.IsNonDelegable(devapiPerm.Manage) {
		t.Errorf("%q must be delegable", devapiPerm.Manage)
	}
}

func TestUnknownKeyIsNonDelegable(t *testing.T) {
	if !permissions.Live().IsNonDelegable(authz.Permission("nope.does.not.exist")) {
		t.Error("an unknown key must be treated as non-delegable")
	}
}

func TestEditableRolesExcludeUserAndRen(t *testing.T) {
	for _, r := range permissions.EditableRoles {
		if r == permissions.RoleUser || r == permissions.RoleRen {
			t.Errorf("%q must not be editable", r)
		}
	}
	if len(permissions.EditableRoles) != 3 {
		t.Errorf("expected creator/moderator/admin, got %v", permissions.EditableRoles)
	}
}

func TestHighestRank(t *testing.T) {
	cases := []struct {
		roles []string
		want  int
	}{
		{nil, 0},
		{[]string{"user"}, 0},
		{[]string{"creator"}, 0},
		{[]string{"moderator"}, 1},
		{[]string{"admin"}, 2},
		{[]string{"ren"}, 3},
		{[]string{"creator", "admin"}, 2},
		{[]string{"admin", "ren"}, 3},
	}
	for _, c := range cases {
		if got := permissions.HighestRank(c.roles); got != c.want {
			t.Errorf("HighestRank(%v) = %d, want %d", c.roles, got, c.want)
		}
	}
}
