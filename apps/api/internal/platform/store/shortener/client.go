package shortener

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	MaxAliasesPerStatsCall = 500
	MaxStatsSpanDays       = 92
)

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

type mintRequest struct {
	DestinationURL string `json:"destination_url"`
	Description    string `json:"description,omitempty"`
	// Reuse is never omitempty and never true. The shortener's POST /s2s/links
	// defaults to reusing an existing link with the same destination_url, and
	// every calling site's purchase link for one DLsite product points at the
	// SAME aff URL — accepting the default would collapse all of them onto one
	// alias and destroy per-site attribution, which is the only thing this
	// whole domain measures. 00-workflow §1.8 / §2.
	Reuse bool `json:"reuse"`
}

type Link struct {
	ID       int64  `json:"id"`
	Alias    string `json:"alias"`
	ShortURL string `json:"short_url"`
	Reused   bool   `json:"reused"`
}

func (c *Client) Mint(ctx context.Context, destinationURL, description string) (*Link, error) {
	body := mintRequest{DestinationURL: destinationURL, Description: description, Reuse: false}
	var out Link
	if err := c.post(ctx, "/s2s/links", body, &out); err != nil {
		return nil, err
	}
	if out.Alias == "" || out.ShortURL == "" {
		return nil, fmt.Errorf("shortener: mint returned an empty alias/short_url")
	}
	return &out, nil
}

type DayStat struct {
	Date    string `json:"date"`
	Total   int64  `json:"total"`
	Uniques int64  `json:"uniques"`
	Bots    int64  `json:"bots"`
}

type dailyStatsRequest struct {
	Aliases []string `json:"aliases"`
	From    string   `json:"from"`
	To      string   `json:"to"`
}

type dailyStatsResponse struct {
	Stats map[string][]DayStat `json:"stats"`
}

// DailyStats reads JST-day click buckets for up to MaxAliasesPerStatsCall
// aliases over a closed [from, to] range of at most MaxStatsSpanDays days.
// An alias the shortener does not know comes back as an absent/empty series,
// not an error.
func (c *Client) DailyStats(ctx context.Context, aliases []string, from, to string) (map[string][]DayStat, error) {
	if len(aliases) == 0 {
		return map[string][]DayStat{}, nil
	}
	if len(aliases) > MaxAliasesPerStatsCall {
		return nil, fmt.Errorf("shortener: %d aliases exceeds the %d per-call limit", len(aliases), MaxAliasesPerStatsCall)
	}
	var out dailyStatsResponse
	if err := c.post(ctx, "/s2s/stats/daily", dailyStatsRequest{Aliases: aliases, From: from, To: to}, &out); err != nil {
		return nil, err
	}
	if out.Stats == nil {
		out.Stats = map[string][]DayStat{}
	}
	return out.Stats, nil
}

func (c *Client) post(ctx context.Context, path string, body, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("shortener %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("shortener %s: read body: %w", path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("shortener %s: status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("shortener %s: decode: %w", path, err)
	}
	return nil
}
