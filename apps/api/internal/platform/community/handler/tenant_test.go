package handler

import (
	"context"
	"testing"

	siteModel "api/internal/platform/site/model"
)

// The forum and moyu file catalog claims as the same site and must not share a
// community tenant. community_site is what separates them; the fallback is what
// keeps every client that predates it where its rows already are.
func TestSiteBindingPrefersCommunitySite(t *testing.T) {
	cases := []struct {
		name   string
		client *siteModel.OAuthClient
		want   string
		fail   bool
	}{
		{
			name:   "community_site wins",
			client: &siteModel.OAuthClient{CatalogSite: "kungal", CommunitySite: "moyu"},
			want:   "moyu",
		},
		{
			name:   "catalog_site is the fallback",
			client: &siteModel.OAuthClient{CatalogSite: "kungal"},
			want:   "kungal",
		},
		{
			name:   "community_site alone is enough",
			client: &siteModel.OAuthClient{CommunitySite: "moyu"},
			want:   "moyu",
		},
		{
			name:   "neither is a refusal",
			client: &siteModel.OAuthClient{},
			fail:   true,
		},
		{
			name: "no client at all is a refusal",
			fail: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.client != nil {
				ctx = context.WithValue(ctx, ctxKeyClient, tc.client)
			}
			got, he := siteBinding(ctx)
			if tc.fail {
				if he == nil {
					t.Fatalf("want a refusal, got site %q", got)
				}
				return
			}
			if he != nil {
				t.Fatalf("want site %q, got error %v", tc.want, he)
			}
			if got != tc.want {
				t.Fatalf("want site %q, got %q", tc.want, got)
			}
		})
	}
}
