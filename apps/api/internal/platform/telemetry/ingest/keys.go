package ingest

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type AppInfo struct {
	ID          int64
	ServiceName string
	Enabled     bool
	IngestKey   string
}

type KeyLookup interface {
	Lookup(key string) (AppInfo, bool)
}

type AppsSource interface {
	ListApps(ctx context.Context) ([]AppInfo, error)
}

type KeyCache struct {
	src      AppsSource
	clock    Clock
	log      *slog.Logger
	mu       sync.Mutex
	byKey    map[string]AppInfo
	loadedAt time.Time
	loaded   bool
}

func NewKeyCache(src AppsSource, clock Clock, log *slog.Logger) *KeyCache {
	if clock == nil {
		clock = sysClock{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &KeyCache{src: src, clock: clock, log: log, byKey: map[string]AppInfo{}}
}

func (c *KeyCache) Lookup(key string) (AppInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock.Now()
	if !c.loaded || now.Sub(c.loadedAt) >= 60*time.Second {
		c.reloadLocked()
	}
	a, ok := c.byKey[key]
	return a, ok
}

func (c *KeyCache) Invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.reloadLocked()
}

func (c *KeyCache) reloadLocked() {
	apps, err := c.src.ListApps(context.Background())
	if err != nil {
		c.log.Error("telemetry key cache reload", "err", err)
		return
	}
	next := make(map[string]AppInfo, len(apps))
	for _, a := range apps {
		next[a.IngestKey] = a
	}
	c.byKey = next
	c.loadedAt = c.clock.Now()
	c.loaded = true
}
