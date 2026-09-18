package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/settings/keys"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/require"
)

// gatedApp mirrors the production chain: Middleware, then an auth stack that
// can refuse before RateLimit ever runs, then RateLimit.
func gatedApp(t *testing.T, store Store) *fiber.App {
	t.Helper()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(func(c fiber.Ctx) error {
		if c.Get("Authorization") == "Bearer good" {
			return c.Next()
		}
		return problem.WriteFiberError(c, problem.New(problem.CodeInvalidCredential,
			problem.RequestID(c), problem.Instance(c), "no."))
	})
	app.Use(RateLimit(store, func(fiber.Ctx) (LimitIdentity, bool) {
		return LimitIdentity{Key: "k1", Rate: 100000, Quota: 100000}, true
	}))
	app.Get("/v2/catalog/works", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"object": "list"})
	})
	return app
}

func TestAuthRejectionsAreRateLimited(t *testing.T) {
	store := NewMemory()
	app := gatedApp(t, store)

	good := map[string]string{"Authorization": "Bearer good"}
	status, _, _ := do(t, app, http.MethodGet, "/v2/catalog/works", good, "")
	require.Equal(t, 200, status, "positive control: an authorized caller is served")

	for i := 0; i < int(keys.APIV2AuthFailPerMinute.Get())-1; i++ {
		status, _, _ := do(t, app, http.MethodGet, "/v2/catalog/works", nil, "")
		require.Equal(t, 401, status, "below the budget an auth failure stays an auth failure")
	}
	status, _, _ = do(t, app, http.MethodGet, "/v2/catalog/works", good, "")
	require.Equal(t, 200, status, "positive control: still served while under budget")

	status, _, _ = do(t, app, http.MethodGet, "/v2/catalog/works", nil, "")
	require.Equal(t, 401, status, "the budget-crossing failure is still answered as one")

	status, h, body := do(t, app, http.MethodGet, "/v2/catalog/works", nil, "")
	require.Equal(t, 429, status)
	require.NotEmpty(t, h.Get("Retry-After"))
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeRateLimited, p.Code)
}

// The quota limiter already counts everything that gets past auth, so a 403 a
// handler reached must not also spend the anti-abuse budget.
func TestAuthorizedRefusalsDoNotSpendTheAuthFailureBudget(t *testing.T) {
	store := NewMemory()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(RateLimit(store, func(fiber.Ctx) (LimitIdentity, bool) {
		return LimitIdentity{Key: "k2", Rate: 100000, Quota: 100000}, true
	}))
	app.Get("/v2/catalog/works", func(c fiber.Ctx) error {
		return problem.WriteFiberError(c, problem.New(problem.CodePermissionRequired,
			problem.RequestID(c), problem.Instance(c), "nope."))
	})
	for i := 0; i < int(keys.APIV2AuthFailPerMinute.Get())+5; i++ {
		status, _, _ := do(t, app, http.MethodGet, "/v2/catalog/works", nil, "")
		require.Equal(t, 403, status)
	}
}

type brokenStore struct{ calls atomic.Int64 }

func (s *brokenStore) Incr(context.Context, string, time.Duration) (int64, error) {
	s.calls.Add(1)
	return 0, errUnavailable
}
func (s *brokenStore) Decr(context.Context, string) error { return errUnavailable }
func (s *brokenStore) Get(context.Context, string) ([]byte, error) {
	return nil, errUnavailable
}
func (s *brokenStore) Set(context.Context, string, []byte, time.Duration) error {
	return errUnavailable
}
func (s *brokenStore) SetNX(context.Context, string, []byte, time.Duration) (bool, error) {
	return false, errUnavailable
}
func (s *brokenStore) Del(context.Context, string) error { return errUnavailable }

func TestLimiterFailsOpenAndSaysSo(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	degradedMu.Lock()
	degradedAt, degradedSkip = time.Time{}, 0
	degradedMu.Unlock()

	store := &brokenStore{}
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(RateLimit(store, func(fiber.Ctx) (LimitIdentity, bool) {
		return LimitIdentity{Key: "k3", Rate: 1, Quota: 1}, true
	}))
	app.Get("/v2/catalog/works", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"object": "list"})
	})

	for i := 0; i < 3; i++ {
		status, _, _ := do(t, app, http.MethodGet, "/v2/catalog/works", nil, "")
		require.Equal(t, 200, status, "a dead store must not take the API down with it")
	}
	require.Positive(t, store.calls.Load(), "positive control: the limiter did try the store")
	require.Contains(t, buf.String(), "failing open", "a silent fail-open is the defect")
}

func TestNotModifiedStillSpendsQuota(t *testing.T) {
	store := NewMemory()
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(RateLimit(store, func(fiber.Ctx) (LimitIdentity, bool) {
		return LimitIdentity{Key: "k4", Rate: 100000, Quota: 3}, true
	}))
	app.Get("/v2/problems", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"object": "list", "items": []any{}})
	})

	_, h, _ := do(t, app, http.MethodGet, "/v2/problems", nil, "")
	tag := h.Get("ETag")
	require.NotEmpty(t, tag)

	cond := map[string]string{"If-None-Match": tag}
	for i := 0; i < 2; i++ {
		status, _, _ := do(t, app, http.MethodGet, "/v2/problems", cond, "")
		require.Equal(t, 304, status)
	}
	status, _, body := do(t, app, http.MethodGet, "/v2/problems", cond, "")
	require.Equal(t, 429, status, "three answers spent a quota of three, 304 or not")
	var p problem.Problem
	require.NoError(t, json.Unmarshal(body, &p))
	require.Equal(t, problem.CodeQuotaExceeded, p.Code)
}

func TestIdempotencyIsScopedToTheCaller(t *testing.T) {
	store := NewMemory()
	var seq atomic.Int64
	app := fiber.New(fiber.Config{ErrorHandler: problem.WriteFiberError})
	app.Use(Middleware(store))
	app.Use(Idempotency(store, func(c fiber.Ctx) (LimitIdentity, bool) {
		who := c.Get("X-Test-User")
		if who == "" {
			return LimitIdentity{}, false
		}
		return LimitIdentity{Key: "u" + who}, true
	}))
	app.Post("/v2/me/claims", func(c fiber.Ctx) error {
		return c.Status(fiber.StatusCreated).JSON(fiber.Map{
			"object": "claim", "id": strconv.FormatInt(seq.Add(1), 10),
		})
	})

	hdrA := map[string]string{"Idempotency-Key": "k1", "X-Test-User": "7"}
	hdrB := map[string]string{"Idempotency-Key": "k1", "X-Test-User": "8"}

	status, _, first := do(t, app, http.MethodPost, "/v2/me/claims", hdrA, `{"work_id":"1"}`)
	require.Equal(t, 201, status)

	status, h, second := do(t, app, http.MethodPost, "/v2/me/claims", hdrB, `{"work_id":"1"}`)
	require.Equal(t, 201, status)
	require.Empty(t, h.Get("Idempotency-Replayed"),
		"one egress IP relays every user; a shared key must not replay another user's write")
	require.NotEqual(t, string(first), string(second))

	status, h, again := do(t, app, http.MethodPost, "/v2/me/claims", hdrA, `{"work_id":"1"}`)
	require.Equal(t, 201, status)
	require.Equal(t, "true", h.Get("Idempotency-Replayed"), "positive control: the same caller still replays")
	require.Equal(t, string(first), string(again))
}
