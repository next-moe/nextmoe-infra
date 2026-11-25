// Package cdn purges deleted image URLs from the Cloudflare edge. The image
// hosts cache for a year (2026-11-01 cache rule), so deleting the R2 object
// alone left a taken-down image public for up to a year.
package cdn

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"api/pkg/config"
)

type Purger interface {
	Purge(ctx context.Context, urls []string) error
}

// Cloudflare purges by URL; the free plan takes at most 30 files per call.
type Cloudflare struct {
	ZoneID   string
	Token    string
	Endpoint string
	HTTP     *http.Client
}

const batch = 30

func (c *Cloudflare) Purge(ctx context.Context, urls []string) error {
	for i := 0; i < len(urls); i += batch {
		chunk := urls[i:min(i+batch, len(urls))]
		body, _ := json.Marshal(map[string]any{"files": chunk})
		endpoint := c.Endpoint
		if endpoint == "" {
			endpoint = "https://api.cloudflare.com/client/v4"
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/zones/"+c.ZoneID+"/purge_cache", bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		resp.Body.Close()
		var out struct {
			Success bool `json:"success"`
		}
		if resp.StatusCode != http.StatusOK || json.Unmarshal(b, &out) != nil || !out.Success {
			return fmt.Errorf("cloudflare purge: HTTP %d: %.300s", resp.StatusCode, b)
		}
	}
	return nil
}

type noop struct{ once sync.Once }

func (n *noop) Purge(_ context.Context, urls []string) error {
	n.once.Do(func() {
		slog.Warn("image cdn purge is not configured; deleted images stay cached at the edge",
			"set", "KUN_IMAGE_CF_ZONE_ID, KUN_IMAGE_CF_PURGE_TOKEN, KUN_IMAGE_PURGE_BASES")
	})
	return nil
}

// FromConfig returns the Cloudflare purger when its zone and token are set.
func FromConfig(c config.ImagePurgeConfig) Purger {
	if c.ZoneID == "" || c.Token == "" {
		return &noop{}
	}
	return &Cloudflare{ZoneID: c.ZoneID, Token: c.Token, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Bases are every public host the bucket is served from; a client may have
// its own (oauth_clients.image_cdn_base), so the list is configured, not
// derived from the default base alone.
func Bases(c config.ImageServiceConfig) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, b := range append(strings.Split(c.Purge.Bases, ","), c.CDNBase) {
		b = strings.TrimRight(strings.TrimSpace(b), "/")
		if b != "" && !seen[b] {
			seen[b] = true
			out = append(out, b)
		}
	}
	return out
}

// URLs is every edge URL of the given object keys under every base.
func URLs(bases, keys []string) []string {
	out := make([]string, 0, len(bases)*len(keys))
	for _, b := range bases {
		for _, k := range keys {
			out = append(out, b+"/"+strings.TrimLeft(k, "/"))
		}
	}
	return out
}
