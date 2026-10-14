package handler

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	artMW "api/internal/platform/artifact/middleware"
	"api/internal/platform/artifact/model"
	"api/internal/platform/artifact/repository"
	"api/internal/platform/artifact/service"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestCompositeListIsNotTakenForAnArtifactUUID(t *testing.T) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "cannot connect to test database: %v", err)
	}
	if err := db.AutoMigrate(model.All()...); err != nil {
		t.Fatalf("artifact migration: %v", err)
	}

	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals(artMW.LocalSiteKey, "t-route-empty")
		c.Locals(artMW.LocalOAuthClient, &siteModel.OAuthClient{ID: "c"})
		c.Locals(artMW.LocalAuthMethod, artMW.MethodBasic)
		return c.Next()
	})
	composites := service.NewCompositeService(repository.NewCompositeRepository(db), nil, nil)
	Setup(app, service.New(nil, nil, nil), composites)

	resp, err := app.Test(httptest.NewRequest("GET", "/api/v1/artifacts/composites", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	var env struct {
		Code int `json:"code"`
		Data struct {
			Items []any `json:"items"`
			Total int64 `json:"total"`
		} `json:"data"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(body, &env) != nil || env.Code != 0 || env.Data.Items == nil {
		t.Fatalf("GET /api/v1/artifacts/composites = %d %s; want the composite list", resp.StatusCode, body)
	}
}
