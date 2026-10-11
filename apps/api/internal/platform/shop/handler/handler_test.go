package handler

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"api/internal/middleware"
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

func newApp(t *testing.T) (*fiber.App, uint, *gorm.DB) {
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
		if v := c.Get("X-Test-OAuth-Client"); v != "" {
			client := &siteModel.OAuthClient{ID: v}
			if rest, ok := strings.CutPrefix(v, "awarder:"); ok {
				client.MoemoepointAwarder = true
				rest, client.CommunitySite, _ = strings.Cut(rest, "@")
				if n, err := strconv.ParseUint(rest, 10, 64); err == nil {
					site := uint(n)
					client.SiteID = &site
				}
			}
			c.Locals(middleware.LocalOAuthClient, client)
		}
		return c.Next()
	})
	app.Get("/shop/me", h.Inventory)
	app.Post("/shop/orders", h.Purchase)
	app.Post("/admin/shop/items/:id/:action", h.TransitionItem)
	app.Post("/admin/shop/assets", h.UploadAsset)
	app.Get("/shop/storefront", h.Storefront)
	app.Get("/users/:id/shop", h.CustomerInventory)
	app.Post("/users/:id/shop/orders", h.CustomerPurchase)
	app.Put("/users/:id/shop/loadout", h.CustomerEquip)
	app.Put("/users/:id/shop/restock-alerts/:offer_id", h.CustomerRestockAlert)
	app.Delete("/users/:id/shop/restock-alerts/:offer_id", h.CustomerRestockAlert)
	return app, u.ID, db
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
	app, _, _ := newApp(t)
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
	app, _, _ := newApp(t)
	if status, _ := call(t, app, "POST", "/admin/shop/items/1/publish", "", "moderator", nil); status != 403 {
		t.Fatalf("a moderator publishing: HTTP %d, want 403", status)
	}
	if status, _ := call(t, app, "POST", "/admin/shop/items/1/publish", "", "admin", nil); status != 403 {
		t.Fatalf("an admin publishing: HTTP %d, want 403 — the shop is ren's alone", status)
	}
	if status, code := call(t, app, "POST", "/admin/shop/items/999999999/publish", "", "ren", nil); status != 404 || code != errors.ErrShopItemNotFound {
		t.Fatalf("ren publishing a missing item: HTTP %d code %d, want 404", status, code)
	}
	if status, code := call(t, app, "POST", "/admin/shop/items/1/explode", "", "ren", nil); status != 400 || code != errors.ErrShopInvalidTransition {
		t.Fatalf("an unknown action: HTTP %d code %d", status, code)
	}
}

