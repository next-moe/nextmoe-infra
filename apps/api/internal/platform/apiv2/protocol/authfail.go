package protocol

import (
	"log/slog"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"api/internal/platform/apiv2/problem"
	"api/internal/platform/settings/keys"

	"github.com/gofiber/fiber/v3"
)

// Deviations 29 and 47 moved the quota limiter behind the auth stack on
// purpose, and that left one hole nothing counted: a request the auth stack
// refuses never reaches RateLimit, so 401/403 volume was unbounded. This bucket
// covers exactly that hole — an answer the quota limiter never saw — and it is
// keyed by IP because a request that failed to authenticate has no other
// identity. Do not "fix" a future finding here by moving RateLimit back in
// front of auth: that reintroduces both regressions at once.
func (l *limiter) authFailBlocked(c fiber.Ctx) bool {
	b, err := l.store.Get(c.Context(), authBlockKey(addressBucket(c.IP())))
	if err != nil {
		storeDegraded("auth-failure block probe", err)
		return false
	}
	return len(b) > 0
}

func (l *limiter) countAuthFailure(c fiber.Ctx) {
	status := c.Response().StatusCode()
	if status != fiber.StatusUnauthorized && status != fiber.StatusForbidden {
		return
	}
	if past, _ := c.Locals(localsPastAuth).(bool); past {
		return
	}
	ip := addressBucket(c.IP())
	minute := time.Now().UTC().Unix() / 60
	n, err := l.store.Incr(c.Context(), "v2:authfail:"+ip+":"+strconv.FormatInt(minute, 10), 70*time.Second)
	if err != nil {
		storeDegraded("auth-failure counter", err)
		return
	}
	if int(n) < int(keys.APIV2AuthFailPerMinute.Get()) {
		return
	}
	if err := l.store.Set(c.Context(), authBlockKey(ip), []byte{'1'}, time.Duration(keys.APIV2AuthFailBlockSeconds.Get())*time.Second); err != nil {
		storeDegraded("auth-failure block write", err)
		return
	}
	slog.Warn("v2 auth failures over budget; blocking the source", "ip", ip, "failures", n, "window", "60s")
}

func authFailRefusal(c fiber.Ctx) error {
	retry := int(keys.APIV2AuthFailBlockSeconds.Get())
	c.Set("Retry-After", strconv.Itoa(retry))
	p := problem.New(problem.CodeRateLimited, problem.RequestID(c), problem.Instance(c),
		"Too many rejected credentials from this address.")
	p.Detail = "Too many rejected credentials from this address."
	return p
}

func authBlockKey(ip string) string { return "v2:authfail:blocked:" + ip }

// One subscriber line is routed a whole IPv6 /64, so a bucket keyed on the
// full address is one per source address the holder cares to pick.
func addressBucket(raw string) string {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		if raw == "" {
			return "unknown"
		}
		return raw
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if p, err := addr.Prefix(64); err == nil {
			return p.String()
		}
	}
	return addr.String()
}

func clientIP(c fiber.Ctx) string {
	if ip := c.IP(); ip != "" {
		return ip
	}
	return "unknown"
}

const degradedLogEvery = 10 * time.Second

var (
	degradedMu   sync.Mutex
	degradedAt   time.Time
	degradedSkip int
)

// Fail-open is the deliberate choice — the limiter's store is the same redis
// every /v2 read already tolerates losing, and failing closed would turn a
// redis blip into a total API outage. It was silent before, so one outage
// removed every rate limit and every daily quota at once with nothing in the
// log to say so.
func storeDegraded(where string, err error) {
	now := time.Now()
	degradedMu.Lock()
	if !degradedAt.IsZero() && now.Sub(degradedAt) < degradedLogEvery {
		degradedSkip++
		degradedMu.Unlock()
		return
	}
	suppressed := degradedSkip
	degradedAt, degradedSkip = now, 0
	degradedMu.Unlock()
	slog.Warn("v2 limiter store unavailable; failing open", "at", where, "err", err, "suppressed", suppressed)
}
