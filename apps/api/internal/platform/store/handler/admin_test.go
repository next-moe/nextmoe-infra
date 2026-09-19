package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/platform/store/service"

	"github.com/gofiber/fiber/v3"
)

type rawEnvelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func mountAdmin(t *testing.T, ren bool) *fiber.App {
	t.Helper()
	app := fiber.New()
	h := NewAdminHandler(
		service.New(testDB, nil, service.Options{}),
		func(_ context.Context, ids []string) ([]service.AdminApp, error) {
			out := make([]service.AdminApp, len(ids))
			for i, id := range ids {
				out[i] = service.AdminApp{ClientID: id, Name: "站 " + id, SettlementEligible: id == "site-a"}
			}
			return out, nil
		},
		func(fiber.Ctx) bool { return ren },
	)
	gate := func(c fiber.Ctx) error {
		if !ren {
			return c.SendStatus(http.StatusForbidden)
		}
		return c.Next()
	}
	g := app.Group("/admin/devapi", func(c fiber.Ctx) error {
		c.Locals("user_id", uint(2))
		return c.Next()
	})
	h.Register(g, gate)
	return app
}

func call(t *testing.T, app *fiber.App, method, path string, body any) (int, rawEnvelope) {
	t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	raw, _ := io.ReadAll(resp.Body)
	var env rawEnvelope
	_ = json.Unmarshal(raw, &env)
	return resp.StatusCode, env
}

func TestAdminUsageCoversEverySite(t *testing.T) {
	today, _ := seedStats(t)
	app := mountAdmin(t, false)

	status, env := call(t, app, http.MethodGet, "/admin/devapi/store/usage?from="+today+"&to="+today, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	var usage struct {
		ByApp            []service.AdminUsageApp `json:"by_app"`
		Uniques          int64                   `json:"uniques"`
		CanManageCoupons bool                    `json:"can_manage_coupons"`
	}
	if err := json.Unmarshal(env.Data, &usage); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(usage.ByApp) != 2 || usage.Uniques != 3+2+99 {
		t.Fatalf("usage = %+v, want both sites and every unique of today", usage)
	}
	if usage.CanManageCoupons {
		t.Error("an admin without the policy permission was told it can manage coupons")
	}

	if status, _ := call(t, app, http.MethodGet, "/admin/devapi/store/usage?from=2026-09-30&to=2026-09-01", nil); status != http.StatusBadRequest {
		t.Errorf("a reversed range answered %d, want 400", status)
	}
}

func TestCouponRoutesSitBehindTheGate(t *testing.T) {
	seedStats(t)
	app := mountAdmin(t, false)
	for _, r := range []struct{ method, path string }{
		{http.MethodGet, "/admin/devapi/store/coupon-batches"},
		{http.MethodPost, "/admin/devapi/store/coupon-batches"},
		{http.MethodGet, "/admin/devapi/store/coupon-batches/1"},
		{http.MethodPost, "/admin/devapi/store/coupon-batches/1/publish"},
		{http.MethodDelete, "/admin/devapi/store/coupon-batches/1"},
	} {
		if status, _ := call(t, app, r.method, r.path, map[string]any{}); status != http.StatusForbidden {
			t.Errorf("%s %s answered %d without the policy permission, want 403", r.method, r.path, status)
		}
	}
}

func TestCouponBatchRoundTripOverHTTP(t *testing.T) {
	today, _ := seedStats(t)
	app := mountAdmin(t, true)

	status, env := call(t, app, http.MethodPost, "/admin/devapi/store/coupon-batches", map[string]any{
		"name": "测试批", "period_from": today, "period_to": today,
		"coupons": []map[string]any{{"face_value": 1000, "code": "C-1"}, {"face_value": 1000, "code": "C-2"}},
	})
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("create = %d %+v", status, env)
	}
	var batch struct {
		ID int64 `json:"id"`
	}
	_ = json.Unmarshal(env.Data, &batch)

	status, env = call(t, app, http.MethodPost, "/admin/devapi/store/coupon-batches", map[string]any{
		"name": "重复", "period_from": today, "period_to": today,
		"coupons": []map[string]any{{"face_value": 1000, "code": "C-1"}},
	})
	if status != http.StatusBadRequest || env.Message == "" {
		t.Errorf("a reused code answered %d %q, want 400 with a reason", status, env.Message)
	}

	path := "/admin/devapi/store/coupon-batches/" + jsonID(batch.ID)
	status, env = call(t, app, http.MethodGet, path, nil)
	if status != http.StatusOK {
		t.Fatalf("detail = %d", status)
	}
	var detail service.BatchDetail
	_ = json.Unmarshal(env.Data, &detail)
	if len(detail.Split) == 0 || detail.Split[0].ClientID != "site-a" || detail.Split[0].AllocatedPoints != 2000 {
		t.Fatalf("split = %+v, want the only eligible site proposed both coupons", detail.Split)
	}

	grants := map[string]any{"grants": []map[string]any{{"client_id": "site-b", "face_value": 1000, "count": 1}}}
	if status, _ := call(t, app, http.MethodPost, path+"/publish", grants); status != http.StatusBadRequest {
		t.Errorf("granting an ineligible site answered %d, want 400", status)
	}
	grants = map[string]any{"grants": []map[string]any{{"client_id": "site-a", "face_value": 1000, "count": 2}}}
	if status, env := call(t, app, http.MethodPost, path+"/publish", grants); status != http.StatusOK {
		t.Fatalf("publish = %d %+v", status, env)
	}
	if status, _ := call(t, app, http.MethodPost, path+"/publish", grants); status != http.StatusConflict {
		t.Errorf("publishing twice answered %d, want 409", status)
	}

	owner := mountDev(t, 1, []service.OwnerApp{{ClientID: "site-a", Name: "站 A"}})
	status, env = call(t, owner, http.MethodGet, "/dev/store/coupons", nil)
	var mine service.OwnerCoupons
	_ = json.Unmarshal(env.Data, &mine)
	if status != http.StatusOK || len(mine.Coupons) != 2 {
		t.Fatalf("owner coupons = %d %+v", status, mine)
	}
	stranger := mountDev(t, 1, []service.OwnerApp{{ClientID: "site-b", Name: "站 B"}})
	delivered := "/dev/store/coupons/" + jsonID(mine.Coupons[0].ID) + "/delivered"
	if status, _ := call(t, stranger, http.MethodPost, delivered, map[string]any{"delivered": true}); status != http.StatusNotFound {
		t.Errorf("another owner marking the coupon answered %d, want 404", status)
	}
	if status, _ := call(t, owner, http.MethodPost, delivered, map[string]any{"delivered": true}); status != http.StatusOK {
		t.Errorf("the owner marking the coupon answered %d, want 200", status)
	}
	anon := mountDev(t, 0, nil)
	if status, _ := call(t, anon, http.MethodGet, "/dev/store/coupons", nil); status != http.StatusUnauthorized {
		t.Errorf("signed-out coupons answered %d, want 401", status)
	}
}

func jsonID(id int64) string {
	raw, _ := json.Marshal(id)
	return string(raw)
}
