package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/ledger/ledgertest"
	ledgerService "api/internal/platform/ledger/service"
	"api/internal/platform/shop/model"
	"api/internal/platform/shop/service"
	siteModel "api/internal/platform/site/model"
	"api/internal/testsupport/dbtest"
	"api/pkg/errors"

	"github.com/gofiber/fiber/v3"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newApp(t *testing.T) (*fiber.App, uint) {
	t.Helper()
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.Skip(t)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.Skipf(t, "open database: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		dbtest.Skipf(t, "migrate: %v", err)
	}
	if err := db.AutoMigrate(append([]any{&siteModel.Site{}}, model.AllModels()...)...); err != nil {
		dbtest.Skipf(t, "migrate shop: %v", err)
	}
	tag := strconv.FormatInt(time.Now().UnixNano(), 36)
	u := &authModel.User{Name: "hs" + tag[len(tag)-10:], Email: "hs-" + tag + "@test.local"}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		_ = ledgertest.Purge(db, u.ID)
		db.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})

	h := New(service.New(db, ledgerService.New(db), nil, "https://cdn.test"), repository.NewUserRepository(db))
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_id", u.ID)
		c.Locals("token_client_id", c.Get("X-Test-Client"))
		c.Locals("user_roles", []string{c.Get("X-Test-Role")})
		return c.Next()
	})
	app.Get("/shop/me", h.Inventory)
	app.Post("/shop/orders", h.Purchase)
	app.Post("/admin/shop/items/:id/:action", h.TransitionItem)
	app.Post("/admin/shop/assets", h.UploadAsset)
	return app, u.ID
}

func call(t *testing.T, app *fiber.App, method, path, client, role string, body any) (int, int) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-Client", client)
	req.Header.Set("X-Test-Role", role)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	var out struct {
		Code int `json:"code"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out.Code
}

func TestTheShopSpendsOnlyForTheAccountCenter(t *testing.T) {
	app, _ := newApp(t)
	if status, code := call(t, app, "GET", "/shop/me", "", "", nil); status != 200 || code != 0 {
		t.Fatalf("first-party inventory: HTTP %d code %d", status, code)
	}
	for _, r := range [][2]string{{"GET", "/shop/me"}, {"POST", "/shop/orders"}} {
		status, code := call(t, app, r[0], r[1], "some-app", "", map[string]any{"offer_id": 1, "idempotency_key": "k"})
		if status != 403 || code != errors.ErrShopFirstPartyOnly {
			t.Fatalf("%s %s with an OAuth token: HTTP %d code %d, want 403/%d", r[0], r[1], status, code, errors.ErrShopFirstPartyOnly)
		}
	}
}

func TestPublishingNeedsThePublishPermission(t *testing.T) {
	app, _ := newApp(t)
	if status, _ := call(t, app, "POST", "/admin/shop/items/1/publish", "", "moderator", nil); status != 403 {
		t.Fatalf("a moderator publishing: HTTP %d, want 403", status)
	}
	if status, code := call(t, app, "POST", "/admin/shop/items/999999999/publish", "", "admin", nil); status != 404 || code != errors.ErrShopItemNotFound {
		t.Fatalf("an admin publishing a missing item: HTTP %d code %d, want 404", status, code)
	}
	if status, code := call(t, app, "POST", "/admin/shop/items/1/explode", "", "admin", nil); status != 400 || code != errors.ErrShopInvalidTransition {
		t.Fatalf("an unknown action: HTTP %d code %d", status, code)
	}
}
