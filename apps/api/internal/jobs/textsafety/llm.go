package textsafety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type LLM struct {
	base  string
	token string
	model string
	http  *http.Client

	promptTokens     atomic.Int64
	completionTokens atomic.Int64
	calls            atomic.Int64
}

func NewLLM(base, token, model string) *LLM {
	return &LLM{
		base:  strings.TrimRight(base, "/"),
		token: token,
		model: model,
		http:  &http.Client{Timeout: 12 * time.Minute},
	}
}

func (c *LLM) Model() string { return c.model }

func (c *LLM) Usage() (calls, prompt, completion int64) {
	return c.calls.Load(), c.promptTokens.Load(), c.completionTokens.Load()
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// ErrBlocked: grok refused whole intro batches during the 2026-10-24 backfill
// ("Request blocked ... under the model provider's usage guidelines"), on every
// retry of the same text, so callers split the batch to isolate the text.
var ErrBlocked = errors.New("llm refused the prompt under the provider's usage guidelines")

// Both upstreams answer 429 by the minute under load (Workers AI: "inference
// request per min rate reached"); four retries over ~2.5 min dropped whole intro
// batches whose earlier calls had already succeeded, so retries wait longer.
var retrySchedule = []time.Duration{
	5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second,
	90 * time.Second, 120 * time.Second, 120 * time.Second, 120 * time.Second,
}

// Ask sends one user message and decodes the JSON object in the reply into out.
func (c *LLM) Ask(ctx context.Context, prompt string, out any) error {
	raw, err := json.Marshal(map[string]any{
		"model":       c.model,
		"temperature": 0,
		"messages":    []chatMessage{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return err
	}
	var lastErr error
	prose := 0
	for attempt := 0; ; attempt++ {
		content, retryable, err := c.post(ctx, raw)
		if err == nil {
			if err = decodeJSONObject(content, out); err == nil {
				return nil
			}
			// A model that refuses in prose ("这类内容涉及未成年人的性剥削…") does so
			// on every retry; the 2026-10-24 backfill retried 29 such intros for
			// ~10 min each. Two prose replies count as a refusal.
			if prose++; prose >= 2 {
				return fmt.Errorf("%w: %v", ErrBlocked, err)
			}
			retryable = true
		}
		lastErr = err
		if !retryable || attempt >= len(retrySchedule) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retrySchedule[attempt] + time.Duration(rand.Int64N(int64(retrySchedule[attempt]/2)+1))):
		}
	}
}

func (c *LLM) post(ctx context.Context, raw []byte) (string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", true, err
	}
	if resp.StatusCode == http.StatusUnprocessableEntity {
		return "", false, fmt.Errorf("%w: %s", ErrBlocked, truncate(string(body), 200))
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("llm http %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("llm http %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", true, fmt.Errorf("decode llm response: %w", err)
	}
	if cr.Error != nil {
		return "", true, fmt.Errorf("llm error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", true, fmt.Errorf("llm returned no choices")
	}
	if fr := cr.Choices[0].FinishReason; fr != "" && fr != "stop" {
		return "", true, fmt.Errorf("llm finish_reason=%q", fr)
	}
	c.calls.Add(1)
	c.promptTokens.Add(cr.Usage.PromptTokens)
	c.completionTokens.Add(cr.Usage.CompletionTokens)
	return cr.Choices[0].Message.Content, false, nil
}

func decodeJSONObject(content string, out any) error {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return fmt.Errorf("no JSON object in reply: %s", truncate(content, 200))
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), out); err != nil {
		return fmt.Errorf("decode reply: %w (%s)", err, truncate(content, 200))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
