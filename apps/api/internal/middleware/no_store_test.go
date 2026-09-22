package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

// TestNoStoreRunsBeforeTheRouteHandler pins the argument order the whole
// cmd/oauth route table relies on: fiber v3's Get(path, handler, middleware...)
// runs its FIRST handler first, so the guard goes in front of the route's own
// handler, not after it. Registered the other way round the header would be set
// after the response was already written.
func TestNoStoreRunsBeforeTheRouteHandler(t *testing.T) {
	app := fiber.New()
	app.Get("/x", NoStore(), func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"ok": true})
	})

	resp, err := app.Test(httptest.NewRequest("GET", "/x", nil))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if got := resp.Header.Get(fiber.HeaderCacheControl); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}
