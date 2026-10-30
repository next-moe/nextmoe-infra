package llm

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RouteLoad struct {
	InFlight int `json:"in_flight"`
	RPMUsed  int `json:"rpm_used"`
}

// Coordinator shares what one gateway process learns with the others (the ai
// service and the agent each run one): route cooldowns, and the in-flight and
// per-minute counts that route limits are enforced against.
type Coordinator interface {
	PublishCool(ctx context.Context, routeID int64, until time.Time)
	ClearCool(ctx context.Context, routeID int64)
	Cooling(ctx context.Context) (map[int64]time.Time, error)
	Acquire(ctx context.Context, routeID int64, maxConcurrent, rpm int) (release func(), ok bool)
	Load(ctx context.Context, routeIDs []int64) map[int64]RouteLoad
	AddSpend(ctx context.Context, key string, nano int64) int64
	Spend(ctx context.Context, keys []string) map[string]int64
	Once(ctx context.Context, key string, ttl time.Duration) bool
}

type localCoord struct {
	mu       sync.Mutex
	cool     map[int64]time.Time
	inflight map[int64]int
	minute   map[int64][2]int64
	spend    map[string]int64
	once     map[string]time.Time
	now      func() time.Time
}

func newLocalCoord(now func() time.Time) *localCoord {
	return &localCoord{cool: map[int64]time.Time{}, inflight: map[int64]int{}, minute: map[int64][2]int64{},
		spend: map[string]int64{}, once: map[string]time.Time{}, now: now}
}

func (l *localCoord) AddSpend(_ context.Context, key string, nano int64) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.spend[key] += nano
	return l.spend[key]
}

func (l *localCoord) Spend(_ context.Context, keys []string) map[string]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int64{}
	for _, k := range keys {
		out[k] = l.spend[k]
	}
	return out
}

func (l *localCoord) Once(_ context.Context, key string, ttl time.Duration) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t, ok := l.once[key]; ok && l.now().Before(t) {
		return false
	}
	l.once[key] = l.now().Add(ttl)
	return true
}

func (l *localCoord) PublishCool(_ context.Context, id int64, until time.Time) {
	l.mu.Lock()
	l.cool[id] = until
	l.mu.Unlock()
}

func (l *localCoord) ClearCool(_ context.Context, id int64) {
	l.mu.Lock()
	delete(l.cool, id)
	l.mu.Unlock()
}

func (l *localCoord) Cooling(context.Context) (map[int64]time.Time, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[int64]time.Time, len(l.cool))
	for k, v := range l.cool {
		out[k] = v
	}
	return out, nil
}

func (l *localCoord) Acquire(_ context.Context, id int64, maxConcurrent, rpm int) (func(), bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if maxConcurrent > 0 && l.inflight[id] >= maxConcurrent {
		return nil, false
	}
	if rpm > 0 {
		m := l.now().Unix() / 60
		cur := l.minute[id]
		if cur[0] != m {
			cur = [2]int64{m, 0}
		}
		if cur[1] >= int64(rpm) {
			return nil, false
		}
		cur[1]++
		l.minute[id] = cur
	}
	l.inflight[id]++
	return func() {
		l.mu.Lock()
		l.inflight[id]--
		l.mu.Unlock()
	}, true
}

func (l *localCoord) Load(_ context.Context, ids []int64) map[int64]RouteLoad {
	l.mu.Lock()
	defer l.mu.Unlock()
	m := l.now().Unix() / 60
	out := map[int64]RouteLoad{}
	for _, id := range ids {
		rl := RouteLoad{InFlight: l.inflight[id]}
		if cur := l.minute[id]; cur[0] == m {
			rl.RPMUsed = int(cur[1])
		}
		out[id] = rl
	}
	return out
}

// RedisCoord: in-flight counters expire after inflightTTL so a process that
// dies mid-call cannot hold a slot for ever.
type RedisCoord struct {
	rdb redis.UniversalClient
	now func() time.Time
}

const (
	coolKey     = "llm:cool"
	inflightTTL = 15 * time.Minute
)

func NewRedisCoord(rdb redis.UniversalClient) *RedisCoord {
	return &RedisCoord{rdb: rdb, now: time.Now}
}

func (r *RedisCoord) PublishCool(ctx context.Context, id int64, until time.Time) {
	_ = r.rdb.HSet(ctx, coolKey, strconv.FormatInt(id, 10), until.UnixMilli()).Err()
}

