package llm

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func ids(rs []*Route) []int64 {
	out := make([]int64, len(rs))
	for i, r := range rs {
		out[i] = r.ID
	}
	return out
}

func TestStrategiesOrderATier(t *testing.T) {
	p1, p2 := int64(1), int64(2)
	s := &snapshot{prices: map[int64]*PriceRevision{
		1: {InputMicro: 3_000_000, OutputMicro: 9_000_000},
		2: {InputMicro: 100_000, OutputMicro: 300_000},
	}}
	tier := []*Route{{ID: 1, Weight: 100, PriceID: &p1}, {ID: 2, Weight: 100, PriceID: &p2}, {ID: 3, Weight: 100}}
	st := newRouteStats()
	st.exploreRate = 0
	st.observe(1, 500)
	st.observe(2, 100)
	done := st.begin(2)
	defer done()
	defer st.begin(2)()
	defer st.begin(3)()

	for _, c := range []struct {
		strategy string
		want     []int64
	}{
		{StrategyCost, []int64{2, 1, 3}},
		{StrategyLeastBusy, []int64{1, 3, 2}},
		{StrategyLatency, []int64{3, 2, 1}},
	} {
		got := ids(s.order(c.strategy, tier, "conv", st))
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: order = %v, want %v", c.strategy, got, c.want)
				break
			}
		}
	}
}

func TestLocalCoordEnforcesConcurrencyAndPerMinuteLimits(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	l := newLocalCoord(func() time.Time { return now })
	ctx := context.Background()
	release, ok := l.Acquire(ctx, 1, 1, 0)
	if !ok {
		t.Fatal("first call fits")
	}
	if _, ok := l.Acquire(ctx, 1, 1, 0); ok {
		t.Fatal("a second concurrent call exceeds max_in_flight=1")
	}
	release()
	if r, ok := l.Acquire(ctx, 1, 1, 0); !ok {
		t.Fatal("released slot is reusable")
	} else {
		r()
	}
	for i := range 2 {
		r, ok := l.Acquire(ctx, 2, 0, 2)
		if !ok {
			t.Fatalf("call %d within rpm=2", i)
		}
		r()
	}
	if _, ok := l.Acquire(ctx, 2, 0, 2); ok {
		t.Fatal("the third call in a minute exceeds rpm=2")
	}
	now = now.Add(time.Minute)
	if _, ok := l.Acquire(ctx, 2, 0, 2); !ok {
		t.Fatal("a new minute resets the count")
	}
}

func TestRedisCoordSharesCooldownsAndSlotsAcrossProcesses(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("no local redis: %v", err)
	}
	t.Cleanup(func() {
		rdb.Del(ctx, coolKey, inflightKey(901))
		_ = rdb.Close()
	})
	a, b := NewRedisCoord(rdb), NewRedisCoord(rdb)
	until := time.Now().Add(time.Minute).Truncate(time.Millisecond)
	a.PublishCool(ctx, 901, until)
	got, err := b.Cooling(ctx)
	if err != nil || !got[901].Equal(until) {
		t.Fatalf("another process sees the cooldown: %v err=%v", got, err)
	}
	b.ClearCool(ctx, 901)
	if got, _ := a.Cooling(ctx); !got[901].IsZero() {
		t.Fatal("clearing is shared too")
	}
	release, ok := a.Acquire(ctx, 901, 1, 0)
	if !ok {
		t.Fatal("first slot")
	}
	if _, ok := b.Acquire(ctx, 901, 1, 0); ok {
		t.Fatal("the slot is held across processes")
	}
	if load := b.Load(ctx, []int64{901}); load[901].InFlight != 1 {
		t.Fatalf("load = %+v", load)
	}
	release()
	if r, ok := b.Acquire(ctx, 901, 1, 0); !ok {
		t.Fatal("released across processes")
	} else {
		r()
	}

	g := New(nil, nil)
	g.SetCoordinator(a)
	a.PublishCool(ctx, 902, time.Now().Add(time.Minute))
	defer a.ClearCool(ctx, 902)
	g.syncCooling(ctx)
	if _, blocked := g.health.blockedUntil(902); !blocked {
		t.Fatal("a gateway that never failed on route 902 still skips it after another process cooled it")
	}
}
