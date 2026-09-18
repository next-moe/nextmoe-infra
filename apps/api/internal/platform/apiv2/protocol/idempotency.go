package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/pkg/routepath"

	"github.com/gofiber/fiber/v3"
)

const (
	idempotencyTTL        = 24 * time.Hour
	idempotencyPendingTTL = 2 * time.Minute
	idempotencyKeyMaxLen  = 255
)

type idempotencyRecord struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	Body        []byte `json:"body"`
	Hash        string `json:"hash"`
	// Pending marks an in-flight claim. A record without the field is a
	// completed record: records written before this deploy have no pending
	// key and live for 24h, and a done-style positive flag would decode them
	// as pending and answer 409 to every retry for a day.
	Pending bool `json:"pending,omitempty"`
}

// Idempotency must be registered AFTER the auth middlewares. The key was
// c.IP() + method + path + Idempotency-Key while this lived in Middleware
// (pre-auth), and a first-party backend relays every one of its users from one
// egress IP — so two different users sending the same Idempotency-Key on the
// same path replayed each other's writes.
func Idempotency(store Store, ident IdentityFunc) fiber.Handler {
	return func(c fiber.Ctx) error {
		if store == nil || c.Method() != fiber.MethodPost || !strings.HasPrefix(routepath.Normalize(c.Path()), "/v2") {
			return c.Next()
		}
		rawKey := c.Get("Idempotency-Key")
		if rawKey == "" {
			return c.Next()
		}
		if len(rawKey) > idempotencyKeyMaxLen {
			p := problem.New(problem.CodeInvalidParameter, problem.RequestID(c), problem.Instance(c),
				"Idempotency-Key is at most 255 bytes.")
			p.Errors = []problem.FieldError{{
				Header: "Idempotency-Key",
				Reason: problem.ReasonTooLong,
				Detail: "at most 255 bytes",
				Params: &problem.FieldParams{MaxLength: problem.Ptr(idempotencyKeyMaxLen)},
			}}
			return writeErr(c, p)
		}
		key := idempotencyKey(c, ident)
		h := bodyHash(c.Body())
		pending, err := json.Marshal(idempotencyRecord{Pending: true, Hash: h})
		if err != nil {
			slog.Warn("v2 idempotency store unavailable; failing open", "request_id", problem.RequestID(c), "err", err)
			return c.Next()
		}
		claimed, err := store.SetNX(c.Context(), key, pending, idempotencyPendingTTL)
		if err != nil {
			slog.Warn("v2 idempotency store unavailable; failing open", "request_id", problem.RequestID(c), "err", err)
			return c.Next()
		}
		if !claimed {
			return replayOrConflict(store, c, key, h)
		}
		stored := false
		defer func() {
			if !stored {
				_ = store.Del(c.Context(), key)
			}
		}()
		err = c.Next()
		stored = rememberPOST(store, c, key)
		return err
	}
}

func idempotencyKey(c fiber.Ctx, ident IdentityFunc) string {
	who := "ip:" + clientIP(c)
	if ident != nil {
		if id, ok := ident(c); ok && id.Key != "" {
			who = id.Key
		}
	}
	return "v2:idem:" + who + ":" + c.Method() + ":" + routepath.Normalize(c.Path()) + ":" + c.Get("Idempotency-Key")
}

func bodyHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func replayOrConflict(store Store, c fiber.Ctx, key, hash string) error {
	raw, err := store.Get(c.Context(), key)
	if err != nil || len(raw) == 0 {
		if err != nil {
			slog.Warn("v2 idempotency store unavailable; failing open", "request_id", problem.RequestID(c), "err", err)
			return c.Next()
		}
		p := problem.New(problem.CodeIdempotencyRequestInProgress, problem.RequestID(c), problem.Instance(c),
			"A request with the same Idempotency-Key is still being processed. Retry after it completes.")
		return writeErr(c, p)
	}
	var rec idempotencyRecord
	if json.Unmarshal(raw, &rec) != nil {
		err := c.Next()
		rememberPOST(store, c, key)
		return err
	}
	if hash != rec.Hash {
		p := problem.New(problem.CodeIdempotencyKeyReused, problem.RequestID(c), problem.Instance(c),
			"Idempotency-Key was reused with a different request body.")
		return writeErr(c, p)
	}
	if rec.Pending {
		p := problem.New(problem.CodeIdempotencyRequestInProgress, problem.RequestID(c), problem.Instance(c),
			"A request with the same Idempotency-Key is still being processed. Retry after it completes.")
		return writeErr(c, p)
	}
	c.Set("Idempotency-Replayed", "true")
	if rec.ContentType != "" {
		c.Set("Content-Type", rec.ContentType)
	}
	c.Status(rec.Status)
	return c.Send(rec.Body)
}

func rememberPOST(store Store, c fiber.Ctx, key string) bool {
	status := c.Response().StatusCode()
	// 429 became reachable here when the limiter moved inside the chain
	// (RateLimit runs within c.Next()); remembering one would replay the
	// rate-limit refusal for 24h after the window reset.
	if status < 200 || status >= 500 || status == fiber.StatusTooManyRequests {
		return false
	}
	rec := idempotencyRecord{
		Status:      status,
		ContentType: string(c.Response().Header.ContentType()),
		Body:        append([]byte(nil), c.Response().Body()...),
		Hash:        bodyHash(c.Body()),
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return false
	}
	if err := store.Set(c.Context(), key, b, idempotencyTTL); err != nil {
		return false
	}
	return true
}
