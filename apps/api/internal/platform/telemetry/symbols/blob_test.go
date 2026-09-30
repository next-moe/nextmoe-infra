package symbols

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestFSBlobStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := NewFSStore(t.TempDir())
	plain := []byte("hello symbols")
	key := BlobKey("abc123")
	if err := s.Put(ctx, key, bytes.NewReader(plain)); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(s.root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatalf("disk is not gzip: %x", raw[:min(4, len(raw))])
	}
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	_ = zr.Close()
	if !bytes.Equal(got, plain) {
		t.Fatalf("gzip payload=%q", got)
	}
	rc, err := s.Open(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatalf("open=%q", out)
	}
	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	_, err = s.Open(ctx, key)
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing: %v", err)
	}
}
