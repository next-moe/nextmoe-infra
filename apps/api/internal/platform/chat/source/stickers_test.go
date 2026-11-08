package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOfficialStickers(t *testing.T) {
	var calls atomic.Int32
	var down atomic.Bool
	var extra atomic.Value
	extra.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if down.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.Header.Get("Authorization") != "Bearer nmk_live_k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body any
		switch {
		case r.URL.Path == "/v2/sticker/packs" && r.URL.Query().Get("official") != "true":
			w.WriteHeader(http.StatusBadRequest)
			return
		case r.URL.Path == "/v2/sticker/packs" && r.URL.Query().Get("cursor") == "":
			body = map[string]any{"items": []any{map[string]any{"id": "p1"}}, "next_cursor": "cur_2"}
		case r.URL.Path == "/v2/sticker/packs":
			body = map[string]any{"items": []any{map[string]any{"id": "p2"}}}
		case r.URL.Path == "/v2/sticker/packs/p1":
			body = map[string]any{"stickers": []any{map[string]any{"image": map[string]any{"hash": "a"}}}}
		case r.URL.Path == "/v2/sticker/packs/p2":
			ss := []any{map[string]any{"image": map[string]any{"hash": "b"}}}
			if e := extra.Load().(string); e != "" {
				ss = append(ss, map[string]any{"image": map[string]any{"hash": e}})
			}
			body = map[string]any{"stickers": ss}
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	clock := time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC)
	s := NewOfficialStickers(srv.URL+"/", "nmk_live_k")
	s.now = func() time.Time { return clock }
	ctx := context.Background()
	is := func(h string) bool {
		t.Helper()
		ok, err := s.IsOfficial(ctx, h)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !is("a") || !is("b") || is("c") {
		t.Fatal("both pages of packs are read")
	}
	if n := calls.Load(); n != 4 {
		t.Fatalf("one load is 2 list pages + 2 packs, got %d calls", n)
	}
	extra.Store("c")
	if is("c") {
		t.Fatal("a miss inside the reload floor does not refetch")
	}
	clock = clock.Add(stickerMissReload + time.Second)
	if !is("c") {
		t.Fatal("a miss past the floor refetches")
	}
	down.Store(true)
	clock = clock.Add(stickerSetTTL + time.Second)
	if !is("a") {
		t.Fatal("a failed refresh keeps the last set")
	}

	fresh := NewOfficialStickers(srv.URL, "nmk_live_k")
	if _, err := fresh.IsOfficial(ctx, "a"); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("no set yet and the face is down: %v", err)
	}
}
