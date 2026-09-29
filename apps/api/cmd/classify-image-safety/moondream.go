package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

type moondreamClient struct {
	apiRoot   string
	accountID string
	token     string
	model     string
	http      *http.Client
	// neurons are fractional; keep them as micro-units so the counter can be atomic.
	microNeurons  atomic.Int64
	succeededOnce atomic.Bool
}

func newMoondreamClient(accountID, token, model string) *moondreamClient {
	return &moondreamClient{
		apiRoot:   "https://api.cloudflare.com",
		accountID: accountID,
		token:     token,
		model:     model,
		http:      &http.Client{Timeout: 120 * time.Second},
	}
}

func (c *moondreamClient) configured() bool {
	return c.accountID != "" && c.token != ""
}

func (c *moondreamClient) neurons() float64 {
	return float64(c.microNeurons.Load()) / 1e6
}

type moondreamEvent struct {
	Error  string `json:"error"`
	Output []struct {
		Answer       string `json:"answer"`
		FinishReason string `json:"finish_reason"`
	} `json:"output"`
	Usage struct {
		Neurons float64 `json:"neurons"`
	} `json:"usage"`
}

// A non-streaming call returns HTTP 200 with "success":true and an empty result
// when the model runner could not serve the request — the real error only ever
// reaches the SSE channel, so this client always sets stream:true.
func (c *moondreamClient) ask(ctx context.Context, dataURI, question string) (string, error) {
	raw, err := json.Marshal(map[string]any{
		"task":       "query",
		"image":      dataURI,
		"question":   question,
		"max_tokens": 8,
		"stream":     true,
		"reasoning":  false,
	})
	if err != nil {
		return "", err
	}
	url := fmt.Sprintf("%s/client/v4/accounts/%s/ai/run/%s", c.apiRoot, c.accountID, c.model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", retryable{err}
	}
	defer resp.Body.Close()

	// 409 carries "Cog streaming prediction failed" — a transient model-runner
	// fault, not a rejection of the request, and it reappears under concurrency.
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusConflict || resp.StatusCode >= 500 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		if resp.StatusCode == http.StatusTooManyRequests && strings.Contains(string(body), "daily free allocation") {
			return "", errDailyQuota
		}
		return "", retryable{fmt.Errorf("workers-ai http %d: %s", resp.StatusCode, truncate(string(body), 200))}
	}
	// Under sustained concurrency the gateway sheds load as 401 code 10000
	// "Authentication error" on a token that is working — the first full-corpus run
	// lost 2,734 images to it. Retry it only once the token has proven itself, so a
	// genuinely wrong token still fails on the first request instead of grinding.
	if resp.StatusCode == http.StatusUnauthorized && c.succeededOnce.Load() {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", retryable{fmt.Errorf("workers-ai http 401: %s", truncate(string(body), 200))}
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("workers-ai http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var (
		answer   string
		streamed string
		total    float64
	)
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimSpace(line[6:])
		if payload == "[DONE]" {
			break
		}
		var ev moondreamEvent
		if json.Unmarshal([]byte(payload), &ev) != nil {
			continue
		}
		if ev.Error != "" {
			streamed = ev.Error
		}
		if ev.Usage.Neurons > total {
			total = ev.Usage.Neurons
		}
		for _, out := range ev.Output {
			if out.Answer != "" {
				answer = out.Answer
			}
		}
	}
	c.microNeurons.Add(int64(total * 1e6))
	if err := sc.Err(); err != nil {
		return "", retryable{fmt.Errorf("read stream: %w", err)}
	}
	if streamed != "" {
		return "", retryable{fmt.Errorf("workers-ai: %s", streamed)}
	}
	if answer == "" {
		return "", retryable{fmt.Errorf("workers-ai returned no answer")}
	}
	c.succeededOnce.Store(true)
	return answer, nil
}

func (c *moondreamClient) askYesNo(ctx context.Context, dataURI, question string) (bool, error) {
	var lastErr error
	for attempt := 0; attempt < 5; attempt++ {
		answer, err := c.ask(ctx, dataURI, question)
		if err != nil {
			if _, retry := err.(retryable); !retry {
				return false, err
			}
			lastErr = err
		} else {
			switch normalizeYesNo(answer) {
			case "yes":
				return true, nil
			case "no":
				return false, nil
			}
			// moondream is not deterministic: an answer that is neither yes nor no
			// parses on a later attempt rather than never.
			lastErr = fmt.Errorf("unparsable answer %q", truncate(answer, 60))
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	return false, lastErr
}

func normalizeYesNo(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, ".,!\"' \t")
	switch {
	case strings.HasPrefix(s, "yes"):
		return "yes"
	// Two images in the full-corpus run answered the nude question with the
	// question's own words ("partially nude") on every attempt at every
	// concurrency — not jitter. Left unparsed they fail the run, which under the
	// nightly cron means a false alert every night; answering with a partial
	// degree of the asked attribute is a yes.
	case strings.HasPrefix(s, "partial"):
		return "yes"
	case strings.HasPrefix(s, "no"):
		return "no"
	}
	return ""
}

func dataURI(mime string, body []byte) string {
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body)
}
