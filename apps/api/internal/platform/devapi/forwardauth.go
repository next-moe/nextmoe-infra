package devapi

import (
	"context"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"

	"github.com/gofiber/fiber/v3"
)

// Unregistered faces must not reach Record: a string longer than varchar(40)
// poisons every later flush batch (model.go Face comment, 06a→06b W1).
//
// Face → static pathLabel. No scope: these free read-only faces take any valid
// key — the first wiring shipped with a per-face scope and the 2026-09-08
// smoke run was 403'd by it, which is when the ruling landed that the gate's
// job here is identity and metering, not authorization (08 §16.5).
var forwardAuthFaces = map[string]string{
	"moyu":    "/v2/moyu/*",
	"sticker": "/v2/sticker/*",
}

type ForwardAuth struct {
	mw    *Middleware
	usage *UsageRecorder
}

func NewForwardAuth(mw *Middleware, usage *UsageRecorder) *ForwardAuth {
	return &ForwardAuth{mw: mw, usage: usage}
}

func (f *ForwardAuth) Handle(c fiber.Ctx) error {
	name := c.Query("face")
	pathLabel, ok := forwardAuthFaces[name]
	if !ok {
		slog.Error("unregistered forward-auth face", "face", name)
		return refuseForward(c, problem.CodeInternalError, "unregistered forward-auth face.")
	}

	raw := extractKey(c)
	if raw == "" && c.Get("Authorization") == "" && c.Get("X-API-Key") == "" {
		return refuseForward(c, problem.CodeMissingCredential,
			"An application key is required: send Authorization: Bearer nmk_live_….")
	}
	if IsV2KeyPrefix(raw) {
		if !ValidV2Key(raw) {
			return refuseForward(c, problem.CodeInvalidCredential, "The application key is invalid.")
		}
	} else if !HasV1KeyPrefix(raw) {
		return refuseForward(c, problem.CodeInvalidCredential, "The application key is invalid.")
	}

	cred, err := f.mw.resolve(c.Context(), raw)
	if err != nil {
		slog.Error("devapi credential resolve failed", "err", err)
		return refuseForward(c, problem.CodeServiceUnavailable, "credential store is unavailable.")
	}
	if cred == nil {
		return refuseForward(c, problem.CodeInvalidCredential, "The application key is invalid, revoked or expired.")
	}

	now := time.Now()
	limit, remaining, reset, allowed, failOpen := f.mw.rateResult(c.Context(), cred, now)
	if failOpen {
		slog.Warn("devapi rate-limit store unavailable; failing open", "key_id", cred.KeyID)
	} else {
		if limit > 0 {
			c.Set("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			c.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		}
		if !allowed {
			c.Set("Retry-After", strconv.FormatInt(max(reset-now.UTC().Unix(), 1), 10))
			f.usage.Record(cred, name, pathLabel, fiber.StatusTooManyRequests)
			return refuseForward(c, problem.CodeRateLimited, "Short-window rate limit exceeded.")
		}
	}

	qLimit, qRemaining, qAllowed, qFailOpen := f.mw.quotaResult(c.Context(), cred, now)
	if qFailOpen {
		slog.Warn("devapi quota store unavailable; failing open", "key_id", cred.KeyID)
	} else {
		if qLimit > 0 {
			c.Set("X-Quota-Limit", strconv.Itoa(qLimit))
			c.Set("X-Quota-Remaining", strconv.Itoa(qRemaining))
		}
		if !qAllowed {
			c.Set("Retry-After", strconv.FormatInt(max(nextDayStartUnix(now)-now.UTC().Unix(), 1), 10))
			f.usage.Record(cred, name, pathLabel, fiber.StatusTooManyRequests)
			return refuseForward(c, problem.CodeQuotaExceeded, "Daily quota exceeded.")
		}
	}

	c.Set("X-NextMoe-Client-Id", cred.ClientID)
	c.Set("X-NextMoe-Key-Id", strconv.FormatUint(uint64(cred.KeyID), 10))
	c.Set("X-NextMoe-Tier", cred.Tier)
	f.usage.Record(cred, name, pathLabel, fiber.StatusNoContent)
	go f.usage.TouchLastUsed(context.Background(), cred)
	return c.SendStatus(fiber.StatusNoContent)
}

// Traefik returns a refused forward-auth response to the caller verbatim,
// headers and body, so this is the error document a /v2/moyu or /v2/sticker
// client reads. Until 2026-09-18 it was the platform's {code, message}
// envelope while every other /v2 answer was problem+json.
func refuseForward(c fiber.Ctx, code, detail string) error {
	instance := ""
	if uri := c.Get("X-Forwarded-Uri"); strings.HasPrefix(uri, "/") {
		instance = uri
	}
	return problem.WriteFiberError(c, problem.New(code, problem.RequestID(c), instance, detail))
}