func TestOnlyASiteThatMaySpendRunsAStorefront(t *testing.T) {
	app, user, db := newApp(t)
	site := siteModel.Site{Name: "店面", Domain: "shop-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".test.local"}
	if err := db.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	t.Cleanup(func() { db.Exec(`DELETE FROM sites WHERE id = ?`, site.ID) })
	sid := strconv.FormatUint(uint64(site.ID), 10)
	inv := "/users/" + strconv.FormatUint(uint64(user), 10) + "/shop"

	for _, client := range []string{"reader", "awarder:"} {
		for _, r := range [][2]string{{"GET", "/shop/storefront"}, {"GET", inv}, {"POST", inv + "/orders"}, {"PUT", inv + "/loadout"},
			{"PUT", inv + "/restock-alerts/1"}, {"DELETE", inv + "/restock-alerts/1"}} {
			status, code := callAs(t, app, r[0], r[1], client, map[string]any{"offer_id": 1, "idempotency_key": "k", "slot": "avatar_frame"})
			if status != 403 || code != errors.ErrShopNotStorefront {
				t.Fatalf("%s %s as %q: HTTP %d code %d, want 403/%d", r[0], r[1], client, status, code, errors.ErrShopNotStorefront)
			}
		}
	}
	shop := "awarder:" + sid
	if status, code := callAs(t, app, "GET", "/shop/storefront", shop, nil); status != 200 || code != 0 {
		t.Fatalf("storefront: HTTP %d code %d", status, code)
	}
	if status, code := callAs(t, app, "GET", inv, shop, nil); status != 200 || code != 0 {
		t.Fatalf("customer inventory: HTTP %d code %d", status, code)
	}
	if status, code := callAs(t, app, "PUT", inv+"/loadout", shop, map[string]any{"slot": "avatar_frame", "site_id": site.ID + 1}); status != 403 || code != errors.ErrForbidden {
		t.Fatalf("wearing for another site: HTTP %d code %d, want 403", status, code)
	}
	if status, code := callAs(t, app, "PUT", inv+"/loadout", shop, map[string]any{"slot": "avatar_frame", "site_id": site.ID}); status != 200 || code != 0 {
		t.Fatalf("taking off at this site: HTTP %d code %d", status, code)
	}
}

func callAs(t *testing.T, app *fiber.App, method, path, client string, body any) (int, int) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Test-OAuth-Client", client)
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

func TestASiteWithAnInboxSubscribesToRestocks(t *testing.T) {
	app, user, db := newApp(t)
	site := siteModel.Site{Name: "店面", Domain: "alert-" + strconv.FormatInt(time.Now().UnixNano(), 36) + ".test.local"}
	if err := db.Create(&site).Error; err != nil {
		t.Fatalf("create site: %v", err)
	}
	item := model.Item{Kind: model.KindRedeemCode, Status: model.ItemPublished, Name: "券", Render: []byte("{}"), CreatedBy: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	offer := model.Offer{
		Status: model.OfferActive, Costs: []byte(`[{"asset":"moemoepoint","amount":100}]`),
		Rewards: []byte(`[{"item_id":` + strconv.FormatInt(item.ID, 10) + `}]`), CreatedBy: 1,
	}
	if err := db.Create(&offer).Error; err != nil {
		t.Fatalf("create offer: %v", err)
	}
	t.Cleanup(func() {
		db.Exec(`DELETE FROM shop_restock_alerts WHERE offer_id = ?`, offer.ID)
		db.Exec(`DELETE FROM shop_offers WHERE id = ?`, offer.ID)
		db.Exec(`DELETE FROM shop_items WHERE id = ?`, item.ID)
		db.Exec(`DELETE FROM sites WHERE id = ?`, site.ID)
	})
	sid := strconv.FormatUint(uint64(site.ID), 10)
	inv := "/users/" + strconv.FormatUint(uint64(user), 10) + "/shop"
	alert := inv + "/restock-alerts/" + strconv.FormatInt(offer.ID, 10)

	if status, code := callAs(t, app, "PUT", alert, "awarder:"+sid, nil); status != 403 || code != errors.ErrShopNotStorefront {
		t.Fatalf("subscribing from a site with no inbox: HTTP %d code %d", status, code)
	}
	shop := "awarder:" + sid + "@kungal"
	if status, code := callAs(t, app, "PUT", inv+"/restock-alerts/0", shop, nil); status != 400 || code != errors.ErrInvalidID {
		t.Fatalf("a bad offer id: HTTP %d code %d", status, code)
	}
	for range 2 {
		if status, code := callAs(t, app, "PUT", alert, shop, nil); status != 200 || code != 0 {
			t.Fatalf("subscribe: HTTP %d code %d", status, code)
		}
	}
	var got model.RestockAlert
	if err := db.Where("user_id = ? AND offer_id = ?", user, offer.ID).First(&got).Error; err != nil ||
		got.Inbox != "kungal" || got.SiteID != site.ID {
		t.Fatalf("stored alert %+v: %v", got, err)
	}
	if status, code := callAs(t, app, "DELETE", alert, shop, nil); status != 200 || code != 0 {
		t.Fatalf("unsubscribe: HTTP %d code %d", status, code)
	}
	var left int64
	db.Model(&model.RestockAlert{}).Where("user_id = ?", user).Count(&left)
	if left != 0 {
		t.Fatalf("%d alerts left after unsubscribing", left)
	}
}
