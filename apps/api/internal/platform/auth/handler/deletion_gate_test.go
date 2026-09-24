package handler

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// A 403 written by a helper that returned nil let the handler run on and
// schedule the deletion anyway (review of #305); the service here is nil, so
// reaching it panics instead of passing quietly.
func TestAnotherAppsTokenCannotTouchDeletion(t *testing.T) {
	h := &AuthHandler{}
	app := fiber.New()
	app.Use(func(c fiber.Ctx) error {
		c.Locals("user_uuid", "u-1")
		c.Locals("token_client_id", "some-third-party-app")
		return c.Next()
	})
	app.Post("/auth/me/deletion/send-code", h.SendDeletionCode)
	app.Post("/auth/me/deletion", h.RequestDeletion)
	app.Delete("/auth/me/deletion", h.CancelDeletion)

	for _, rt := range []struct{ method, path, body string }{
		{"POST", "/auth/me/deletion/send-code", `{}`},
		{"POST", "/auth/me/deletion", `{"code":"123456"}`},
		{"DELETE", "/auth/me/deletion", ``},
	} {
		req := httptest.NewRequest(rt.method, rt.path, strings.NewReader(rt.body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("%s %s: %v", rt.method, rt.path, err)
		}
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 403 || strings.Contains(string(body), `"code":0`) {
			t.Fatalf("%s %s: want a bare 403, got %d %s", rt.method, rt.path, resp.StatusCode, body)
		}
	}
}
