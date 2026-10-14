package devapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"api/internal/platform/settings/keys"
	"api/pkg/errors"
	"api/pkg/response"

	"github.com/gofiber/fiber/v3"
)

const credLocalsKey = "devapi_cred"

const credCacheNeg = byte('-')

type Middleware struct {
	repo  *Repository
	store Store
}

func NewMiddleware(repo *Repository, store Store) *Middleware {
	return &Middleware{repo: repo, store: store}
}

func CredentialFrom(c fiber.Ctx) *Credential {
	cred, _ := c.Locals(credLocalsKey).(*Credential)
	return cred
}

func WithCredential(c fiber.Ctx, cred *Credential) {
	c.Locals(credLocalsKey, cred)
}

func (m *Middleware) Lookup(ctx context.Context, raw string) (*Credential, error) {
	if IsV2KeyPrefix(raw) {
		if !ValidV2Key(raw) {
			return nil, nil
		}
	} else if !HasV1KeyPrefix(raw) {
		return nil, nil
	}
	return m.resolve(ctx, raw)
}

func credCacheKey(hashHex string) string { return "devkey:" + hashHex }

// The cached credential carries the APP's dev_enabled, tier and limits, not
// only the key's own row, so every writer of those columns is a writer of this
// cache. RevokeKey has busted its own entry since the beginning; disabling an
// app did not, and its keys went on resolving for the 60s TTL.
func credCacheKeyForStoredHash(storedHash string) (string, bool) {
	hexPart, ok := strings.CutPrefix(storedHash, keyHashPrefix)
	if !ok {
		return "", false
	}
	return credCacheKey(hexPart), true
}

func (m *Middleware) resolve(ctx context.Context, raw string) (*Credential, error) {
	cacheKey := credCacheKey(hashHex(raw))
	if b, _ := m.store.Get(ctx, cacheKey); len(b) > 0 {
		if len(b) == 1 && b[0] == credCacheNeg {
			return nil, nil
		}
		var cred Credential
		if json.Unmarshal(b, &cred) == nil {
			return &cred, nil
		}
	}

	cred, err := m.repo.ResolveByHash(ctx, HashKey(raw), time.Now())
	if err != nil {
		return nil, err
	}
	if cred == nil {
		_ = m.store.Set(ctx, cacheKey, []byte{credCacheNeg}, time.Duration(keys.DeveloperCredentialCacheNegativeTTLSeconds.Get())*time.Second)
		return nil, nil
	}
	if b, err := json.Marshal(cred); err == nil {
		_ = m.store.Set(ctx, cacheKey, b, time.Duration(keys.DeveloperCredentialCacheTTLSeconds.Get())*time.Second)
	}
	return cred, nil
}

func (m *Middleware) RateLimit(c fiber.Ctx) error {
	cred := CredentialFrom(c)
	if cred == nil {
		return resp401(c)
	}
	limit, remaining, reset, allowed, failOpen := m.rateResult(c.Context(), cred, time.Now())
	if failOpen {
		slog.Warn("devapi rate-limit store unavailable; failing open", "key_id", cred.KeyID)
		return c.Next()
	}
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
		return resp429(c)
	}
	return c.Next()
}

func (m *Middleware) rateResult(ctx context.Context, cred *Credential, now time.Time) (limit, remaining int, reset int64, allowed, failOpen bool) {
	lim, unlimited := cred.EffectiveRate()
	if unlimited {
		return 0, 0, 0, true, false
	}
	minute := now.UTC().Unix() / 60
	key := fmt.Sprintf("ratelimit:%d:%d", cred.KeyID, minute)
	n, err := m.store.Incr(ctx, key, 65*time.Second)
	if err != nil {
		return 0, 0, 0, false, true
	}
	reset = (minute + 1) * 60
	remaining = lim - int(n)
	if remaining < 0 {
		remaining = 0
	}
	return lim, remaining, reset, int(n) <= lim, false
}

func (m *Middleware) quotaResult(ctx context.Context, cred *Credential, now time.Time) (limit, remaining int, allowed, failOpen bool) {
	lim, unlimited := cred.EffectiveQuota()
	if unlimited {
		return 0, 0, true, false
	}
	utc := now.UTC()
	key := quotaCounterKey(cred.KeyID, utc.Format("2006-01-02"))
	n, err := m.store.Incr(ctx, key, ttlUntilNextDay(utc))
	if err != nil {
		return 0, 0, false, true
	}
	remaining = lim - int(n)
	if remaining < 0 {
		remaining = 0
	}
	return lim, remaining, int(n) <= lim, false
}

func RequireScope(scope string) fiber.Handler {
	return func(c fiber.Ctx) error {
		cred := CredentialFrom(c)
		if cred == nil {
			return resp401(c)
		}
		if !cred.HasScope(scope) {
			return response.ForbiddenMsg(c, errors.ErrForbidden, "missing required scope: "+scope)
		}
		return c.Next()
	}
}

func RequireTier(tier string) fiber.Handler {
	return func(c fiber.Ctx) error {
		cred := CredentialFrom(c)
		if cred == nil {
			return resp401(c)
		}
		if cred.Tier != tier {
			return response.ForbiddenMsg(c, errors.ErrForbidden, "requires "+tier+" tier")
		}
		return c.Next()
	}
}

func extractKey(c fiber.Ctx) string {
	if h := c.Get("Authorization"); h != "" {
		if v, ok := strings.CutPrefix(h, "Bearer "); ok {
			if v = strings.TrimSpace(v); HasKeyPrefix(v) {
				return v
			}
		}
	}
	return strings.TrimSpace(c.Get("X-API-Key"))
}

func quotaCounterKey(keyID uint, day string) string {
	return fmt.Sprintf("quota:%d:%s", keyID, day)
}

func nextDayStartUnix(now time.Time) int64 {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1).Unix()
}

func ttlUntilNextDay(now time.Time) time.Duration {
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	return next.Sub(now) + time.Minute
}

func resp401(c fiber.Ctx) error {
	return response.Unauthorized(c, errors.ErrAuthUnauthorized)
}

func resp429(c fiber.Ctx) error {
	return response.TooManyRequests(c)
}
