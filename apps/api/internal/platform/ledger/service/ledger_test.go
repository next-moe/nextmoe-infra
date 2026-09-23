package service_test

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	authModel "api/internal/platform/auth/model"
	"api/internal/platform/ledger/ledgertest"
	"api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"
	"api/internal/testsupport/dbtest"
	"api/pkg/errors"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDB *gorm.DB

func TestMain(m *testing.M) {
	dsn, ok := dbtest.DSN()
	if !ok {
		dbtest.SkipMain("ledger suite")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		dbtest.SkipMainf("ledger suite", "connect: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		dbtest.SkipMainf("ledger suite", "migrate: %v", err)
	}
	testDB = db
	os.Exit(m.Run())
}

var userSeq atomic.Int64

func newUser(t *testing.T, moemoepoint int) uint {
	t.Helper()
	tag := strconv.FormatInt(time.Now().UnixNano(), 36) + strconv.FormatInt(userSeq.Add(1), 36)
	u := &authModel.User{Name: "lg" + tag[len(tag)-10:], Email: "lg-" + tag + "@test.local", Moemoepoint: moemoepoint}
	if err := testDB.Create(u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	t.Cleanup(func() {
		if err := ledgertest.Purge(testDB, u.ID); err != nil {
			t.Errorf("purge: %v", err)
		}
		testDB.Exec(`DELETE FROM users WHERE id = ?`, u.ID)
	})
	return u.ID
}

func balances(t *testing.T, l *service.Ledger, id uint) (ledger int64, mirror int64) {
	t.Helper()
	ledger, err := l.UserBalance(context.Background(), id)
	if err != nil {
		t.Fatalf("balance: %v", err)
	}
	if err := testDB.Raw(`SELECT moemoepoint FROM users WHERE id = ?`, id).Scan(&mirror).Error; err != nil {
		t.Fatalf("mirror: %v", err)
	}
	return ledger, mirror
}

func wantBalance(t *testing.T, l *service.Ledger, id uint, want int64) {
	t.Helper()
	got, mirror := balances(t, l, id)
	if got != want || mirror != want {
		t.Fatalf("balance: ledger=%d users.moemoepoint=%d, want %d", got, mirror, want)
	}
}

// audit asserts the two invariants every writer must keep: each transfer's
// entries cancel out, and each account holds the sum of its entries.
func audit(t *testing.T) {
	t.Helper()
	var unbalanced, drifted int64
	testDB.Raw(`SELECT COUNT(*) FROM (SELECT transfer_id FROM ledger_entries
		GROUP BY transfer_id HAVING SUM(amount) <> 0) x`).Scan(&unbalanced)
	testDB.Raw(`SELECT COUNT(*) FROM ledger_accounts a
		WHERE a.balance <> COALESCE((SELECT SUM(amount) FROM ledger_entries e WHERE e.account_id = a.id), 0)`).
		Scan(&drifted)
	if unbalanced != 0 || drifted != 0 {
		t.Fatalf("ledger invariants broken: %d unbalanced transfers, %d drifted accounts", unbalanced, drifted)
	}
}

func key(t *testing.T, parts ...any) string {
	return fmt.Sprintf("test:%s:%v", t.Name(), parts)
}

func TestAwardChargeAndHistory(t *testing.T) {
	l := service.New(testDB)
	ctx := context.Background()
	u := newUser(t, 0)

	res, err := l.Award(ctx, service.Award{UserID: u, Delta: 10, Reason: model.ReasonContentApproved,
		SourceApp: "forum-test", Ref: "topic:1", IdempotencyKey: key(t, "award")})
	if err != nil || !res.Applied || res.UserBalance(u) != 10 {
		t.Fatalf("award: res=%+v err=%v", res, err)
	}
	res, err = l.Charge(ctx, service.Charge{UserID: u, Amount: 4, Reason: model.ReasonSpend,
		SourceApp: "forum-test", Ref: "topic_upvote:1", ActorUserID: u, IdempotencyKey: key(t, "charge")})
	if err != nil || !res.Applied || res.UserBalance(u) != 6 {
		t.Fatalf("charge: res=%+v err=%v", res, err)
	}
	wantBalance(t, l, u, 6)

	items, more, err := l.UserHistory(ctx, u, 10, 0, "")
	if err != nil || more || len(items) != 2 {
		t.Fatalf("history: %d items more=%v err=%v", len(items), more, err)
	}
	if items[0].Delta != -4 || items[0].BalanceAfter != 6 || items[0].Reason != model.ReasonSpend ||
		items[1].Delta != 10 || items[1].BalanceAfter != 10 || items[1].Ref != "topic:1" {
		t.Fatalf("history rows: %+v", items)
	}
	filtered, _, _ := l.UserHistory(ctx, u, 10, 0, model.ReasonSpend)
	if len(filtered) != 1 {
		t.Fatalf("reason filter returned %d rows", len(filtered))
	}
	audit(t)
}

func TestChargeRefusesWhatTheUserDoesNotHave(t *testing.T) {
	l := service.New(testDB)
	ctx := context.Background()
	u := newUser(t, 0)
	ledgertest.Fund(t, l, u, 5)

	_, err := l.Charge(ctx, service.Charge{UserID: u, Amount: 6, Reason: model.ReasonSpend,
		SourceApp: "forum-test", IdempotencyKey: key(t, 1)})
	if !errors.Is(err, errors.ErrMoemoepointInsufficient) {
		t.Fatalf("got %v, want ErrMoemoepointInsufficient", err)
	}
	wantBalance(t, l, u, 5)
	if items, _, _ := l.UserHistory(ctx, u, 10, 0, model.ReasonSpend); len(items) != 0 {
		t.Fatalf("a refused charge left %d rows", len(items))
	}

	if _, err := l.Charge(ctx, service.Charge{UserID: u, Amount: 5, Reason: model.ReasonSpend,
		SourceApp: "forum-test", IdempotencyKey: key(t, 2)}); err != nil {
		t.Fatalf("spending the whole balance: %v", err)
	}
	wantBalance(t, l, u, 0)
	audit(t)
}

func TestClawBackMayGoNegative(t *testing.T) {
	l := service.New(testDB)
	u := newUser(t, 0)
	ledgertest.Fund(t, l, u, 3)
	if _, err := l.Award(context.Background(), service.Award{UserID: u, Delta: -5,
		Reason: model.ReasonContentRemoved, SourceApp: "forum-test", IdempotencyKey: key(t)}); err != nil {
		t.Fatalf("claw-back: %v", err)
	}
	wantBalance(t, l, u, -2)
	audit(t)
}

func TestUnknownUserIsNotFound(t *testing.T) {
	l := service.New(testDB)
	_, err := l.Award(context.Background(), service.Award{UserID: 1 << 30, Delta: 1,
		Reason: model.ReasonLiked, SourceApp: "forum-test", IdempotencyKey: key(t)})
	if !errors.Is(err, errors.ErrAuthUserNotFound) {
		t.Fatalf("got %v, want ErrAuthUserNotFound", err)
	}
}

func TestReplayAnswersAndConflictRefuses(t *testing.T) {
	l := service.New(testDB)
	ctx := context.Background()
	u := newUser(t, 0)
	award := service.Award{UserID: u, Delta: 3, Reason: model.ReasonLiked,
		SourceApp: "forum-test", Ref: "topic:9", IdempotencyKey: key(t)}

	for i, wantApplied := range []bool{true, false, false} {
		res, err := l.Award(ctx, award)
		if err != nil || res.Applied != wantApplied || res.UserBalance(u) != 3 {
			t.Fatalf("attempt %d: res=%+v err=%v", i, res, err)
		}
	}

	changed := award
	changed.Delta = 4
	if _, err := l.Award(ctx, changed); !errors.Is(err, errors.ErrMoemoepointIdemConflict) {
		t.Fatalf("reused key with another delta: got %v, want ErrMoemoepointIdemConflict", err)
	}

	// Keys are scoped to the client that sent them: another site choosing
	// the same string is a different transfer, not a replay or a conflict.
	other := award
	other.SourceApp = "patch-test"
	if res, err := l.Award(ctx, other); err != nil || !res.Applied || res.UserBalance(u) != 6 {
		t.Fatalf("same key from another client: res=%+v err=%v", res, err)
	}
	wantBalance(t, l, u, 6)
	audit(t)
}

func TestReverseUndoesOnce(t *testing.T) {
	l := service.New(testDB)
	ctx := context.Background()
	u := newUser(t, 0)
	stranger := newUser(t, 0)
	ledgertest.Fund(t, l, u, 10)

	chargeKey := key(t, "charge")
	if _, err := l.Charge(ctx, service.Charge{UserID: u, Amount: 4, Reason: model.ReasonSpend,
		SourceApp: "forum-test", IdempotencyKey: chargeKey}); err != nil {
		t.Fatalf("charge: %v", err)
	}

	if _, err := l.Reverse(ctx, service.Reversal{SourceApp: "forum-test", IdempotencyKey: chargeKey,
		UserID: stranger}); !errors.Is(err, errors.ErrMoemoepointTransferNotFound) {
		t.Fatalf("reversal naming a non-party: got %v, want ErrMoemoepointTransferNotFound", err)
	}
	if _, err := l.Reverse(ctx, service.Reversal{SourceApp: "patch-test", IdempotencyKey: chargeKey,
		UserID: u}); !errors.Is(err, errors.ErrMoemoepointTransferNotFound) {
		t.Fatalf("reversal by another client: got %v, want ErrMoemoepointTransferNotFound", err)
	}

	res, err := l.Reverse(ctx, service.Reversal{SourceApp: "forum-test", IdempotencyKey: chargeKey,
		UserID: u, Note: "first"})
	if err != nil || !res.Applied || res.UserBalance(u) != 10 {
		t.Fatalf("reverse: res=%+v err=%v", res, err)
	}
	res, err = l.Reverse(ctx, service.Reversal{SourceApp: "forum-test", IdempotencyKey: chargeKey,
		UserID: u, Note: "a retry with another note"})
	if err != nil || res.Applied || res.UserBalance(u) != 10 {
		t.Fatalf("second reverse: res=%+v err=%v", res, err)
	}
	wantBalance(t, l, u, 10)

	var originalID int64
	testDB.Raw(`SELECT reverses_id FROM ledger_transfers WHERE id = ?`, res.TransferID).Scan(&originalID)
	reversalKey := "reversal:" + strconv.FormatInt(originalID, 10)
	if _, err := l.Reverse(ctx, service.Reversal{SourceApp: "forum-test", IdempotencyKey: reversalKey,
		UserID: u}); !errors.Is(err, errors.ErrMoemoepointNotReversible) {
		t.Fatalf("reversing a reversal: got %v, want ErrMoemoepointNotReversible", err)
	}
	audit(t)
}

func TestConcurrentChargesNeverOverdraw(t *testing.T) {
	l := service.New(testDB)
	u := newUser(t, 0)
	ledgertest.Fund(t, l, u, 50)

	var ok, refused atomic.Int64
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := l.Charge(context.Background(), service.Charge{UserID: u, Amount: 10,
				Reason: model.ReasonSpend, SourceApp: "forum-test", IdempotencyKey: key(t, i)})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, errors.ErrMoemoepointInsufficient):
				refused.Add(1)
			default:
				t.Errorf("charge %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 5 || refused.Load() != 15 {
		t.Fatalf("%d charges succeeded and %d were refused, want 5 and 15", ok.Load(), refused.Load())
	}
	wantBalance(t, l, u, 0)
	audit(t)
}

// A rename locks the users row before it charges; an award takes the ledger's
// own locks. Both must take them in the same order, or the two deadlock.
func TestChargeUnderAHeldUserLockDoesNotDeadlockAwards(t *testing.T) {
	l := service.New(testDB)
	u := newUser(t, 0)
	ledgertest.Fund(t, l, u, 1000)

	var wg sync.WaitGroup
	for i := range 15 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			err := testDB.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT id FROM users WHERE id = ? FOR UPDATE`, u).Error; err != nil {
					return err
				}
				if err := tx.Exec(`UPDATE users SET bio = ? WHERE id = ?`, strconv.Itoa(i), u).Error; err != nil {
					return err
				}
				_, err := l.ChargeTx(context.Background(), tx, service.Charge{UserID: u, Amount: 1,
					Reason: model.ReasonNameChange, SourceApp: "oauth", IdempotencyKey: key(t, "rename", i)})
				return err
			})
			if err != nil {
				t.Errorf("rename-shaped charge %d: %v", i, err)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := l.Award(context.Background(), service.Award{UserID: u, Delta: 1,
				Reason: model.ReasonLiked, SourceApp: "forum-test", IdempotencyKey: key(t, "like", i)}); err != nil {
				t.Errorf("award %d: %v", i, err)
			}
		}()
	}
	wg.Wait()
	wantBalance(t, l, u, 1000)
	audit(t)
}
