package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"api/internal/platform/llm"
)

// StatusError carries the upstream HTTP status so a caller can tell transient
// contention from a permanent failure. Cloudflare delivers its per-minute
// inference limit as a real 429 with body code 3021 — not as a 200 carrying an
// error object, and not as the 401 it returns when merely overloaded — so
// matching on the status is enough. The Error text is unchanged from the
// fmt.Errorf it replaced; log lines and their greps still match.
type StatusError struct {
	Code int
	Body string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("upstream http %d: %s", e.Code, e.Body)
}

func IsRateLimited(err error) bool {
	var se *StatusError
	return errors.As(err, &se) && se.Code == http.StatusTooManyRequests
}

type ChatResult struct {
	Content          string
	Channel          string
	FinishReason     string
	PromptTokens     int
	CompletionTokens int
}

type OmniResult struct {
	Flagged        bool
	Categories     map[string]bool
	CategoryScores map[string]float64
	Channel        string
}

type gateway interface {
	Ready(scene string) (string, bool)
	Stream(ctx context.Context, call llm.Call, onEvent func(llm.Event)) (llm.Result, error)
	Moderate(ctx context.Context, r llm.Request) (llm.Response, error)
}

type Client struct {
	gw    gateway
	scene string
}

func NewClient(gw gateway, scene string) *Client { return &Client{gw: gw, scene: scene} }

func (c *Client) Configured() bool {
	_, ok := c.gw.Ready(c.scene)
	return ok
}

func (c *Client) Model() string {
	m, _ := c.gw.Ready(c.scene)
	return m
}

func typed(err error) error {
	var le *llm.Error
	if errors.As(err, &le) && le.Status > 0 {
		return &StatusError{Code: le.Status, Body: le.Msg}
	}
	if llm.KindOf(err) == llm.ErrKindRateLimit {
		return &StatusError{Code: http.StatusTooManyRequests, Body: err.Error()}
	}
	return err
}

func (c *Client) ChatJSON(ctx context.Context, system, user string, maxTokens int) (ChatResult, error) {
	res, err := c.gw.Stream(ctx, llm.Call{
		Scene: c.scene, Buffered: true, JSON: true, MaxTokens: maxTokens, Site: "ai.moderation",
		Messages: []llm.Message{{Role: llm.RoleSystem, Content: system}, {Role: llm.RoleUser, Content: user}},
		Extra:    map[string]json.RawMessage{"temperature": json.RawMessage("0")},
	}, nil)
	if err != nil {
		return ChatResult{}, typed(err)
	}
	channel := res.Model
	if channel == "" {
		channel = c.Model()
	}
	return ChatResult{Content: strings.TrimSpace(res.Text), Channel: channel, FinishReason: res.FinishReason,
		PromptTokens: res.Usage.InputTokens, CompletionTokens: res.Usage.OutputTokens}, nil
}

type OmniClient struct {
	gw    gateway
	scene string
}

func NewOmniClient(gw gateway, scene string) *OmniClient { return &OmniClient{gw: gw, scene: scene} }

func (c *OmniClient) Configured() bool {
	_, ok := c.gw.Ready(c.scene)
	return ok
}

func (c *OmniClient) Model() string {
	m, _ := c.gw.Ready(c.scene)
	return m
}

func (c *OmniClient) Moderate(ctx context.Context, input string) (OmniResult, error) {
	raw, _ := json.Marshal(input)
	res, err := c.gw.Moderate(ctx, llm.Request{Scene: c.scene, Site: "ai.moderation", Body: map[string]json.RawMessage{"input": raw}})
	if err != nil {
		return OmniResult{}, fmt.Errorf("omni: %w", typed(err))
	}
	var parsed struct {
		Model   string `json:"model"`
		Results []struct {
			Flagged        bool               `json:"flagged"`
			Categories     map[string]bool    `json:"categories"`
			CategoryScores map[string]float64 `json:"category_scores"`
		} `json:"results"`
	}
	if err := json.Unmarshal(res.Body, &parsed); err != nil {
		return OmniResult{}, fmt.Errorf("decode omni response: %w", err)
	}
	if len(parsed.Results) == 0 {
		return OmniResult{}, errors.New("omni returned no results")
	}
	channel := parsed.Model
	if channel == "" {
		channel = c.Model()
	}
	r := parsed.Results[0]
	return OmniResult{Flagged: r.Flagged, Categories: r.Categories, CategoryScores: r.CategoryScores, Channel: channel}, nil
}
