package middleware

import (
	"slices"
	"testing"

	catalogperm "api/internal/platform/catalog/perm"
	galgameperm "api/internal/platform/galgame/perm"
	siteperm "api/internal/platform/site/perm"
)

func TestUnionRoles(t *testing.T) {
	cases := []struct {
		name         string
		global, site []string
		want         []string
	}{
		{"empty site returns global", []string{"creator"}, nil, []string{"creator"}},
		{"union adds site roles", []string{"creator"}, []string{"moderator"}, []string{"creator", "moderator"}},
		{"dedup overlapping", []string{"moderator"}, []string{"moderator"}, []string{"moderator"}},
		{"both empty", nil, nil, nil},
		{"only site roles", nil, []string{"event_organizer"}, []string{"event_organizer"}},
	}
	for _, tc := range cases {
		if got := UnionRoles(tc.global, tc.site); !slices.Equal(got, tc.want) {
			t.Errorf("%s: UnionRoles(%v, %v) = %v, want %v", tc.name, tc.global, tc.site, got, tc.want)
		}
	}
}

func TestSiteRolesCannotReachAdminBundles(t *testing.T) {
	roles := UnionRoles(nil, []string{"moderator", "event_organizer"})

	if catalogperm.Resolver.Can(roles, catalogperm.Review) {
		t.Error("site roles must not grant catalog.review (ren-only)")
	}
	if siteperm.Resolver.Can(roles, siteperm.AdminAccess) {
		t.Error("site roles must not grant oauth.admin_access (admin/ren)")
	}
	if siteperm.Resolver.Can(roles, siteperm.RolesGrantSite) {
		t.Error("site roles must not grant oauth.roles.grant_site (admin/ren)")
	}
	if !galgameperm.Resolver.Can(roles, galgameperm.Review) {
		t.Error("a site moderator should grant galgame.review")
	}
}
