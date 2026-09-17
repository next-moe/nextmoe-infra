package charportraits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"api/pkg/imageclient"
)

func TestFillPortraitDecodeFailedIsRejectedWithoutRetry(t *testing.T) {
	uploads := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uploads++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":80010,"message":"图片解码失败"}`))
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	path := filepath.Join(dir, "ch", "52", "175652.jpg")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := &runner{cli: imageclient.New(imageclient.Config{BaseURL: srv.URL, ClientID: "t", ClientSecret: "t"})}
	if r.fillPortrait(context.Background(), dir, candidate{CatalogCharacterID: 1, ImageID: "ch175652"}) {
		t.Fatal("decode failure must not be quota")
	}
	if r.rejected != 1 || r.errors != 0 {
		t.Fatalf("rejected=%d errors=%d, want rejected=1 errors=0", r.rejected, r.errors)
	}
	if uploads != 1 {
		t.Fatalf("uploads=%d, want 1", uploads)
	}
}
