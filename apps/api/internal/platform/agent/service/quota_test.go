package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/settings"
	"api/internal/platform/settings/keys"

	"github.com/redis/go-redis/v9"
)

func testRedis(t *testing.T) redis.UniversalClient {
	t.Helper()
	rdb := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379", DB: 15})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("no local redis: %v", err)
	}
	t.Cleanup(func() { _ = rdb.Close() })
	return rdb
}

type fakeLedger struct {
	balance int64
	charges []map[string]any
}

func (f *fakeLedger) server(t *testing.T) *LedgerClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balance": f.balance}})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		amount := int64(body["amount"].(float64))
		if amount > f.balance {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":16006,"message":"萌萌点不足"}`))
			return
		}
		f.balance -= amount
		f.charges = append(f.charges, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"balance": f.balance}})
	}))
	t.Cleanup(srv.Close)
	return NewLedgerClient(srv.URL, "agent", "secret")
}

func TestQuotaSpendsFreeUnitsThenMoemoepoints(t *testing.T) {
	rdb := testRedis(t)
	settings.Override(t, keys.AgentFreeUnitsPerDay, int64(3))
	led := &fakeLedger{balance: 1}
	q := NewQuota(rdb, led.server(t))
	q.now = func() time.Time { return time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	user := time.Now().UnixNano()
	defer rdb.Del(ctx, q.freeKey(user))

	r1, err := q.Reserve(ctx, user, 2)
	if err != nil || r1.Free != 2 || r1.Paid != 0 {
		t.Fatalf("first reserve = %+v err=%v", r1, err)
	}
	s1 := q.Settle(ctx, user, r1, 1, "run-1", "assistant")
	if s1.Free != 1 || s1.State != model.SettleFree || q.FreeLeft(ctx, user) != 2 {
		t.Fatalf("settle must hand back the unused unit: %+v left=%d", s1, q.FreeLeft(ctx, user))
	}

	r2, err := q.Reserve(ctx, user, 3)
	if err != nil || r2.Free != 2 || r2.Paid != 1 {
		t.Fatalf("second reserve = %+v err=%v", r2, err)
	}
	s2 := q.Settle(ctx, user, r2, 3, "run-2", "assistant")
	if s2.State != model.SettlePaid || s2.Paid != 1 || len(led.charges) != 1 || led.charges[0]["idempotency_key"] != "agent:run:run-2" {
		t.Fatalf("settle = %+v charges=%v", s2, led.charges)
	}

	if _, err := q.Reserve(ctx, user, 1); !errors.Is(err, ErrInsufficientPoints) {
		t.Fatalf("no free units and no points: err=%v", err)
	}
	if q.FreeLeft(ctx, user) != 0 {
		t.Fatal("a refused reservation must not hold free units")
	}
}

func TestQuotaMarksARaceLostChargeUnpaid(t *testing.T) {
	rdb := testRedis(t)
	settings.Override(t, keys.AgentFreeUnitsPerDay, int64(0))
	led := &fakeLedger{balance: 5}
	q := NewQuota(rdb, led.server(t))
	ctx := context.Background()
	user := time.Now().UnixNano()
	defer rdb.Del(ctx, q.freeKey(user))
	r, err := q.Reserve(ctx, user, 2)
	if err != nil || r.Paid != 2 {
		t.Fatalf("reserve = %+v err=%v", r, err)
	}
	led.balance = 1
	if s := q.Settle(ctx, user, r, 2, "run-3", "assistant"); s.State != model.SettleUnpaid {
		t.Fatalf("settle = %+v", s)
	}
	if !strings.Contains(ErrInsufficientPoints.Error(), "moemoepoints") {
		t.Fatal("error text")
	}
}
