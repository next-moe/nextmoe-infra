package main

import (
	"context"
	"errors"
	"sync"
	"time"
)

var errDailyQuota = errors.New("workers-ai daily allocation used up")

// quotaGate holds every worker while the Workers AI daily allocation is used up,
// probing until it frees or the wait runs out. The allocation is a rolling
// window: the 2026-09-29 run started at 07:00Z with it still full from the day
// before and freed minutes later, so stopping at the first refusal would have
// graded nothing, and never stopping spent 3h40m re-asking 12,000 images.
type quotaGate struct {
	wait  time.Duration
	probe time.Duration

	mu     sync.Mutex
	since  time.Time
	gaveUp bool
}

func (g *quotaGate) hold(ctx context.Context) bool {
	g.mu.Lock()
	if g.gaveUp {
		g.mu.Unlock()
		return false
	}
	if g.since.IsZero() {
		g.since = time.Now()
	}
	if time.Since(g.since) >= g.wait {
		g.gaveUp = true
		g.mu.Unlock()
		return false
	}
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		return false
	case <-time.After(g.probe):
		return true
	}
}

func (g *quotaGate) clear() {
	g.mu.Lock()
	if !g.gaveUp {
		g.since = time.Time{}
	}
	g.mu.Unlock()
}

func (g *quotaGate) exhausted() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.gaveUp
}

func gradeWithQuota(ctx context.Context, g *quotaGate, grade func() error) error {
	for {
		err := grade()
		if !errors.Is(err, errDailyQuota) {
			if err == nil {
				g.clear()
			}
			return err
		}
		if !g.hold(ctx) {
			return err
		}
	}
}
