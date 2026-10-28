package llm

import (
	"sync"
	"time"
)

const (
	breakAfterTransient = 3
	breakBase           = 30 * time.Second
	breakCap            = 10 * time.Minute
	breakNotFound       = 5 * time.Minute
	breakAuth           = time.Minute
	suspendAfterAuth    = 3
)

type routeHealth struct {
	transient   int
	authStrikes int
	openUntil   time.Time
}

type health struct {
	mu  sync.Mutex
	m   map[int64]*routeHealth
	now func() time.Time
}

func newHealth(now func() time.Time) *health {
	return &health{m: map[int64]*routeHealth{}, now: now}
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
	r := h.m[id]
	if r == nil || !h.now().Before(r.openUntil) {
		return time.Time{}, false
	}
	return r.openUntil, true
}

func (h *health) success(id int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.m, id)
}

func (h *health) failure(id int64, kind ErrorKind) (suspend bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.get(id)
	now := h.now()
	switch {
	case kind.transient():
		r.transient++
		if r.transient >= breakAfterTransient {
			d := breakBase << min(r.transient-breakAfterTransient, 5)
			r.openUntil = now.Add(min(d, breakCap))
		}
	case kind == ErrKindNotFound:
		r.openUntil = now.Add(breakNotFound)
	case kind == ErrKindAuth || kind == ErrKindQuota:
		r.authStrikes++
		r.openUntil = now.Add(breakAuth)
		return r.authStrikes >= suspendAfterAuth
	}
	return false
}
