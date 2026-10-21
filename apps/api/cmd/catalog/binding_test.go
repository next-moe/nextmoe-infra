package main

import (
	"testing"

	siteModel "api/internal/platform/site/model"
)

func TestIsThirdPartyEditClient(t *testing.T) {
	owner := uint(2)
	dev := uint(7311)

	cases := []struct {
		name string
		cl   *siteModel.OAuthClient
		want bool
	}{
		// The four production rows that were capped. Each is bound to a catalog
		// site AND owned by user 2, which is the shape the old predicate missed.
		{"forum", &siteModel.OAuthClient{CatalogSite: "kungal", OwnerUserID: &owner}, false},
		{"patch", &siteModel.OAuthClient{CatalogSite: "kungal", OwnerUserID: &owner}, false},
		{"sticker", &siteModel.OAuthClient{CatalogSite: "sticker", OwnerUserID: &owner}, false},
		{"letmoe", &siteModel.OAuthClient{CatalogSite: "letmoe", OwnerUserID: &owner}, false},

		// What the cap is actually for: a portal-registered app, which cannot
		// carry a catalog_site because devapi never writes one.
		{"developer app", &siteModel.OAuthClient{OwnerUserID: &dev}, true},

		{"unowned service client", &siteModel.OAuthClient{}, false},
		{"unowned site client", &siteModel.OAuthClient{CatalogSite: "news"}, false},
		{"nil", nil, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isThirdPartyEditClient(c.cl); got != c.want {
				t.Fatalf("isThirdPartyEditClient() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestSiteBindingOrigin(t *testing.T) {
	owner := uint(2)
	dev := uint(7311)
	cases := []struct {
		name string
		cl   *siteModel.OAuthClient
		site string
		want string
	}{
		{"forum", &siteModel.OAuthClient{CatalogSite: "kungal", OwnerUserID: &owner}, "kungal", "kungal"},
		{"forum app", &siteModel.OAuthClient{ID: "kungal-app", CatalogSite: "kungal"}, "kungal", "kungal"},
		{"moyu files under the forum's catalog site", &siteModel.OAuthClient{CatalogSite: "kungal", CommunitySite: "moyu", OwnerUserID: &owner}, "kungal", "moyu"},
		{"letmoe", &siteModel.OAuthClient{CatalogSite: "letmoe", OwnerUserID: &owner}, "letmoe", "letmoe"},
		{"editor", &siteModel.OAuthClient{CatalogSite: "editor"}, "editor", "editor"},
		{"news is no edit origin", &siteModel.OAuthClient{CatalogSite: "news"}, "news", ""},
		{"developer app", &siteModel.OAuthClient{OwnerUserID: &dev}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := siteBindingOf(c.cl)
			if b.Site != c.site || b.Origin != c.want {
				t.Fatalf("siteBindingOf() = %+v, want site %q origin %q", b, c.site, c.want)
			}
		})
	}
}
