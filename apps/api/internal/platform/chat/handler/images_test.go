package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/platform/chat/service"

	"github.com/gofiber/fiber/v3"
)

type uploadOnly struct{}

func (uploadOnly) Meta(context.Context, []string) (map[string]service.ImageMeta, error) {
	return map[string]service.ImageMeta{}, nil
}

func (uploadOnly) Upload(_ context.Context, r io.Reader, name, sub string) (*service.UploadedImage, error) {
	b, _ := io.ReadAll(r)
	if string(b) != "png-bytes" || name != "cat.png" || sub != "chat:1" {
		return nil, service.ErrImageRejected
	}
	return &service.UploadedImage{Hash: strings.Repeat("f", 64), Width: 3, Height: 4}, nil
}

func (uploadOnly) Ping(context.Context, []string) (int64, int, error) { return 0, 0, nil }

func uploadRequest(t *testing.T, app *fiber.App, token, filename, content string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", filename)
	_, _ = fw.Write([]byte(content))
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/v2/chat/images", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestUploadImageRoute(t *testing.T) {
	clean(t)
	svc := service.New(testDB, service.Options{Images: uploadOnly{}, Users: users{1: {ID: 1, CreatedAt: time.Unix(0, 0)}}})
	app := fiber.New()
	Setup(app, Options{Chat: svc, Identify: identify})

	status, out := uploadRequest(t, app, "write:1", "cat.png", "png-bytes")
	if status != http.StatusCreated || out["object"] != "chat_image" || out["image_hash"] != strings.Repeat("f", 64) || out["type"] != "photo" {
		t.Fatalf("upload: %d %v", status, out)
	}
	status, out = uploadRequest(t, app, "write:1", "evil.png", "x")
	if status != http.StatusUnprocessableEntity || out["code"] != "VALIDATION_FAILED" {
		t.Fatalf("rejected bytes: %d %v", status, out)
	}
	status, _ = uploadRequest(t, app, "read:1", "cat.png", "png-bytes")
	if status != http.StatusForbidden {
		t.Fatalf("read scope cannot upload: %d", status)
	}
}
