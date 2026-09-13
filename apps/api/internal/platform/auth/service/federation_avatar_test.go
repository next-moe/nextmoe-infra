package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchUpstreamAvatar(t *testing.T) {
	t.Run("rejects a non-https url without dialling", func(t *testing.T) {
		if _, _, err := fetchUpstreamAvatar(context.Background(), "http://example.com/a.png"); err == nil {
			t.Fatal("expected http:// to be refused")
		}
	})

	t.Run("rejects a non-image content type", func(t *testing.T) {
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		}))
		defer ts.Close()
		defer swapAvatarHTTP(t, ts.Client())()

		if _, _, err := fetchUpstreamAvatar(context.Background(), ts.URL); err == nil {
			t.Fatal("expected a text/html body to be refused")
		}
	})

	t.Run("caps the body at the size limit", func(t *testing.T) {
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = io.Copy(w, strings.NewReader(strings.Repeat("x", upstreamAvatarMaxBytes+4096)))
		}))
		defer ts.Close()
		defer swapAvatarHTTP(t, ts.Client())()

		body, ext, err := fetchUpstreamAvatar(context.Background(), ts.URL)
		if err != nil {
			t.Fatalf("fetch: %v", err)
		}
		defer body.Close()

		n, err := io.Copy(io.Discard, body)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if n != upstreamAvatarMaxBytes {
			t.Fatalf("read %d bytes, want the %d cap", n, upstreamAvatarMaxBytes)
		}
		if ext != ".png" {
			t.Fatalf("ext = %q, want .png", ext)
		}
	})
}

func TestAvatarExt(t *testing.T) {
	cases := map[string]string{
		"image/png":                ".png",
		"image/webp":               ".webp",
		"image/gif":                ".gif",
		"image/avif":               ".avif",
		"image/jpeg":               ".jpg",
		"IMAGE/PNG; charset=utf-8": ".png",
		"":                         ".jpg",
	}
	for ct, want := range cases {
		if got := avatarExt(ct); got != want {
			t.Errorf("avatarExt(%q) = %q, want %q", ct, got, want)
		}
	}
}

func swapAvatarHTTP(t *testing.T, c *http.Client) func() {
	t.Helper()
	prev := upstreamAvatarHTTP
	upstreamAvatarHTTP = c
	return func() { upstreamAvatarHTTP = prev }
}
