package mcpface

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const upstreamTimeout = 30 * time.Second

// The catalog's limiter keys a credential-less call on this header when the
// peer is a private address. Without it every anonymous MCP user shares this
// container's one bucket — the 2026-08-27 shape, where a first-party backend
// spent one egress IP's daily quota for its whole site.
const clientIPHeader = "CF-Connecting-IP"

type Upstream struct {
	base   string
	client *http.Client
}

func NewUpstream(base string) *Upstream {
	return &Upstream{
		base: strings.TrimRight(base, "/"),
		client: &http.Client{
			Timeout: upstreamTimeout,
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 20,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

func (u *Upstream) Get(ctx context.Context, path string, query url.Values, authorization, clientIP string) (status int, body []byte, err error) {
	target := u.base + path
	if enc := query.Encode(); enc != "" {
		target += "?" + enc
	}

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		status, body, err = u.do(ctx, target, authorization, clientIP)
		if err == nil {
			return status, body, nil
		}
		if ctx.Err() != nil {
			break
		}
	}
	return 0, nil, err
}

func (u *Upstream) do(ctx context.Context, target, authorization, clientIP string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("build request: %w", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	if clientIP != "" {
		req.Header.Set(clientIPHeader, clientIP)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := u.client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read body: %w", err)
	}
	return resp.StatusCode, b, nil
}
