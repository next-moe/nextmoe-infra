package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"api/internal/platform/image/processor"

	"github.com/gofiber/fiber/v3"
)

func TestRespondUploadErrorDecodeFailedVersusStoreFailed(t *testing.T) {
	app := fiber.New()
	app.Get("/map", func(c fiber.Ctx) error {
		var err error
		if c.Query("kind") == "decode" {
			err = fmt.Errorf("decode: %w", fmt.Errorf("%w: %v", processor.ErrInvalidInput, errors.New("unsupported JPEG feature: luma/chroma subsampling ratio")))
		} else {
			err = errors.New("store exploded")
		}
		return respondUploadError(c, "catalog", "catalog_logo", "client", err)
	})

	hit := func(kind string) (int, int) {
		t.Helper()
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/map?kind="+kind, nil))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Code int `json:"code"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatalf("body %s: %v", raw, err)
		}
		return resp.StatusCode, body.Code
	}

	if status, code := hit("decode"); status != http.StatusBadRequest || code != 80010 {
		t.Fatalf("decode: status=%d code=%d, want 400/80010", status, code)
	}
	if status, code := hit("other"); status != http.StatusInternalServerError || code != 80012 {
		t.Fatalf("other: status=%d code=%d, want 500/80012", status, code)
	}
}