func (r *RedisCoord) ClearCool(ctx context.Context, id int64) {
	_ = r.rdb.HDel(ctx, coolKey, strconv.FormatInt(id, 10)).Err()
}

func (r *RedisCoord) Cooling(ctx context.Context) (map[int64]time.Time, error) {
	raw, err := r.rdb.HGetAll(ctx, coolKey).Result()
	if err != nil {
		return nil, err
	}
	now := r.now()
	out := map[int64]time.Time{}
	var stale []string
	for k, v := range raw {
		id, err1 := strconv.ParseInt(k, 10, 64)
		ms, err2 := strconv.ParseInt(v, 10, 64)
		if err1 != nil || err2 != nil {
			stale = append(stale, k)
			continue
		}
		until := time.UnixMilli(ms)
		if !until.After(now) {
			stale = append(stale, k)
			continue
		}
		out[id] = until
	}
	if len(stale) > 0 {
		_ = r.rdb.HDel(ctx, coolKey, stale...).Err()
	}
	return out, nil
}

func inflightKey(id int64) string { return fmt.Sprintf("llm:inflight:%d", id) }

func minuteKey(id int64, t time.Time) string { return fmt.Sprintf("llm:rpm:%d:%d", id, t.Unix()/60) }

func (r *RedisCoord) Acquire(ctx context.Context, id int64, maxConcurrent, rpm int) (func(), bool) {
	if rpm > 0 {
		k := minuteKey(id, r.now())
		n, err := r.rdb.Incr(ctx, k).Result()
		if err == nil {
			_ = r.rdb.Expire(ctx, k, 2*time.Minute).Err()
			if n > int64(rpm) {
				return nil, false
			}
		}
	}
	if maxConcurrent <= 0 {
		return func() {}, true
	}
	k := inflightKey(id)
	n, err := r.rdb.Incr(ctx, k).Result()
	if err != nil {
		return func() {}, true
	}
	_ = r.rdb.Expire(ctx, k, inflightTTL).Err()
	if n > int64(maxConcurrent) {
		_ = r.rdb.Decr(context.WithoutCancel(ctx), k).Err()
		return nil, false
	}
	return func() { _ = r.rdb.Decr(context.Background(), k).Err() }, true
}

func (r *RedisCoord) Load(ctx context.Context, ids []int64) map[int64]RouteLoad {
	out := map[int64]RouteLoad{}
	if len(ids) == 0 {
		return out
	}
	pipe := r.rdb.Pipeline()
	now := r.now()
	in := make([]*redis.StringCmd, len(ids))
	rpm := make([]*redis.StringCmd, len(ids))
	for i, id := range ids {
		in[i] = pipe.Get(ctx, inflightKey(id))
		rpm[i] = pipe.Get(ctx, minuteKey(id, now))
	}
	_, _ = pipe.Exec(ctx)
	for i, id := range ids {
		a, _ := in[i].Int()
		b, _ := rpm[i].Int()
		out[id] = RouteLoad{InFlight: max(a, 0), RPMUsed: b}
	}
	return out
}

func (r *RedisCoord) AddSpend(ctx context.Context, key string, nano int64) int64 {
	k := "llm:spend:" + key
	n, err := r.rdb.IncrBy(ctx, k, nano).Result()
	if err != nil {
		return 0
	}
	_ = r.rdb.Expire(ctx, k, 48*time.Hour).Err()
	return n
}

func (r *RedisCoord) Spend(ctx context.Context, keys []string) map[string]int64 {
	out := map[string]int64{}
	if len(keys) == 0 {
		return out
	}
	full := make([]string, len(keys))
	for i, k := range keys {
		full[i] = "llm:spend:" + k
	}
	vals, err := r.rdb.MGet(ctx, full...).Result()
	if err != nil {
		return out
	}
	for i, v := range vals {
		if s, ok := v.(string); ok {
			n, _ := strconv.ParseInt(s, 10, 64)
			out[keys[i]] = n
		}
	}
	return out
}

func (r *RedisCoord) Once(ctx context.Context, key string, ttl time.Duration) bool {
	ok, err := r.rdb.SetNX(ctx, "llm:once:"+key, 1, ttl).Result()
	return err == nil && ok
}
