package main

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

var descRetryBackoff = []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}

type descHTTPTranslator struct {
	baseURL   string
	token     string
	model     string
	maxTokens int
	http      *http.Client
}

func newDescHTTPTranslator(baseURL, token, model string, maxTokens int) *descHTTPTranslator {
	return &descHTTPTranslator{
		baseURL:   strings.TrimRight(baseURL, "/"),
		token:     token,
		model:     model,
		maxTokens: maxTokens,
		http:      &http.Client{Timeout: 300 * time.Second},
	}
}

func (t *descHTTPTranslator) Configured() bool { return t.baseURL != "" && t.token != "" }

type descChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type descChatRequest struct {
	Model              string              `json:"model"`
	Messages           []descChatMessage   `json:"messages"`
	MaxTokens          int                 `json:"max_tokens"`
	Temperature        float64             `json:"temperature"`
	ChatTemplateKwargs *descTemplateKwargs `json:"chat_template_kwargs,omitempty"`
}

type descTemplateKwargs struct {
	EnableThinking bool `json:"enable_thinking"`
}

// With thinking off, about 5% of the 2026-09-25 run came back as the first
// sentence alone (Lactation, Dress, Earrings: every paragraph after it gone).
// Asking again with thinking on restored all of them.
func (t *descHTTPTranslator) Translate(ctx context.Context, name, prepared string, gloss []glossPair) (string, error) {
	zh, err := t.translate(ctx, name, prepared, gloss, false)
	if err != nil || nonEmptyLines(zh) >= nonEmptyLines(prepared) {
		return zh, err
	}
	return t.translate(ctx, name, prepared, gloss, true)
}

func nonEmptyLines(s string) int {
	n := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

func (t *descHTTPTranslator) translate(ctx context.Context, name, prepared string, gloss []glossPair, thinking bool) (string, error) {
	reqBody := descChatRequest{
		Model:       t.model,
		MaxTokens:   t.maxTokens,
		Temperature: 0,
		Messages: []descChatMessage{
			{Role: "system", Content: DescribeSystemPrompt},
			{Role: "user", Content: describeUserMessage(name, prepared, gloss)},
		},
	}
	if !thinking {
		reqBody.ChatTemplateKwargs = &descTemplateKwargs{EnableThinking: false}
	}
	raw, err := json.Marshal(reqBody)
	if err != nil {
		return "", err
	}
	var last error
	for attempt := 0; ; attempt++ {
		zh, retryable, err := t.once(ctx, raw)
		if err == nil {
			return zh, nil
		}
		last = err
		if !retryable || attempt >= len(descRetryBackoff) {
			return "", last
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(descRetryBackoff[attempt]):
		}
	}
}

func (t *descHTTPTranslator) once(ctx context.Context, raw []byte) (zh string, retryable bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+t.token)

	resp, err := t.http.Do(req)
	if err != nil {
		return "", ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", true, err
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return "", true, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncateRunes(string(data), 300))
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("gateway http %d: %s", resp.StatusCode, truncateRunes(string(data), 300))
	}
	var cr struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &cr); err != nil {
		return "", false, fmt.Errorf("decode chat response: %w", err)
	}
	if cr.Error != nil {
		return "", false, fmt.Errorf("gateway error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return "", false, fmt.Errorf("gateway returned no choices")
	}
	if cr.Choices[0].FinishReason != "stop" {
		return "", false, fmt.Errorf("generation finished with finish_reason=%q — refusing partial output", cr.Choices[0].FinishReason)
	}
	zh = strings.TrimSpace(cr.Choices[0].Message.Content)
	if zh == "" {
		return "", false, fmt.Errorf("gateway returned empty content")
	}
	return zh, false, nil
}
