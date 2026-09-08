package devapi

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

type forwardAuthFace struct {
	scope     string
	pathLabel string
}

// Unregistered faces must not reach Record: a string longer than varchar(40)
// poisons every later flush batch (model.go Face comment, 06a→06b W1).
var forwardAuthFaces = map[string]forwardAuthFace{
	"moyu":    {scope: ScopeMoyuRead, pathLabel: "/v1/moyu/*"},
	"sticker": {scope: ScopeStickerRead, pathLabel: "/v1/sticker/*"},
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
	face, ok := forwardAuthFaces[name]
	if !ok {
		slog.Error("unregistered forward-auth face", "face", name)
		return response.Error(c, fiber.StatusInternalServerError, errors.ErrInternalServer, "unregistered forward-auth face")
	}

	raw := extractKey(c)
	if IsV2KeyPrefix(raw) {
		if !ValidV2Key(raw) {
			return resp401(c)
		}
	} else if !HasV1KeyPrefix(raw) {
		return resp401(c)
	}

	cred, err := f.mw.resolve(c.Context(), raw)
	if err != nil {
		slog.Error("devapi credential resolve failed", "err", err)
		return response.Error(c, fiber.StatusServiceUnavailable, errors.ErrInternalServer, "credential store unavailable")
	}
	if cred == nil {
		return resp401(c)
	}

	if !cred.HasScope(face.scope) {
		f.usage.Record(cred, name, face.pathLabel, fiber.StatusForbidden)
		return response.ForbiddenMsg(c, errors.ErrForbidden, "missing required scope: "+face.scope)
	}

	limit, remaining, reset, allowed, failOpen := f.mw.rateResult(c.Context(), cred, time.Now())
	if failOpen {
		slog.Warn("devapi rate-limit store unavailable; failing open", "key_id", cred.KeyID)
	} else {
		if limit > 0 {
			c.Set("X-RateLimit-Limit", strconv.Itoa(limit))
			c.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			c.Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		}
		if !allowed {
			retry := reset - time.Now().UTC().Unix()
			if retry < 1 {
				retry = 1
			}
			c.Set("Retry-After", strconv.FormatInt(retry, 10))
			f.usage.Record(cred, name, face.pathLabel, fiber.StatusTooManyRequests)
			return resp429(c)
		}
	}

	qLimit, qRemaining, qAllowed, qFailOpen := f.mw.quotaResult(c.Context(), cred, time.Now())
	if qFailOpen {
		slog.Warn("devapi quota store unavailable; failing open", "key_id", cred.KeyID)
	} else {
		if qLimit > 0 {
			c.Set("X-Quota-Limit", strconv.Itoa(qLimit))
			c.Set("X-Quota-Remaining", strconv.Itoa(qRemaining))
		}
		if !qAllowed {
			f.usage.Record(cred, name, face.pathLabel, fiber.StatusTooManyRequests)
			return resp429(c)
		}
	}

	c.Set("X-NextMoe-Client-Id", cred.ClientID)
	c.Set("X-NextMoe-Key-Id", strconv.FormatUint(uint64(cred.KeyID), 10))
	c.Set("X-NextMoe-Tier", cred.Tier)
	f.usage.Record(cred, name, face.pathLabel, fiber.StatusNoContent)
	go f.usage.TouchLastUsed(context.Background(), cred)
	return c.SendStatus(fiber.StatusNoContent)
}
