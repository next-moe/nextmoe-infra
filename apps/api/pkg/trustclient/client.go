package trustclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type Client struct {
	cfg  Config
	http *http.Client
}

func New(cfg Config) *Client {
	if cfg.BaseURL == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		timeout := cfg.Timeout
		if timeout == 0 {
			timeout = 15 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{cfg: cfg, http: hc}
}

type ForwardRequest struct {
	Site         string   `json:"site"`
	SubjectKind  string   `json:"subject_kind"`
	SubjectID    string   `json:"subject_id"`
	Severity     *int16   `json:"severity,omitempty"`
	WeightSum    *float32 `json:"weight_sum,omitempty"`
	ContextNote  *string  `json:"context_note,omitempty"`
	AuthorID     *int64   `json:"author_id,omitempty"`
	ForwarderRef *string  `json:"forwarder_ref,omitempty"`
}

type envelope[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

type forwardData struct {
	ReviewItemID int64 `json:"review_item_id"`
	Created      bool  `json:"created"`
}

func (c *Client) Forward(ctx context.Context, req ForwardRequest) (trustItemID int64, created bool, err error) {
	var env envelope[forwardData]
	if err := c.post(ctx, "/api/v1/trust/forward", req, &env); err != nil {
		return 0, false, err
	}
	return env.Data.ReviewItemID, env.Data.Created, nil
}

type ScanRequest struct {
	Site        string `json:"site,omitempty"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Text        string `json:"text"`
	AuthorID    *int64 `json:"author_id,omitempty"`
}

type scanData struct {
	ScanID    int64 `json:"scan_id"`
	Truncated bool  `json:"truncated"`
}

func (c *Client) Scan(ctx context.Context, req ScanRequest) (scanID int64, truncated bool, err error) {
	var env envelope[scanData]
	if err := c.post(ctx, "/api/v1/trust/scan", req, &env); err != nil {
		return 0, false, err
	}
	return env.Data.ScanID, env.Data.Truncated, nil
}

type CheckRequest struct {
	Site     string `json:"site,omitempty"`
	Text     string `json:"text"`
	AuthorID *int64 `json:"author_id,omitempty"`
}

type checkData struct {
	Decision string   `json:"decision"`
	Matched  []string `json:"matched"`
}

func (c *Client) Check(ctx context.Context, req CheckRequest) (decision string, matched []string, err error) {
	var env envelope[checkData]
	if err := c.post(ctx, "/api/v1/trust/check", req, &env); err != nil {
		return "", nil, err
	}
	return env.Data.Decision, env.Data.Matched, nil
}

type resolveRequest struct {
	ReviewItemID int64   `json:"review_item_id"`
	Outcome      string  `json:"outcome"`
	ActorRef     *string `json:"actor_ref,omitempty"`
}

type resolveData struct {
	Closed bool `json:"closed"`
}

func (c *Client) Resolve(ctx context.Context, trustItemID int64, outcome, actorRef string) (closed bool, err error) {
	req := resolveRequest{ReviewItemID: trustItemID, Outcome: outcome}
	if actorRef != "" {
		req.ActorRef = &actorRef
	}
	var env envelope[resolveData]
	if err := c.post(ctx, "/api/v1/trust/forward/resolve", req, &env); err != nil {
		return false, err
	}
	return env.Data.Closed, nil
}

type StatusError struct {
	Path   string
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("trust %s: status %d: %s", e.Path, e.Status, e.Body)
}

func (e *StatusError) StatusCode() int { return e.Status }

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.cfg.ClientID+":"+c.cfg.ClientSecret)))
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read trust response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &StatusError{Path: path, Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var head struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return fmt.Errorf("decode trust response: %w", err)
	}
	if head.Code != 0 {
		return fmt.Errorf("trust %s: code %d: %s", path, head.Code, head.Message)
	}
	return json.Unmarshal(raw, out)
}
