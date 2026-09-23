package problem

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humafiber"
	"github.com/gofiber/fiber/v3"
)

func TestAnOversizedBodyIsPayloadTooLarge(t *testing.T) {
	prev, prevCtx := huma.NewError, huma.NewErrorWithContext
	t.Cleanup(func() {
		huma.NewError, huma.NewErrorWithContext = prev, prevCtx
	})
	huma.NewError = func(status int, msg string, errs ...error) huma.StatusError {
		return FromHuma(nil, status, msg, errs...)
	}
	huma.NewErrorWithContext = func(ctx huma.Context, status int, msg string, errs ...error) huma.StatusError {
		return FromHuma(ctx, status, msg, errs...)
	}

	type sinkInput struct {
		Body struct {
			Text string `json:"text"`
		}
	}
	register := func(app *fiber.App, maxBody int64) {
		cfg := huma.DefaultConfig("t", "0")
		cfg.OpenAPIPath, cfg.DocsPath, cfg.SchemasPath = "", "", ""
		huma.Register(humafiber.New(app, cfg), huma.Operation{
			OperationID: "sink", Method: http.MethodPost, Path: "/v2/sink", MaxBodyBytes: maxBody,
		}, func(context.Context, *sinkInput) (*struct{}, error) {
			return nil, nil
		})
	}
	oversized, err := json.Marshal(map[string]string{"text": strings.Repeat("x", 4096)})
	if err != nil {
		t.Fatal(err)
	}

	// app.Test enforces fiber's BodyLimit on the client side and never reaches
	// the server, so that limit is exercised over a real listener.
	serve := func(t *testing.T, app *fiber.App, req *http.Request) *http.Response {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = app.Listener(ln, fiber.ListenConfig{DisableStartupMessage: true}) }()
		t.Cleanup(func() { _ = app.Shutdown() })
		req.URL.Scheme, req.URL.Host, req.RequestURI = "http", ln.Addr().String(), ""
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	cases := []struct {
		name string
		send func(t *testing.T, req *http.Request) *http.Response
	}{
		{"huma operation limit", func(t *testing.T, req *http.Request) *http.Response {
			app := fiber.New(fiber.Config{ErrorHandler: WriteFiberError})
			register(app, 256)
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			return resp
		}},
		{"fiber server limit", func(t *testing.T, req *http.Request) *http.Response {
			app := fiber.New(fiber.Config{ErrorHandler: WriteFiberError, BodyLimit: 256})
			register(app, 0)
			return serve(t, app, req)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v2/sink", bytes.NewReader(oversized))
			req.Header.Set("Content-Type", "application/json")
			resp := tc.send(t, req)
			defer resp.Body.Close()
			raw, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != http.StatusRequestEntityTooLarge {
				t.Fatalf("status %d, want 413; body %s", resp.StatusCode, raw)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
				t.Fatalf("content-type %q", ct)
			}
			var p Problem
			if err := json.Unmarshal(raw, &p); err != nil {
				t.Fatalf("decode %s: %v", raw, err)
			}
			if p.Code != CodePayloadTooLarge || p.Status != http.StatusRequestEntityTooLarge ||
				p.Type != TypeURIPrefix+"platform/payload-too-large" {
				t.Fatalf("problem %+v", p)
			}
		})
	}
}
