package devapi

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestOnlyRenMovesAnAppsSettlement(t *testing.T) {
	cleanupSelf(t)
	svc, admin, repo, _ := newSelfService(t)
	ctx := context.Background()
	const owner, operator = uint(1), uint(9)

	app, err := svc.CreateApp(ctx, owner, "settlement gate", "", nil)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	patch := func(role, body string) int {
		t.Helper()
		fiberApp := fiber.New()
		auth := func(c fiber.Ctx) error {
			c.Locals("user_id", operator)
			c.Locals("user_roles", []string{role})
			return c.Next()
		}
		noop := func(c fiber.Ctx) error { return c.Next() }
		NewAdminHandler(admin).Register(fiberApp.Group("/admin/devapi", auth), noop)
		req := httptest.NewRequest("PATCH", "/admin/devapi/apps/"+app.ID, bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := fiberApp.Test(req)
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	if got := patch("admin", `{"dev_tier":"trusted","owner_user_id":1,"store_settlement_eligible":true}`); got != fiber.StatusOK {
		t.Fatalf("admin saving the config modal with owner and roster unchanged = %d, want 200", got)
	}
	if got := patch("admin", `{"owner_user_id":9}`); got != fiber.StatusForbidden {
		t.Errorf("admin re-homing an app = %d, want 403", got)
	}
	if got := patch("admin", `{"store_settlement_eligible":false}`); got != fiber.StatusForbidden {
		t.Errorf("admin taking an app off the roster = %d, want 403", got)
	}
	stored, err := repo.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.OwnerUserID == nil || *stored.OwnerUserID != owner || !stored.StoreSettlementEligible || stored.DevTier != "trusted" {
		t.Fatalf("after admin patches: owner=%v eligible=%v tier=%q, want owner 1, eligible, trusted",
			stored.OwnerUserID, stored.StoreSettlementEligible, stored.DevTier)
	}

	if got := patch("ren", `{"owner_user_id":9,"store_settlement_eligible":false}`); got != fiber.StatusOK {
		t.Fatalf("ren moving owner and roster = %d, want 200", got)
	}
	stored, err = repo.GetApp(ctx, app.ID)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if stored.OwnerUserID == nil || *stored.OwnerUserID != operator || stored.StoreSettlementEligible {
		t.Errorf("after ren's patch: owner=%v eligible=%v, want owner 9 and off the roster",
			stored.OwnerUserID, stored.StoreSettlementEligible)
	}
}
