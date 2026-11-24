package fanin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"api/internal/canon/contract"
)

type Pool interface {
	Outbox(ctx context.Context, after int64, limit int) (contract.OutboxPage, error)
	Snapshot(ctx context.Context, hash string) (contract.Snapshot, error)
	Record(ctx context.Context, id string) (contract.Record, error)
	Answer(ctx context.Context, rows []contract.InboxRow) error
	Block(ctx context.Context, items []contract.BlockItem) error
	Refs(ctx context.Context, batch contract.RefBatch) error
}

type HTTPPool struct {
	Base   string
	Token  string
	Client *http.Client
}

func NewHTTPPool(base, token string) *HTTPPool {
	return &HTTPPool{Base: strings.TrimRight(base, "/"), Token: token, Client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *HTTPPool) Outbox(ctx context.Context, after int64, limit int) (contract.OutboxPage, error) {
	var out contract.OutboxPage
	q := url.Values{"after": {strconv.FormatInt(after, 10)}, "limit": {strconv.Itoa(limit)}}
	return out, p.do(ctx, http.MethodGet, contract.PathOutbox+"?"+q.Encode(), nil, &out)
}

func (p *HTTPPool) Snapshot(ctx context.Context, hash string) (contract.Snapshot, error) {
	var out contract.Snapshot
	return out, p.do(ctx, http.MethodGet, strings.Replace(contract.PathSnapshot, "{hash}", url.PathEscape(hash), 1), nil, &out)
}

func (p *HTTPPool) Record(ctx context.Context, id string) (contract.Record, error) {
	var out contract.Record
	return out, p.do(ctx, http.MethodGet, strings.Replace(contract.PathRecord, "{id}", url.PathEscape(id), 1), nil, &out)
}

func (p *HTTPPool) Answer(ctx context.Context, rows []contract.InboxRow) error {
	if len(rows) == 0 {
		return nil
	}
	return p.do(ctx, http.MethodPost, contract.PathInbox, contract.InboxBatch{Items: rows}, nil)
}

func (p *HTTPPool) Block(ctx context.Context, items []contract.BlockItem) error {
	if len(items) == 0 {
		return nil
	}
	return p.do(ctx, http.MethodPost, contract.PathBlock, contract.BlockBatch{Items: items}, nil)
}

func (p *HTTPPool) Refs(ctx context.Context, batch contract.RefBatch) error {
	return p.do(ctx, http.MethodPost, contract.PathRefs, batch, nil)
}

// ErrNotFound is the pool saying it has no such evidence; any other failure
// is the pool being unreachable, and the row waits for the next run.
var ErrNotFound = errors.New("not in the pool")

func (p *HTTPPool) do(ctx context.Context, method, path string, body, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, p.Base+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
