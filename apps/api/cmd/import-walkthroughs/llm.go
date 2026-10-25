package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"

	"api/internal/platform/catalog/llmsuggest"
)

type llm struct {
	client *llmsuggest.Client
	model  string
}

var retryBackoff = []time.Duration{
	5 * time.Second, 10 * time.Second, 20 * time.Second, 40 * time.Second,
	60 * time.Second, 60 * time.Second, 60 * time.Second, 60 * time.Second,
}

func mustSchema(raw string) map[string]any {
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		panic(err)
	}
	return m
}

func (l *llm) chatJSON(ctx context.Context, system, user, name string, schema map[string]any, maxTokens int, out any) (llmsuggest.ChatResult, error) {
	var lastErr error
	for attempt := 0; ; attempt++ {
		res, err := l.client.ChatJSON(ctx, system, user, name, schema, maxTokens)
		if err == nil {
			if res.FinishReason == "length" {
				return res, fmt.Errorf("completion truncated at %d tokens: …%s", res.CompletionTokens, tail(res.Content, 300))
			}
			if err = json.Unmarshal([]byte(res.Content), out); err == nil {
				return res, nil
			}
			err = fmt.Errorf("unmarshal %s: %w", name, err)
			if attempt >= 2 {
				return res, err
			}
			lastErr = err
			continue
		}
		lastErr = err
		if !retryable(err) || attempt >= len(retryBackoff) {
			return res, lastErr
		}
		d := retryBackoff[attempt]
		d += rand.N(d / 2)
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return res, ctx.Err()
		}
	}
}

func retryable(err error) bool {
	var se *llmsuggest.StatusError
	if errors.As(err, &se) {
		return se.Status == http.StatusTooManyRequests || se.Status == http.StatusRequestTimeout || se.Status >= 500
	}
	return !errors.Is(err, context.Canceled)
}

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}
