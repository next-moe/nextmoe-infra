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

	"api/internal/platform/ai/upstream"
)

const llmTimeout = 90 * time.Second

type httpClient struct {
	baseURL string
	token   string
	model   string
	http    *http.Client
}

func newClient(baseURL, token, model string) *httpClient {
	return &httpClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		model:   model,
		http:    &http.Client{Timeout: llmTimeout},
	}
}

type chatRequest struct {
	Model          string         `json:"model"`
	Messages       []chatMessage  `json:"messages"`
	MaxTokens      int            `json:"max_tokens"`
	Temperature    float64        `json:"temperature"`
	ResponseFormat map[string]any `json:"response_format,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (c *httpClient) ChatJSON(ctx context.Context, system, user string, maxTokens int) (upstream.ChatResult, error) {
	body := chatRequest{
		Model:       c.model,
		MaxTokens:   maxTokens,
		Temperature: 0,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: map[string]any{"type": "json_object"},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return upstream.ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return upstream.ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return upstream.ChatResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return upstream.ChatResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return upstream.ChatResult{}, &upstream.StatusError{Code: resp.StatusCode, Body: truncate(string(data), 300)}
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return upstream.ChatResult{}, fmt.Errorf("decode chat response: %w (body: %s)", err, truncate(string(data), 300))
	}
	if cr.Error != nil {
		return upstream.ChatResult{}, fmt.Errorf("upstream error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return upstream.ChatResult{}, fmt.Errorf("upstream returned no choices")
	}
	channel := cr.Model
	if channel == "" {
		channel = c.model
	}
	return upstream.ChatResult{
		Content:          strings.TrimSpace(cr.Choices[0].Message.Content),
		Channel:          channel,
		FinishReason:     cr.Choices[0].FinishReason,
		PromptTokens:     cr.Usage.PromptTokens,
		CompletionTokens: cr.Usage.CompletionTokens,
	}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
