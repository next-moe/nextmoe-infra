package communityclient

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
			timeout = 30 * time.Second
		}
		hc = &http.Client{Timeout: timeout}
	}
	return &Client{cfg: cfg, http: hc}
}

type ActivityItem struct {
	Key          string     `json:"key"`
	ActorID      int64      `json:"actor_id"`
	Revision     int64      `json:"revision"`
	Removed      bool       `json:"removed,omitempty"`
	Verb         string     `json:"verb,omitempty"`
	ObjectKind   string     `json:"object_kind,omitempty"`
	ObjectLabel  string     `json:"object_label,omitempty"`
	Title        string     `json:"title,omitempty"`
	Excerpt      string     `json:"excerpt,omitempty"`
	WorkID       *int64     `json:"work_id,omitempty"`
	ObjectRef    string     `json:"object_ref,omitempty"`
	ContentLimit string     `json:"content_limit,omitempty"`
	OccurredAt   *time.Time `json:"occurred_at,omitempty"`
}

type ActivityOutcome struct {
	Key     string `json:"key"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
}

type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("community /activities: status %d: %s", e.Status, e.Body)
}

func (c *Client) WriteActivities(ctx context.Context, items []ActivityItem) ([]ActivityOutcome, error) {
	payload, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/api/v1/community/activities", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(c.cfg.ClientID+":"+c.cfg.ClientSecret)))
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read community response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &StatusError{Status: resp.StatusCode, Body: strings.TrimSpace(string(raw))}
	}
	var env struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Data    struct {
			Results []ActivityOutcome `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode community response: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("community /activities: code %d: %s", env.Code, env.Message)
	}
	if len(env.Data.Results) != len(items) {
		return nil, fmt.Errorf("community /activities: %d results for %d items", len(env.Data.Results), len(items))
	}
	return env.Data.Results, nil
}
