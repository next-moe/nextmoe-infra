package ingest

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

const (
	ipCapacity      = 60
	ipRefillPerSec  = 1.0
	appCapacity     = 1000
	appRefillPerSec = 100.0
	idleTTL         = 10 * time.Minute
	sweepEvery      = time.Minute
)

type ipKey [32]byte

type tokenBucket struct {
	tokens float64
	last   time.Time
}

type Limiter struct {
	mu         sync.Mutex
	processKey []byte
	ip         map[ipKey]*tokenBucket
	app        map[int64]*tokenBucket
	lastSweep  time.Time
	clock      Clock
}

func NewLimiter(clock Clock) *Limiter {
	if clock == nil {
		clock = sysClock{}
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		panic("ingest: process key: " + err.Error())
	}
	return &Limiter{
		processKey: key,
		ip:         make(map[ipKey]*tokenBucket),
		app:        make(map[int64]*tokenBucket),
		clock:      clock,
	}
}

func (l *Limiter) hashIP(appID int64, ip string) ipKey {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], uint64(appID))
	mac := hmac.New(sha256.New, l.processKey)
	_, _ = mac.Write(buf[:])
	_, _ = mac.Write([]byte(ip))
	var out ipKey
	copy(out[:], mac.Sum(nil))
	return out
}

type LimitResult struct {
	OK         bool
	Status     int
	RetryAfter int
}

func (l *Limiter) Allow(appID int64, ip string) LimitResult {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.lastSweep) >= sweepEvery {
		l.sweepLocked(now)
		l.lastSweep = now
	}
	ik := l.hashIP(appID, ip)
	ipB := l.ip[ik]
	if ipB == nil {
		ipB = &tokenBucket{tokens: ipCapacity, last: now}
		l.ip[ik] = ipB
	}
	if !take(ipB, now, ipCapacity, ipRefillPerSec) {
		return LimitResult{Status: 429, RetryAfter: retryAfter(ipB, ipRefillPerSec, 5)}
	}
	appB := l.app[appID]
	if appB == nil {
		appB = &tokenBucket{tokens: appCapacity, last: now}
		l.app[appID] = appB
	}
	if !take(appB, now, appCapacity, appRefillPerSec) {
		ipB.tokens = math.Min(ipCapacity, ipB.tokens+1)
		return LimitResult{Status: 503, RetryAfter: 30}
	}
	return LimitResult{OK: true}
}

func take(b *tokenBucket, now time.Time, capacity, refill float64) bool {
	elapsed := now.Sub(b.last).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	b.tokens = math.Min(capacity, b.tokens+elapsed*refill)
	b.last = now
	if b.tokens >= 1 {
		b.tokens -= 1
		return true
	}
	return false
}

func retryAfter(b *tokenBucket, refill float64, floor int) int {
	need := 1 - b.tokens
	if need < 0 {
		need = 0
	}
	sec := need / refill
	n := int(math.Ceil(sec))
	if n < floor {
		n = floor
	}
	return n
}

func (l *Limiter) Sweep(now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sweepLocked(now)
	l.lastSweep = now
}

func (l *Limiter) sweepLocked(now time.Time) {
	for k, b := range l.ip {
		if now.Sub(b.last) >= idleTTL {
			delete(l.ip, k)
		}
	}
	for k, b := range l.app {
		if now.Sub(b.last) >= idleTTL {
			delete(l.app, k)
		}
	}
}

func (l *Limiter) ipKeyStrings() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.ip))
	for k := range l.ip {
		out = append(out, string(k[:]))
	}
	return out
}

func (l *Limiter) ipBucketCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.ip)
}
