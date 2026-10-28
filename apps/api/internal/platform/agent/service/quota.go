package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"api/internal/platform/agent/model"
	"api/internal/platform/settings/keys"

	"github.com/redis/go-redis/v9"
)

var ErrQuotaExhausted = errors.New("agent: today's free units are used up")

const freeKeyTTL = 48 * time.Hour

var reserveFree = redis.NewScript(`
local used = tonumber(redis.call('GET', KEYS[1]) or '0')
local limit = tonumber(ARGV[1])
local want = tonumber(ARGV[2])
local take = math.max(0, math.min(limit - used, want))
if take > 0 then
  redis.call('INCRBY', KEYS[1], take)
  redis.call('EXPIRE', KEYS[1], ARGV[3])
end
return take`)

type Quota struct {
	rdb    redis.UniversalClient
	ledger *LedgerClient
	now    func() time.Time
}

func NewQuota(rdb redis.UniversalClient, ledger *LedgerClient) *Quota {
	return &Quota{rdb: rdb, ledger: ledger, now: time.Now}
}

type Reservation struct {
	Price int
	Free  int
	Paid  int
}

func (q *Quota) freeKey(userID int64) string {
	return fmt.Sprintf("agent:free:%d:%s", userID, q.now().UTC().Format("20060102"))
}

func (q *Quota) FreeLeft(ctx context.Context, userID int64) int {
	used, err := q.rdb.Get(ctx, q.freeKey(userID)).Int()
	if err != nil && !errors.Is(err, redis.Nil) {
		return 0
	}
	return max(int(keys.AgentFreeUnitsPerDay.Get())-used, 0)
}

func (q *Quota) PaidAvailable() bool {
	return q.ledger != nil && keys.AgentPaidEnabled.Get()
}

func (q *Quota) Balance(ctx context.Context, userID int64) (int64, bool) {
	if q.ledger == nil {
		return 0, false
	}
	b, err := q.ledger.Balance(ctx, userID)
	return b, err == nil
}

func (q *Quota) Reserve(ctx context.Context, userID int64, price int) (Reservation, error) {
	res := Reservation{Price: price}
	if price <= 0 {
		return res, nil
	}
	take, err := reserveFree.Run(ctx, q.rdb, []string{q.freeKey(userID)},
		keys.AgentFreeUnitsPerDay.Get(), price, int(freeKeyTTL.Seconds())).Int()
	if err != nil {
		return res, fmt.Errorf("reserve free units: %w", err)
	}
	res.Free, res.Paid = take, price-take
	if res.Paid == 0 {
		return res, nil
	}
	if !q.PaidAvailable() {
		q.release(ctx, userID, res.Free)
		return Reservation{}, ErrQuotaExhausted
	}
	balance, err := q.ledger.Balance(ctx, userID)
	if err != nil {
		q.release(ctx, userID, res.Free)
		return Reservation{}, fmt.Errorf("read moemoepoint balance: %w", err)
	}
	if balance < int64(res.Paid) {
		q.release(ctx, userID, res.Free)
		return Reservation{}, ErrInsufficientPoints
	}
	return res, nil
}

func (q *Quota) release(ctx context.Context, userID int64, units int) {
	if units <= 0 {
		return
	}
	if err := q.rdb.DecrBy(ctx, q.freeKey(userID), int64(units)).Err(); err != nil {
		slog.Warn("agent quota: release free units", "user", userID, "err", err)
	}
}

func (q *Quota) Release(ctx context.Context, userID int64, r Reservation) {
	q.release(ctx, userID, r.Free)
}

type Settlement struct {
	Free, Paid int
	State      string
}

func (q *Quota) Settle(ctx context.Context, userID int64, r Reservation, actual int, runID, note string) Settlement {
	actual = min(max(actual, 0), r.Price)
	usedFree := min(r.Free, actual)
	q.release(ctx, userID, r.Free-usedFree)
	s := Settlement{Free: usedFree, State: model.SettleNone}
	if usedFree > 0 {
		s.State = model.SettleFree
	}
	paid := actual - usedFree
	if paid <= 0 {
		return s
	}
	s.Paid = paid
	err := q.ledger.Charge(ctx, userID, paid, runID, note)
	switch {
	case err == nil:
		s.State = model.SettlePaid
	case errors.Is(err, ErrInsufficientPoints):
		s.State = model.SettleUnpaid
		slog.Warn("agent quota: balance spent between check and charge", "user", userID, "run", runID)
	default:
		s.State = model.SettlePending
		slog.Warn("agent quota: charge failed; the run stays pending", "user", userID, "run", runID, "err", err)
	}
	return s
}
