package cdn

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCloudflarePurgeBatchesBy30(t *testing.T) {
	var calls [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/zones/z1/purge_cache" || r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "bad", http.StatusBadRequest)
			return
		}
		var body struct {
			Files []string `json:"files"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, body.Files)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	var urls []string
	for i := 0; i < 61; i++ {
		urls = append(urls, fmt.Sprintf("https://image.example/k%d.webp", i))
	}
	c := &Cloudflare{ZoneID: "z1", Token: "tok", Endpoint: srv.URL, HTTP: srv.Client()}
	if err := c.Purge(context.Background(), urls); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 || len(calls[0]) != 30 || len(calls[2]) != 1 {
		t.Errorf("batches %d", len(calls))
	}
}

func TestURLsCoverEveryBase(t *testing.T) {
	got := URLs([]string{"https://a.example", "https://b.example"}, []string{"ab/cd/x.webp", "/ab/cd/x_thumb.webp"})
	if len(got) != 4 || got[1] != "https://a.example/ab/cd/x_thumb.webp" || got[2] != "https://b.example/ab/cd/x.webp" {
		t.Errorf("%v", got)
	}
}
