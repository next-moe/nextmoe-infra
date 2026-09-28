package handler

import (
	"encoding/base64"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	suitelock "api/internal/platform/ai/dbtest"
	aiMigrate "api/internal/platform/ai/migrate"
	aiModel "api/internal/platform/ai/model"
	"api/internal/platform/ai/service"
	"api/internal/platform/ai/upstream"
	siteModel "api/internal/platform/site/model"
	siteRepo "api/internal/platform/site/repository"
	"api/internal/testsupport/dbtest"

	"github.com/gofiber/fiber/v3"
	"gorm.io/datatypes"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("ai/handler")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("ai/handler", "cannot connect to test database: %v", err)
	}
	sqlDB, _ := db.DB()
	release := suitelock.AcquireSuiteLock(sqlDB)

	if err := aiMigrate.Run(db); err != nil {
		release()
		dbtest.SkipMainf("ai/handler", "ai migration failed: %v", err)
	}
	if err := db.AutoMigrate(&siteModel.Site{}, &siteModel.OAuthClient{}); err != nil {
		release()
		dbtest.SkipMainf("ai/handler", "oauth_clients migration failed: %v", err)
	}

	testDB = db
	code := m.Run()
	release()
	os.Exit(code)
}

func TestSpecExport(t *testing.T) {
	api := Setup(fiber.New(), nil)
	b, err := api.OpenAPI().YAML()
	if err != nil {
		t.Fatalf("marshal spec: %v", err)
	}
	spec := string(b)
	for _, want := range []string{
		"/api/v1/ai/moderate-text",
		"operationId: moderateText",
		"degraded",
		"flagged",
	} {
		if !strings.Contains(spec, want) {
			t.Errorf("spec missing %q", want)
		}
	}
}

func seedClient(t *testing.T, id, secret, catalogSite string) {
	t.Helper()
	testDB.Where("id = ?", id).Delete(&siteModel.OAuthClient{})
	c := &siteModel.OAuthClient{
		ID:           id,
		Name:         "ai s2s test " + id,
		Secret:       siteModel.HashOAuthClientSecret(secret),
		RedirectURIs: datatypes.JSON([]byte("[]")),
		Grants:       datatypes.JSON([]byte("[]")),
		CatalogSite:  catalogSite,
	}
	if err := testDB.Create(c).Error; err != nil {
		t.Fatalf("seed client %s: %v", id, err)
	}
}

func basicAuth(id, secret string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(id+":"+secret))
}

func buildApp() *fiber.App {
	app := fiber.New()
	repo := siteRepo.NewOAuthClientRepository(testDB)
	app.Use("/api/v1/ai", S2SAuth(repo))
	svc := service.NewModerationService(testDB, upstream.NewOmniClient("", "", ""), upstream.NewClient("", "", ""), service.ModerationOptions{})
	Setup(app, svc)
	return app
}

func TestS2SAuthHTTP(t *testing.T) {
	if err := testDB.Exec("TRUNCATE ai_usage RESTART IDENTITY").Error; err != nil {
		t.Fatalf("truncate ai_usage: %v", err)
	}
	seedClient(t, "ai-test-bound", "s3cr3t", "letmoe")
	seedClient(t, "ai-test-unbound", "s3cr3t", "")
	app := buildApp()

	const body = `{"text":"hello world"}`

	post := func(auth string) int {
		req := httptest.NewRequest("POST", "/api/v1/ai/moderate-text", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		status, _ := send(t, app, publishedSpec(t), req)
		return status
	}

	if code := post(basicAuth("ai-test-bound", "s3cr3t")); code != 200 {
		t.Fatalf("correct creds → %d, want 200", code)
	}
	if code := post(basicAuth("ai-test-bound", "wrong")); code != 401 {
		t.Fatalf("wrong secret → %d, want 401", code)
	}
	if code := post(basicAuth("ai-test-nope", "s3cr3t")); code != 401 {
		t.Fatalf("unknown client → %d, want 401", code)
	}
	if code := post(""); code != 401 {
		t.Fatalf("no auth → %d, want 401", code)
	}
	if code := post(basicAuth("ai-test-unbound", "s3cr3t")); code != 403 {
		t.Fatalf("unbound catalog_site → %d, want 403", code)
	}

	var rows []aiModel.AIUsage
	if err := testDB.Order("id").Find(&rows).Error; err != nil {
		t.Fatalf("load usage: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 metered row (only the authed call), got %d", len(rows))
	}
	if rows[0].Site != "letmoe" {
		t.Fatalf("metered site = %q, want letmoe (derived from binding, not body)", rows[0].Site)
	}
	if rows[0].Status != aiModel.StatusDegraded {
		t.Fatalf("metered status = %d, want degraded(4) (unconfigured upstream)", rows[0].Status)
	}
}
