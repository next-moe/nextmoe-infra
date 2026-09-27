// Package realtime talks to Centrifugo: it mints the connection tokens
// clients connect with, and publishes chat events to each user's personal
// channel through Centrifugo's server API.
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/chat/service"

	"github.com/golang-jwt/jwt/v5"
)

const Namespace = "user"

func Channel(uid int64) string { return Namespace + ":" + strconv.FormatInt(uid, 10) }

type TokenIssuer struct {
	secret []byte
	ttl    time.Duration
}

func NewTokenIssuer(secret string, ttl time.Duration) *TokenIssuer {
	if secret == "" {
		return nil
	}
	return &TokenIssuer{secret: []byte(secret), ttl: ttl}
}

// The channels claim subscribes the connection on the server side: a client
// never names a channel, so it cannot name someone else's.
func (t *TokenIssuer) Issue(uid int64, now time.Time) (string, time.Time, error) {
	exp := now.Add(t.ttl)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub":      strconv.FormatInt(uid, 10),
		"iat":      now.Unix(),
		"exp":      exp.Unix(),
		"channels": []string{Channel(uid)},
	})
	signed, err := tok.SignedString(t.secret)
	return signed, exp, err
}

type Centrifugo struct {
	apiURL string
	apiKey string
	http   *http.Client
	queue  chan []service.Delivery
}

func NewCentrifugo(apiURL, apiKey string) *Centrifugo {
	if apiURL == "" || apiKey == "" {
		return nil
	}
	return &Centrifugo{
		apiURL: strings.TrimRight(apiURL, "/"), apiKey: apiKey,
		http:  &http.Client{Timeout: 5 * time.Second},
		queue: make(chan []service.Delivery, 1024),
	}
}

// One background sender keeps a user's events in commit order. A full queue
// drops the batch; clients recover it from their update stream.
func (c *Centrifugo) Publish(_ context.Context, deliveries []service.Delivery) {
	if len(deliveries) == 0 {
		return
	}
	select {
	case c.queue <- deliveries:
	default:
		slog.Warn("chat realtime queue full; dropping a batch", "deliveries", len(deliveries))
	}
}

func (c *Centrifugo) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case batch := <-c.queue:
			if err := c.send(ctx, batch); err != nil {
				slog.Warn("chat realtime publish failed", "deliveries", len(batch), "err", err)
			}
		}
	}
}

type publishCommand struct {
	Publish struct {
		Channel string `json:"channel"`
		Data    any    `json:"data"`
	} `json:"publish"`
}

func (c *Centrifugo) send(ctx context.Context, batch []service.Delivery) error {
	cmds := make([]publishCommand, len(batch))
	for i, d := range batch {
		cmds[i].Publish.Channel = Channel(d.UserID)
		cmds[i].Publish.Data = d.Data
	}
	body, err := json.Marshal(map[string]any{"commands": cmds, "parallel": true})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/batch", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var out struct {
		Error   *apiError `json:"error"`
		Replies []struct {
			Error *apiError `json:"error"`
		} `json:"replies"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return fmt.Errorf("decode reply: %w", err)
	}
	if out.Error != nil {
		return out.Error
	}
	failed := 0
	var first *apiError
	for _, r := range out.Replies {
		if r.Error != nil {
			failed++
			if first == nil {
				first = r.Error
			}
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d publishes failed, first: %w", failed, len(batch), first)
	}
	return nil
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *apiError) Error() string { return fmt.Sprintf("centrifugo error %d: %s", e.Code, e.Message) }
