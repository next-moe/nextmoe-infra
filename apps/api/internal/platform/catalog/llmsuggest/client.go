package llmsuggest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type Client struct {
	baseURL string
	model   string
	apiKey  string
	http    *http.Client
}

func NewClient(baseURL, model string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  os.Getenv("KUN_LLM_API_KEY"),
		http:    &http.Client{Timeout: 180 * time.Second},
	}
}

func (c *Client) applyAuth(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
}

type chatRequest struct {
	Model             string         `json:"model"`
	Messages          []chatMessage  `json:"messages"`
	MaxTokens         int            `json:"max_tokens"`
	Temperature       float64        `json:"temperature"`
	ChatTemplateKwarg map[string]any `json:"chat_template_kwargs"`
	ResponseFormat    map[string]any `json:"response_format,omitempty"`
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
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// StatusError carries the upstream status code so the retry loop can tell a
// rate limit from a request the gateway will never accept. The 2026-09 wave
// lost 3,341 of 6,413 workpair judgements to "vllm http 429": the loop retried
// three times with no pause, which against a concurrency-capped gateway is
// three more 429s.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("vllm http %d: %s", e.Status, e.Body)
}

type ChatResult struct {
	Content          string
	CompletionTokens int
	FinishReason     string
}

func (c *Client) ChatJSON(ctx context.Context, system, user, schemaName string, schema map[string]any, maxTokens int) (ChatResult, error) {
	body := chatRequest{
		Model:             c.model,
		MaxTokens:         maxTokens,
		Temperature:       0,
		ChatTemplateKwarg: map[string]any{"enable_thinking": false},
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		ResponseFormat: map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   schemaName,
				"schema": schema,
			},
		},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return ChatResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return ChatResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return ChatResult{}, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return ChatResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return ChatResult{}, &StatusError{Status: resp.StatusCode, Body: truncate(string(data), 300)}
	}
	var cr chatResponse
	if err := json.Unmarshal(data, &cr); err != nil {
		return ChatResult{}, fmt.Errorf("decode chat response: %w (body: %s)", err, truncate(string(data), 300))
	}
	if cr.Error != nil {
		return ChatResult{}, fmt.Errorf("vllm error: %s", cr.Error.Message)
	}
	if len(cr.Choices) == 0 {
		return ChatResult{}, fmt.Errorf("vllm returned no choices")
	}
	return ChatResult{
		Content:          strings.TrimSpace(cr.Choices[0].Message.Content),
		CompletionTokens: cr.Usage.CompletionTokens,
		FinishReason:     cr.Choices[0].FinishReason,
	}, nil
}

func (c *Client) Ping(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	ids := make([]string, len(out.Data))
	for i, d := range out.Data {
		ids[i] = d.ID
	}
	return ids, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
