package main

import (
	"encoding/json"
	"testing"

	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEditorClientSeedIsCreateOnly(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := db.AutoMigrate(&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Exec(`DELETE FROM oauth_clients WHERE id = 'nextmoe-editor'`)
	if err := db.Exec(`INSERT INTO sites (name, domain, description) VALUES (?, ?, '') ON CONFLICT (domain) DO NOTHING`,
		editorSiteName, editorSiteDomain).Error; err != nil {
		t.Fatalf("seed site: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM oauth_clients WHERE id = 'nextmoe-editor'`) })

	if err := seedEditorClient(db, ""); err != nil {
		t.Fatalf("seed without url: %v", err)
	}
	var n int64
	db.Model(&siteModel.OAuthClient{}).Where("id = 'nextmoe-editor'").Count(&n)
	if n != 0 {
		t.Fatal("seeded the editor client with KUN_EDITOR_PUBLIC_URL unset")
	}

	if err := seedEditorClient(db, "https://editor.example.test/"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var cl siteModel.OAuthClient
	if err := db.Preload("Site").First(&cl, "id = ?", "nextmoe-editor").Error; err != nil {
		t.Fatalf("load client: %v", err)
	}
	if !cl.IsPublic || !cl.AutoConsent || !cl.UnionSiteRoles || cl.CatalogSite != "editor" || cl.OwnerUserID != nil {
		t.Errorf("client flags = public %v auto_consent %v union %v catalog_site %q owner %v",
			cl.IsPublic, cl.AutoConsent, cl.UnionSiteRoles, cl.CatalogSite, cl.OwnerUserID)
	}
	if cl.Site == nil || cl.Site.Domain != editorSiteDomain {
		t.Errorf("client site = %+v, want %s", cl.Site, editorSiteDomain)
	}
	if !cl.HasRedirectURI("https://editor.example.test/auth/callback") {
		t.Errorf("redirect uris = %s", cl.RedirectURIs)
	}
	if _, ok := cl.CheckScope("openid profile catalog:read catalog:edit"); !ok {
		t.Errorf("allowed scopes = %s", cl.AllowedScopes)
	}
	if !cl.IsGrantAllowed("refresh_token") {
		t.Errorf("grants = %s", cl.Grants)
	}

	db.Model(&siteModel.OAuthClient{}).Where("id = 'nextmoe-editor'").Update("auto_consent", false)
	if err := seedEditorClient(db, "https://elsewhere.example.test"); err != nil {
		t.Fatalf("reseed: %v", err)
	}
	var again siteModel.OAuthClient
	db.First(&again, "id = ?", "nextmoe-editor")
	var uris []string
	_ = json.Unmarshal(again.RedirectURIs, &uris)
	if again.AutoConsent || len(uris) != 1 || uris[0] != "https://editor.example.test/auth/callback" {
		t.Errorf("a later deploy overwrote the client: auto_consent %v redirects %v", again.AutoConsent, uris)
	}
}
