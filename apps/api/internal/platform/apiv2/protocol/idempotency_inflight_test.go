package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"api/internal/platform/apiv2/problem"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

func doWait(t *testing.T, app *fiber.App, method, path string, hdr map[string]string, body string) (int, http.Header, []byte) {
	t.Helper()
	var rdr io.Reader
	if body != "" {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if body != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := app.Test(req, fiber.TestConfig{Timeout: 5 * time.Second})
	require.NoError(t, err)
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, resp.Header, b
}

type countingStore struct {
	*Memory
	setNX atomic.Int64
}

func (s *countingStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.setNX.Add(1)
	return s.Memory.SetNX(ctx, key, value, ttl)
}

type captureStore struct {
	*Memory
	mu  sync.Mutex
	key string
}

func (s *captureStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	s.key = key
	s.mu.Unlock()
	return s.Memory.SetNX(ctx, key, value, ttl)
}

func (s *captureStore) lastKey() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.key
}

func TestIdempotencyInFlightBlocksOverlap(t *testing.T) {
	store := NewMemory()
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		n := runs.Add(1)
		if n == 1 {
			close(started)
			<-release
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true, "n": n})
	})

	hdr := map[string]string{"Idempotency-Key": "k-overlap"}
	body := `{"a":1}`
	var firstStatus int
	var firstBody []byte
	done := make(chan struct{})
	go func() {
		firstStatus, _, firstBody = doWait(t, app, http.MethodPost, "/v2/probe", hdr, body)
		close(done)
	}()
	<-started
	status, h, raw := doWait(t, app, http.MethodPost, "/v2/probe", hdr, body)
	require.Equal(t, 409, status)
	require.Contains(t, h.Get("Content-Type"), "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, problem.CodeIdempotencyRequestInProgress, p.Code)
	require.EqualValues(t, 1, runs.Load())
	close(release)
	<-done
	require.Equal(t, 201, firstStatus)

	status, h, raw = doWait(t, app, http.MethodPost, "/v2/probe", hdr, body)
	require.Equal(t, 201, status)
	require.Equal(t, "true", h.Get("Idempotency-Replayed"))
	require.Equal(t, firstBody, raw)
	require.EqualValues(t, 1, runs.Load())
}

func TestIdempotencyInFlightDifferentBodyIsReused(t *testing.T) {
	store := NewMemory()
	started := make(chan struct{})
	release := make(chan struct{})
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		n := runs.Add(1)
		if n == 1 {
			close(started)
			<-release
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": "k-mismatch"}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	}()
	<-started
	status, _, raw := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":2}`)
	require.Equal(t, 409, status)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, problem.CodeIdempotencyKeyReused, p.Code)
	require.EqualValues(t, 1, runs.Load())
	close(release)
	<-done
}

func TestIdempotency5xxReleasesKey(t *testing.T) {
	store := NewMemory()
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		n := runs.Add(1)
		if n == 1 {
			return c.SendStatus(fiber.StatusInternalServerError)
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": "k-5xx"}
	status, _, _ := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 500, status)
	status, h, _ := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 201, status)
	require.Empty(t, h.Get("Idempotency-Replayed"))
	require.EqualValues(t, 2, runs.Load())
}

func TestIdempotencyPanicReleasesKey(t *testing.T) {
	store := NewMemory()
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Use(func(c fiber.Ctx) error {
		defer func() {
			if recover() != nil {
				_ = problem.WriteFiberError(c, fiber.NewError(fiber.StatusInternalServerError, "panic"))
			}
		}()
		return c.Next()
	})
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		n := runs.Add(1)
		if n == 1 {
			panic("boom")
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": "k-panic"}
	status, _, _ := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 500, status)
	status, _, _ = doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 201, status)
	require.EqualValues(t, 2, runs.Load())
}

func TestIdempotency429ReleasesKey(t *testing.T) {
	store := NewMemory()
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		n := runs.Add(1)
		if n == 1 {
			return c.SendStatus(fiber.StatusTooManyRequests)
		}
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": "k-429"}
	status, _, _ := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 429, status)
	status, h, _ := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 201, status)
	require.Empty(t, h.Get("Idempotency-Replayed"))
	require.EqualValues(t, 2, runs.Load())
}

