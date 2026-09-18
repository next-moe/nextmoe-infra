package protocol

import (
	"context"
	"sync"
	"time"

	"api/internal/infrastructure/cache"
)

type Store interface {
	Incr(ctx context.Context, key string, ttl time.Duration) (int64, error)
	Decr(ctx context.Context, key string) error
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
}

type Memory struct {
	mu    sync.Mutex
	n     map[string]int64
	exp   map[string]time.Time
	kv    map[string][]byte
	kvExp map[string]time.Time
}

func NewMemory() *Memory {
	return &Memory{
		n:     map[string]int64{},
		exp:   map[string]time.Time{},
		kv:    map[string][]byte{},
		kvExp: map[string]time.Time{},
	}
}

func (m *Memory) Incr(_ context.Context, key string, ttl time.Duration) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	if exp, ok := m.exp[key]; ok && now.After(exp) {
		delete(m.n, key)
		delete(m.exp, key)
	}
	m.n[key]++
	if _, ok := m.exp[key]; !ok {
		m.exp[key] = now.Add(ttl)
	}
	return m.n[key], nil
}

func (m *Memory) Decr(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.n[key] > 0 {
		m.n[key]--
	}
	return nil
}

func (m *Memory) Get(_ context.Context, key string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if exp, ok := m.kvExp[key]; ok && time.Now().After(exp) {
		delete(m.kv, key)
		delete(m.kvExp, key)
		return nil, nil
	}
	b := m.kv[key]
	if b == nil {
		return nil, nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out, nil
}

func (m *Memory) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(value))
	copy(cp, value)
	m.kv[key] = cp
	m.kvExp[key] = time.Now().Add(ttl)
	return nil
}

func (m *Memory) SetNX(_ context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if exp, ok := m.kvExp[key]; ok && time.Now().After(exp) {
		delete(m.kv, key)
		delete(m.kvExp, key)
	}
	if _, ok := m.kv[key]; ok {
		return false, nil
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	m.kv[key] = cp
	m.kvExp[key] = time.Now().Add(ttl)
	return true, nil
}

func (m *Memory) Del(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.kv, key)
	delete(m.kvExp, key)
	return nil
}

type RedisStore struct {
	cache *cache.RedisCache
}

func NewRedisStore(c *cache.RedisCache) *RedisStore {
	return &RedisStore{cache: c}
}

func (s *RedisStore) live() bool {
	return s != nil && s.cache != nil && s.cache.Enabled()
}

func (s *RedisStore) Incr(ctx context.Context, key string, ttl time.Duration) (int64, error) {
	if !s.live() {
		return 0, errUnavailable
	}
	n, err := s.cache.Storage().Conn().Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = s.cache.Storage().Conn().Expire(ctx, key, ttl).Err()
	}
	return n, nil
}

func (s *RedisStore) Decr(ctx context.Context, key string) error {
	if !s.live() {
		return nil
	}
	_, err := s.cache.Storage().Conn().Decr(ctx, key).Result()
	return err
}

func (s *RedisStore) Get(_ context.Context, key string) ([]byte, error) {
	if !s.live() {
		return nil, errUnavailable
	}
	b, err := s.cache.Get(key)
	if err != nil {
		return nil, nil
	}
	return b, nil
}

func (s *RedisStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	if !s.live() {
		return errUnavailable
	}
	return s.cache.Set(key, value, ttl)
}

func (s *RedisStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	if !s.live() {
		return false, errUnavailable
	}
	return s.cache.Storage().Conn().SetNX(ctx, key, value, ttl).Result()
}

func (s *RedisStore) Del(ctx context.Context, key string) error {
	if !s.live() {
		return errUnavailable
	}
	return s.cache.Storage().Conn().Del(ctx, key).Err()
}

var errUnavailable = errStore("protocol store unavailable")

type errStore string

func (e errStore) Error() string { return string(e) }
