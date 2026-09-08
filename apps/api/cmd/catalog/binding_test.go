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
