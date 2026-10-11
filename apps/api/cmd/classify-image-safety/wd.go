package main

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

type wdClient struct {
	base string
	http *http.Client
}

type wdScores struct {
	Model  string             `json:"model"`
	Rating map[string]float64 `json:"rating"`
	Tags   map[string]float64 `json:"tags"`
}

var errUndecodable = errors.New("the tagger could not decode the image")

func newWDClient(base string) *wdClient {
	return &wdClient{base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 2 * time.Minute}}
}

func (c *wdClient) waitReady(ctx context.Context, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
		if err != nil {
			return "", err
		}
		resp, err := c.http.Do(req)
		if err == nil {
			var body struct {
				Model string `json:"model"`
			}
			decodeErr := json.NewDecoder(resp.Body).Decode(&body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK && decodeErr == nil && body.Model != "" {
				return body.Model, nil
			}
			err = fmt.Errorf("healthz: http %d", resp.StatusCode)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("tagger at %s not ready after %s: %w", c.base, timeout, err)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *wdClient) tag(ctx context.Context, image []byte) (wdScores, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return wdScores{}, ctx.Err()
			case <-time.After(time.Duration(attempt) * 2 * time.Second):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/v1/tag", bytes.NewReader(image))
		if err != nil {
			return wdScores{}, err
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		switch {
		case readErr != nil:
			lastErr = readErr
		case resp.StatusCode == http.StatusUnprocessableEntity:
			return wdScores{}, fmt.Errorf("%w: %s", errUndecodable, truncate(string(body), 200))
		case resp.StatusCode != http.StatusOK:
			lastErr = fmt.Errorf("tag: http %d: %s", resp.StatusCode, truncate(string(body), 200))
			if resp.StatusCode < 500 {
				return wdScores{}, lastErr
			}
		default:
			var s wdScores
			if err := json.Unmarshal(body, &s); err != nil {
				return wdScores{}, fmt.Errorf("decode tag response: %w", err)
			}
			return s, nil
		}
	}
	return wdScores{}, lastErr
}

var (
	wdRatingLevels = []string{"general", "sensitive", "questionable", "explicit"}
	wdActTags      = []string{"sex", "penis", "pussy", "fellatio", "vaginal", "anal", "cum", "paizuri"}
	wdNudeTags     = []string{"nipples", "areola_slip", "nude", "completely_nude", "topless", "bottomless"}
	wdCensorTags   = []string{"mosaic_censoring", "bar_censor"}
)

// The rating's argmax is the grade, raised by three tag families. The cut-offs
// were set on 2026-10-11 against 2,337 images with a human verdict (VNDB's
// screenshot and cover votes plus every hand regrade) and only hold for the
// weights pinned in docker/wdtagger.Dockerfile; the censor rule is what catches
// the mosaic-covered explicit art that moondream graded safe.
func wdLevel(s wdScores) (int, error) {
	level, best := -1, -1.0
	for i, name := range wdRatingLevels {
		p, ok := s.Rating[name]
		if !ok {
			return 0, fmt.Errorf("tagger response has no %q rating", name)
		}
		if p > best {
			level, best = i, p
		}
	}
	peak := func(names []string) (float64, error) {
		m := 0.0
		for _, name := range names {
			p, ok := s.Tags[name]
			if !ok {
				return 0, fmt.Errorf("tagger response has no %q tag", name)
			}
			m = max(m, p)
		}
		return m, nil
	}
	act, err := peak(wdActTags)
	if err != nil {
		return 0, err
	}
	nude, err := peak(wdNudeTags)
	if err != nil {
		return 0, err
	}
	censor, err := peak(wdCensorTags)
	if err != nil {
		return 0, err
	}
	switch {
	case act >= 0.35 || censor >= 0.5:
		return 3, nil
	case nude >= 0.35:
		return max(level, 2), nil
	}
	return level, nil
}
