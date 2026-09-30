package ingest

import "time"

type Clock interface {
	Now() time.Time
}

type sysClock struct{}

func (sysClock) Now() time.Time { return time.Now() }

type frozenClock struct {
	t time.Time
}

func (c *frozenClock) Now() time.Time { return c.t }

func (c *frozenClock) Set(t time.Time) { c.t = t }

func (c *frozenClock) Add(d time.Duration) { c.t = c.t.Add(d) }
