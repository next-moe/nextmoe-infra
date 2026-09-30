package worker

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSymbolCacheLRU(t *testing.T) {
	dir := t.TempDir()
	c := &SymbolCache{Dir: dir, Cap: 1000}
	ctx := context.Background()
	put := func(sha, body string) {
		t.Helper()
		_, err := c.Get(ctx, sha, func(context.Context) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(body)), nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	body := strings.Repeat("x", 400)
	put("aaa", body)
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "aaa"), old, old); err != nil {
		t.Fatal(err)
	}
	put("bbb", body)
	mid := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "bbb"), mid, mid); err != nil {
		t.Fatal(err)
	}
	put("ccc", body)
	if _, err := os.Stat(filepath.Join(dir, "aaa")); !os.IsNotExist(err) {
		t.Fatalf("oldest file kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "bbb")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ccc")); err != nil {
		t.Fatal(err)
	}
}
