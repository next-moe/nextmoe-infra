package realtime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"api/internal/platform/chat/service"
)

func TestSendBatchesOnePublishPerUser(t *testing.T) {
	var got struct {
		Commands []struct {
			Publish struct {
				Channel string          `json:"channel"`
				Data    json.RawMessage `json:"data"`
			} `json:"publish"`
		} `json:"commands"`
	}
	var key, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, path = r.Header.Get("X-API-Key"), r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"replies":[{"publish":{}},{"publish":{}}]}`))
	}))
	defer srv.Close()

	c := NewCentrifugo(srv.URL+"/", "k")
	err := c.send(context.Background(), []service.Delivery{
		{UserID: 1, Data: map[string]any{"type": "typing"}},
		{UserID: 2, Data: map[string]any{"type": "typing"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if key != "k" || path != "/api/batch" {
		t.Fatalf("key %q path %q", key, path)
	}
	if len(got.Commands) != 2 || got.Commands[0].Publish.Channel != "user:1" || got.Commands[1].Publish.Channel != "user:2" {
		t.Fatalf("commands: %+v", got.Commands)
	}
}

func TestSendReportsCentrifugoErrors(t *testing.T) {
	for name, body := range map[string]string{
		"top level": `{"error":{"code":101,"message":"unauthorized"}}`,
		"one reply": `{"replies":[{"publish":{}},{"error":{"code":102,"message":"unknown channel"}}]}`,
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		err := NewCentrifugo(srv.URL, "k").send(context.Background(), []service.Delivery{{UserID: 1}, {UserID: 2}})
		srv.Close()
		if err == nil || !strings.Contains(err.Error(), "centrifugo error") {
			t.Errorf("%s: want the Centrifugo error surfaced, got %v", name, err)
		}
	}
}

func TestPublishNeverBlocks(t *testing.T) {
	c := NewCentrifugo("http://unused", "k")
	for i := 0; i < cap(c.queue)+10; i++ {
		c.Publish(context.Background(), []service.Delivery{{UserID: 1}})
	}
	if len(c.queue) != cap(c.queue) {
		t.Fatalf("queue %d/%d", len(c.queue), cap(c.queue))
	}
	if NewCentrifugo("", "k") != nil || NewCentrifugo("http://x", "") != nil {
		t.Fatal("an unconfigured Centrifugo is nil")
	}
}
