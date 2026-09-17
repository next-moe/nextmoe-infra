package bgmimages

import (
	"context"
	"crypto/tls"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	_ "golang.org/x/image/webp"
)

type pacer struct {
	mu   sync.Mutex
	gap  time.Duration
	next time.Time
}

func newPacer(rps float64) *pacer {
	if rps <= 0 {
		return &pacer{}
	}
	return &pacer{gap: time.Duration(float64(time.Second) / rps)}
}

func (p *pacer) wait(ctx context.Context) error {
	p.mu.Lock()
	now := time.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.gap)
	p.mu.Unlock()
	if d := time.Until(at); d > 0 {
		return sleep(ctx, d)
	}
	return ctx.Err()
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type httpClient struct {
	hc       *http.Client
	api, cdn *pacer
	ua       string
	token    string
	maxRetry int
}

func newHTTPClient(rps float64, ua, token string, maxRetry int) *httpClient {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.MaxIdleConnsPerHost = 32
	tr.IdleConnTimeout = 60 * time.Second
	// A timed-out HTTP/2 connection is never retired: on 2026-08-25 a rotated
	// IPv6 prefix left one pooled h2 connection as a zombie and every request
	// on it timed out for 17 minutes with no error surfaced. HTTP/1.1 closes
	// and redials. The ALPN list must be pinned too, or the server negotiates
	// h2 while the client speaks h1 and every response is a SETTINGS frame.
	tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	if tr.TLSClientConfig == nil {
		tr.TLSClientConfig = &tls.Config{}
	} else {
		tr.TLSClientConfig = tr.TLSClientConfig.Clone()
	}
	tr.TLSClientConfig.NextProtos = []string{"http/1.1"}
	return &httpClient{
		hc:       &http.Client{Timeout: 60 * time.Second, Transport: tr},
		api:      newPacer(rps),
		cdn:      newPacer(rps),
		ua:       ua,
		token:    token,
		maxRetry: maxRetry,
	}
}

func (c *httpClient) getJSON(ctx context.Context, url string) ([]byte, int, error) {
	var lastErr error
	for attempt := 0; attempt <= c.maxRetry; attempt++ {
		if err := c.api.wait(ctx); err != nil {
			return nil, 0, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, 0, err
		}
		req.Header.Set("User-Agent", c.ua)
		req.Header.Set("Accept", "application/json")
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			lastErr = err
			if err := backoff(ctx, attempt, 0); err != nil {
				return nil, 0, err
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusOK && readErr == nil:
			return body, http.StatusOK, nil
		case resp.StatusCode == http.StatusOK:
			lastErr = readErr
		case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			if err := backoff(ctx, attempt, retryAfter(resp.Header.Get("Retry-After"))); err != nil {
				return nil, resp.StatusCode, err
			}
			continue
		default:
			return body, resp.StatusCode, fmt.Errorf("http %d", resp.StatusCode)
		}
		if err := backoff(ctx, attempt, 0); err != nil {
			return nil, 0, err
		}
	}
	return nil, 0, fmt.Errorf("exhausted %d retries: %w", c.maxRetry, lastErr)
}

func (c *httpClient) download(ctx context.Context, url, dest string) (contentType string, err error) {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", err
	}
	var lastErr error
	for attempt := 0; attempt <= c.maxRetry; attempt++ {
		if err := c.cdn.wait(ctx); err != nil {
			return "", err
		}
		ct, retryable, err := c.downloadOnce(ctx, url, dest)
		if err == nil {
			return ct, nil
		}
		lastErr = err
		if !retryable {
			return "", err
		}
		if err := backoff(ctx, attempt, 0); err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("exhausted %d retries: %w", c.maxRetry, lastErr)
}

func (c *httpClient) downloadOnce(ctx context.Context, url, dest string) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", c.ua)
	req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", true, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		return resp.Header.Get("Content-Type"), false, writeAtomic(dest, resp.Body)
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		return "", true, fmt.Errorf("http %d", resp.StatusCode)
	default:
		return "", false, fmt.Errorf("http %d", resp.StatusCode)
	}
}

func writeAtomic(dest string, r io.Reader) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".part-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), dest)
}

func backoff(ctx context.Context, attempt int, after time.Duration) error {
	d := after
	if d == 0 {
		d = time.Duration(1<<attempt)*500*time.Millisecond + rand.N(500*time.Millisecond)
	}
	return sleep(ctx, min(d, 60*time.Second))
}

func retryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode().IsRegular() && fi.Size() > 0
}

func readDims(path string) (w, h int, format string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, "", err
	}
	defer f.Close()
	cfg, format, err := image.DecodeConfig(f)
	if err != nil {
		// image/jpeg refuses a Cb/Cr sampling pair it cannot decode even when only the size is
		// asked for. The first Bangumi drain on 2026-09-17 counted four valid logos as errors,
		// every week, with "unsupported JPEG feature: luma/chroma subsampling ratio".
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			return 0, 0, "", err
		}
		var magic [2]byte
		if _, readErr := io.ReadFull(f, magic[:]); readErr != nil || magic[0] != 0xff || magic[1] != 0xd8 {
			return 0, 0, "", err
		}
		if _, seekErr := f.Seek(0, io.SeekStart); seekErr != nil {
			return 0, 0, "", err
		}
		w, h, jpegErr := jpegSize(f)
		if jpegErr != nil {
			return 0, 0, "", err
		}
		return w, h, "jpeg", nil
	}
	return cfg.Width, cfg.Height, format, nil
}