func TestIdempotencyLegacyRecordReplays(t *testing.T) {
	store := &captureStore{Memory: NewMemory()}
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		runs.Add(1)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": "k-legacy"}
	body := `{"a":1}`
	status, _, first := doWait(t, app, http.MethodPost, "/v2/probe", hdr, body)
	require.Equal(t, 201, status)
	require.EqualValues(t, 1, runs.Load())
	key := store.lastKey()
	require.NotEmpty(t, key)
	raw, err := store.Get(context.Background(), key)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"pending"`)
	legacy := []byte(`{"status":201,"content_type":"application/json","body":` + string(mustJSON(first)) + `,"hash":"` + bodyHash([]byte(body)) + `"}`)
	require.NoError(t, store.Set(context.Background(), key, legacy, time.Hour))
	status, h, second := doWait(t, app, http.MethodPost, "/v2/probe", hdr, body)
	require.Equal(t, 201, status)
	require.Equal(t, "true", h.Get("Idempotency-Replayed"))
	require.Equal(t, first, second)
	require.EqualValues(t, 1, runs.Load())
}

func TestIdempotencyOverlongKeyDoesNotTouchStore(t *testing.T) {
	store := &countingStore{Memory: NewMemory()}
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		runs.Add(1)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	hdr := map[string]string{"Idempotency-Key": strings.Repeat("k", 256)}
	status, h, raw := doWait(t, app, http.MethodPost, "/v2/probe", hdr, `{"a":1}`)
	require.Equal(t, 400, status)
	require.Contains(t, h.Get("Content-Type"), "application/problem+json")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, problem.CodeInvalidParameter, p.Code)
	require.Len(t, p.Errors, 1)
	require.Equal(t, "Idempotency-Key", p.Errors[0].Header)
	require.Equal(t, problem.ReasonTooLong, p.Errors[0].Reason)
	require.NotNil(t, p.Errors[0].Params)
	require.NotNil(t, p.Errors[0].Params.MaxLength)
	require.Equal(t, 255, *p.Errors[0].Params.MaxLength)
	require.EqualValues(t, 0, store.setNX.Load())
	require.EqualValues(t, 0, runs.Load())
}

func TestIdempotencySetNXErrorFailsOpen(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(&brokenStore{}))
	app.Use(Idempotency(&brokenStore{}, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		runs.Add(1)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	status, _, _ := doWait(t, app, http.MethodPost, "/v2/probe", map[string]string{"Idempotency-Key": "k-open"}, `{"a":1}`)
	require.Equal(t, 201, status)
	require.EqualValues(t, 1, runs.Load())
	require.Contains(t, buf.String(), "failing open")
	require.Contains(t, buf.String(), "request_id")
}

func TestIdempotencyLostClaimIsInProgress(t *testing.T) {
	store := &lostClaimStore{Memory: NewMemory()}
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		runs.Add(1)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	status, _, raw := doWait(t, app, http.MethodPost, "/v2/probe", map[string]string{"Idempotency-Key": "k-lost"}, `{"a":1}`)
	require.Equal(t, 409, status)
	var p problem.Problem
	require.NoError(t, json.Unmarshal(raw, &p))
	require.Equal(t, problem.CodeIdempotencyRequestInProgress, p.Code)
	require.EqualValues(t, 0, runs.Load())
}

func TestIdempotencyUndecodableRecordRunsHandler(t *testing.T) {
	store := &badRecordStore{Memory: NewMemory()}
	var runs atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, nil))
	app.Post("/v2/probe", func(c fiber.Ctx) error {
		runs.Add(1)
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{"ok": true})
	})
	status, _, _ := doWait(t, app, http.MethodPost, "/v2/probe", map[string]string{"Idempotency-Key": "k-bad"}, `{"a":1}`)
	require.Equal(t, 201, status)
	require.EqualValues(t, 1, runs.Load())
}

type lostClaimStore struct{ *Memory }

func (s *lostClaimStore) SetNX(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, nil
}
func (s *lostClaimStore) Get(context.Context, string) ([]byte, error) { return nil, nil }

type badRecordStore struct{ *Memory }

func (s *badRecordStore) SetNX(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, nil
}
func (s *badRecordStore) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.HasPrefix(key, "v2:idem:") {
		return []byte("not-json"), nil
	}
	return s.Memory.Get(ctx, key)
}

func mustJSON(v []byte) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
