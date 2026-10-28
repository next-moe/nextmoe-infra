package service

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

var ErrInsufficientPoints = errors.New("agent: not enough moemoepoints")

type LedgerClient struct {
	base, clientID, secret string
	http                   *http.Client
}

func NewLedgerClient(base, clientID, secret string) *LedgerClient {
	if base == "" || clientID == "" || secret == "" {
		return nil
	}
	return &LedgerClient{base: strings.TrimRight(base, "/"), clientID: clientID, secret: secret, http: &http.Client{Timeout: 10 * time.Second}}
}

func (l *LedgerClient) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, l.base+path, rd)
	if err != nil {
		return 0, err
	}
	req.SetBasicAuth(l.clientID, l.secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := l.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("ledger %s %s: http %d: %s", method, path, resp.StatusCode, truncate(string(raw), 200))
	}
	if out != nil {
		return resp.StatusCode, json.Unmarshal(raw, out)
	}
	return resp.StatusCode, nil
}

func (l *LedgerClient) Balance(ctx context.Context, userID int64) (int64, error) {
	var out struct {
		Data struct {
			Balance int64 `json:"balance"`
		} `json:"data"`
		Balance *int64 `json:"balance"`
	}
	if _, err := l.do(ctx, http.MethodGet, fmt.Sprintf("/api/v1/users/%d/moemoepoint", userID), nil, &out); err != nil {
		return 0, err
	}
	if out.Balance != nil {
		return *out.Balance, nil
	}
	return out.Data.Balance, nil
}

func (l *LedgerClient) Charge(ctx context.Context, userID int64, amount int, runID, note string) error {
	key := "agent:run:" + runID
	status, err := l.do(ctx, http.MethodPost, fmt.Sprintf("/api/v1/users/%d/moemoepoint/charges", userID),
		map[string]any{"amount": amount, "idempotency_key": key, "ref": key, "note": note}, nil)
	if err != nil && status == http.StatusBadRequest && strings.Contains(err.Error(), `"code":16006`) {
		return fmt.Errorf("%w: %v", ErrInsufficientPoints, err)
	}
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
