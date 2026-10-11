package protocol

import (
	"strconv"
	"strings"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/settings/keys"
	"api/pkg/routepath"

	"github.com/gofiber/fiber/v3"
)

const localsPastAuth = "v2_past_auth"

type LimitIdentity struct {
	Key       string
	Rate      int
	Quota     int
	Unlimited bool
	Anonymous bool
}

type IdentityFunc func(c fiber.Ctx) (LimitIdentity, bool)

// RateLimit must be registered AFTER the auth middlewares: the counting
// decision needs the resolved credential, which does not exist yet when
// Middleware runs. Reaching this handler at all is therefore the proof that
// the auth stack let the request through, which is what localsPastAuth
// records for the auth-failure bucket.
func RateLimit(store Store, ident IdentityFunc) fiber.Handler {
	lim := newLimiter(store)
	return func(c fiber.Ctx) error {
		if !strings.HasPrefix(routepath.Normalize(c.Path()), "/v2") {
			return c.Next()
		}
		c.Locals(localsPastAuth, true)
		id := LimitIdentity{
			Key:       addressBucket(c.IP()),
			Rate:      int(keys.APIV2AnonRatePerMinute.Get()),
			Quota:     int(keys.APIV2AnonQuotaPerDay.Get()),
			Anonymous: true,
		}
		if ident != nil {
			if ki, ok := ident(c); ok {
				id = ki
			}
		}
		if id.Unlimited {
			return c.Next()
		}
		if err := lim.before(c, id); err != nil {
			return writeErr(c, err)
		}
		return c.Next()
	}
}

type limiter struct {
	store Store
}

func newLimiter(store Store) *limiter {
	if store == nil {
		store = NewMemory()
	}
	return &limiter{store: store}
}

func (l *limiter) before(c fiber.Ctx, id LimitIdentity) error {
	now := time.Now().UTC()
	minute := now.Unix() / 60
	rateKey := "v2:rate:" + id.Key + ":" + strconv.FormatInt(minute, 10)
	quotaKey := "v2:quota:" + id.Key + ":" + now.Format("2006-01-02")
	ctx := c.Context()

	n, err := l.store.Incr(ctx, rateKey, 70*time.Second)
	if err != nil {
		storeDegraded("rate counter", err)
		return nil
	}
	qn, err := l.store.Incr(ctx, quotaKey, untilTomorrow(now))
	if err != nil {
		storeDegraded("quota counter", err)
		_ = l.store.Decr(ctx, rateKey)
		return nil
	}

	rateRemaining := id.Rate - int(n)
	if rateRemaining < 0 {
		rateRemaining = 0
	}
	quotaRemaining := id.Quota - int(qn)
	if quotaRemaining < 0 {
		quotaRemaining = 0
	}
	rateReset := int((minute+1)*60 - now.Unix())
	if rateReset < 1 {
		rateReset = 1
	}
	quotaReset := int(untilTomorrow(now) / time.Second)
	if quotaReset < 1 {
		quotaReset = 1
	}

	writeLimitHeaders(c, id, rateRemaining, rateReset)

	if int(n) > id.Rate {
		c.Set("Retry-After", strconv.Itoa(rateReset))
		return tooMany(c, problem.CodeRateLimited, id.Anonymous)
	}
	if int(qn) > id.Quota {
		c.Set("Retry-After", strconv.Itoa(quotaReset))
		return tooMany(c, problem.CodeQuotaExceeded, id.Anonymous)
	}
	return nil
}

func writeLimitHeaders(c fiber.Ctx, id LimitIdentity, rateRemaining, rateReset int) {
	c.Set("RateLimit", "limit="+strconv.Itoa(id.Rate)+", remaining="+strconv.Itoa(rateRemaining)+", reset="+strconv.Itoa(rateReset))
	c.Set("RateLimit-Policy", strconv.Itoa(id.Rate)+";w=60, "+strconv.Itoa(id.Quota)+";w=86400")
	c.Set("X-RateLimit-Limit", strconv.Itoa(id.Rate))
	c.Set("X-RateLimit-Remaining", strconv.Itoa(rateRemaining))
	c.Set("X-RateLimit-Reset", strconv.Itoa(rateReset))
}

func untilTomorrow(now time.Time) time.Duration {
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return tomorrow.Sub(now)
}

const anonymousLimitHint = " Requests without a credential share one budget per IPv4 address or IPv6 /64; " +
	"an application key from https://developer.nextmoe.dev (self-service, no approval) has its own, higher limit."

func tooMany(c fiber.Ctx, code string, anonymous bool) error {
	p := problem.New(code, problem.RequestID(c), problem.Instance(c), "Rate or quota exceeded.")
	if code == problem.CodeQuotaExceeded {
		p.Detail = "Daily quota exceeded."
	} else {
		p.Detail = "Short-window rate limit exceeded."
	}
	if anonymous {
		p.Detail += anonymousLimitHint
	}
	return p
}
