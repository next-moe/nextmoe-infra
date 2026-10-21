package settings

import "testing"

func Override[T any](tb testing.TB, k *Key[T], v T) {
	tb.Helper()
	prev := k.current.Load()
	k.apply(v, SourceDB)
	tb.Cleanup(func() {
		k.current.Store(prev)
	})
}

func OverrideSite[T any](tb testing.TB, k *Key[T], siteID uint, v T) {
	tb.Helper()
	prev := k.sites.Load()
	next := map[uint]T{}
	if prev != nil {
		for id, val := range *prev {
			next[id] = val
		}
	}
	next[siteID] = v
	k.sites.Store(&next)
	tb.Cleanup(func() {
		k.sites.Store(prev)
	})
}
