package search

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultEmbedModel = "@cf/qwen/qwen3-embedding-0.6b"

type Embedder interface {
	Model() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

type OpenAIEmbedder struct {
	base  string
	token string
	model string
	http  *http.Client
	retry []time.Duration
}

func NewOpenAIEmbedder(baseURL, token, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		base:  strings.TrimRight(baseURL, "/"),
		token: token,
		model: model,
		http:  &http.Client{Timeout: 60 * time.Second},
		retry: []time.Duration{2 * time.Second, 5 * time.Second, 15 * time.Second, 30 * time.Second, 60 * time.Second},
	}
}

func (e *OpenAIEmbedder) Model() string { return e.model }

type embedStatusError struct {
	code int
	body string
}

func (e *embedStatusError) Error() string {
	return fmt.Sprintf("embeddings http %d: %s", e.code, e.body)
}

func retryable(err error) bool {
	var se *embedStatusError
	if errors.As(err, &se) {
		return se.code == http.StatusTooManyRequests || se.code >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// Workers AI caps the tokens of one embeddings request without publishing the
// number: the first full run died on a 50-card batch with 400 "AiError: input
// too big" (code 3030), so an oversized batch is halved instead of failing.
func tooBig(err error) bool {
	var se *embedStatusError
	return errors.As(err, &se) && se.code == http.StatusBadRequest && strings.Contains(se.body, "input too big")
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	out, err := e.embedRetrying(ctx, texts)
	if !tooBig(err) || len(texts) == 1 {
		return out, err
	}
	mid := len(texts) / 2
	head, err := e.Embed(ctx, texts[:mid])
	if err != nil {
		return nil, err
	}
	tail, err := e.Embed(ctx, texts[mid:])
	if err != nil {
		return nil, err
	}
	return append(head, tail...), nil
}

func (e *OpenAIEmbedder) embedRetrying(ctx context.Context, texts []string) ([][]float32, error) {
	for attempt := 0; ; attempt++ {
		out, err := e.embedOnce(ctx, texts)
		if err == nil || attempt >= len(e.retry) || !retryable(err) {
			return out, err
		}
		select {
		case <-time.After(e.retry[attempt]):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

func (e *OpenAIEmbedder) embedOnce(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(map[string]any{"model": e.model, "input": texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.base+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.token)
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &embedStatusError{code: resp.StatusCode, body: truncateRunes(string(raw), 300)}
	}
	var parsed struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(parsed.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings: got %d vectors for %d inputs", len(parsed.Data), len(texts))
	}
	out := make([][]float32, len(texts))
	for i, d := range parsed.Data {
		idx := d.Index
		if idx < 0 || idx >= len(texts) || out[idx] != nil {
			idx = i
		}
		out[idx] = d.Embedding
	}
	return out, nil
}
