package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
)

func TestUserSearch_RefusesTextPostgresCannotStore(t *testing.T) {
	app := fiber.New()
	app.Get("/users/search", NewUserBatchHandler(nil).Search)

	for _, q := range []string{"%E6%94%BE%00", "ab%FF"} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/users/search?q="+q, nil))
		if err != nil {
			t.Fatalf("q=%s: %v", q, err)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("q=%s: status %d, want 400", q, resp.StatusCode)
		}
	}
}
