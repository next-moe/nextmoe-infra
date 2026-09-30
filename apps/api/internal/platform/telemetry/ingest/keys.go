package ingest

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

type AppInfo struct {
	ID          int64
	ServiceName string
	Enabled     bool
	IngestKey   string
}

type LookupResult int

const (
	LookupFound LookupResult = iota + 1
	LookupUnknown
	LookupNotReady
)

type KeyLookup interface {
	Lookup(key string) (AppInfo, LookupResult)
}

type AppsSource interface {
	ListApps(ctx context.Context) ([]AppInfo, error)
}

type keyMap struct {
	byKey    map[string]AppInfo
	loadedAt time.Time
}

type KeyCache struct {
	src   AppsSource
	clock Clock
	log   *slog.Logger
	cur   atomic.Pointer[keyMap]
}

func NewKeyCache(src AppsSource, clock Clock, log *slog.Logger) *KeyCache {
	if clock == nil {
		clock = sysClock{}
	}
	if log == nil {
		log = slog.Default()
	}
	return &KeyCache{src: src, clock: clock, log: log}
}

func (c *KeyCache) Lookup(key string) (AppInfo, LookupResult) {
	snap := c.cur.Load()
	if snap == nil {
		return AppInfo{}, LookupNotReady
	}
	a, ok := snap.byKey[key]
	if !ok {
		return AppInfo{}, LookupUnknown
	}
	return a, LookupFound
}

func (c *KeyCache) Reload(ctx context.Context) error {
	apps, err := c.src.ListApps(ctx)
	if err != nil {
		return err
	}
	next := make(map[string]AppInfo, len(apps))
	for _, a := range apps {
		next[a.IngestKey] = a
	}
	c.cur.Store(&keyMap{byKey: next, loadedAt: c.clock.Now()})
	return nil
}

func (c *KeyCache) Invalidate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return c.Reload(ctx)
}

func (c *KeyCache) Run(ctx context.Context) {
	t := time.NewTicker(5 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if snap := c.cur.Load(); snap != nil && c.clock.Now().Sub(snap.loadedAt) < 60*time.Second {
				continue
			}
			rctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			err := c.Reload(rctx)
			cancel()
			if err != nil {
				c.log.Error("telemetry key cache reload", "err", err)
			}
		}
	}
}
