package llm

import (
	"errors"
	"sync"
	"time"
)

const (
	breakAfterTransient = 3
	breakBase           = 30 * time.Second
	breakCap            = 10 * time.Minute
	breakNotFound       = 5 * time.Minute
	breakAuth           = time.Minute
	breakAuthCap        = 30 * time.Minute
	alertAfterAuth      = 3
	retryAfterFloor     = time.Second
	retryAfterCap       = 10 * time.Minute
)

type routeHealth struct {
	transient   int
	authStrikes int
	openUntil   time.Time
}

type health struct {
	mu     sync.Mutex
	m      map[int64]*routeHealth
	shared map[int64]time.Time
	now    func() time.Time
}

func newHealth(now func() time.Time) *health {
	return &health{m: map[int64]*routeHealth{}, shared: map[int64]time.Time{}, now: now}
}

func (h *health) setShared(m map[int64]time.Time) {
	h.mu.Lock()
	h.shared = m
	h.mu.Unlock()
}

func (h *health) reset(id int64) {
	h.mu.Lock()
	delete(h.m, id)
	delete(h.shared, id)
	h.mu.Unlock()
}

type HealthState struct {
	CoolUntil  *time.Time `json:"cool_until"`
	Strikes    int        `json:"strikes" doc:"Consecutive failures this process has seen."`
	SharedOnly bool       `json:"shared_only" doc:"Cooling because another gateway process opened the breaker."`
}

func (h *health) state(id int64) HealthState {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	var st HealthState
	if r := h.m[id]; r != nil {
		st.Strikes = r.transient + r.authStrikes
		if now.Before(r.openUntil) {
			t := r.openUntil
			st.CoolUntil = &t
		}
	}
	if t, ok := h.shared[id]; ok && now.Before(t) && (st.CoolUntil == nil || t.After(*st.CoolUntil)) {
		st.SharedOnly = st.CoolUntil == nil
		st.CoolUntil = &t
	}
	return st
}

func (h *health) get(id int64) *routeHealth {
	r := h.m[id]
	if r == nil {
		r = &routeHealth{}
		h.m[id] = r
	}
	return r
}

func (h *health) blockedUntil(id int64) (time.Time, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	var until time.Time
	if r := h.m[id]; r != nil && now.Before(r.openUntil) {
		until = r.openUntil
	}
	if t, ok := h.shared[id]; ok && now.Before(t) && t.After(until) {
		until = t
	}
	return until, !until.IsZero()
}

func (h *health) success(id int64) (wasShared bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.m, id)
	_, wasShared = h.shared[id]
	delete(h.shared, id)
	return wasShared
}

func (h *health) failure(id int64, err error) (alert bool, opened time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	defer func() {
		if r := h.m[id]; r != nil && h.now().Before(r.openUntil) {
			opened = r.openUntil
		}
	}()
	r := h.get(id)
	now := h.now()
	kind := KindOf(err)
	var le *Error
	if errors.As(err, &le) && le.RetryAfter > 0 && (kind == ErrKindRateLimit || kind == ErrKindUpstream) {
		r.transient++
		r.openUntil = now.Add(min(max(le.RetryAfter, retryAfterFloor), retryAfterCap))
		return false, opened
	}
	switch {
	case kind.transient():
		r.transient++
		if r.transient >= breakAfterTransient {
			d := breakBase << min(r.transient-breakAfterTransient, 5)
			r.openUntil = now.Add(min(d, breakCap))
		}
	case kind == ErrKindNotFound:
		r.openUntil = now.Add(breakNotFound)
	// Cloudflare has answered 401 when merely overloaded, so an auth failure
	// backs the route off instead of disabling it: a permanent suspension
	// turned a burst of those into an outage that waited for a human.
	case kind == ErrKindAuth || kind == ErrKindQuota:
		r.authStrikes++
		r.openUntil = now.Add(min(breakAuth<<min(r.authStrikes-1, 5), breakAuthCap))
		return r.authStrikes == alertAfterAuth, opened
	}
	return false, opened
}
