package service

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"api/internal/platform/auth/dto"
	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/internal/platform/ledger/ledgertest"
	ledgerModel "api/internal/platform/ledger/model"
	ledgerService "api/internal/platform/ledger/service"
	"api/pkg/config"
	"api/pkg/errors"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func renameTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_DSN to run (scripts/ephemeral-test-db.sh create <slug>)")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := ledgertest.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func renameTestService(t *testing.T, db *gorm.DB) *AuthService {
	t.Helper()
	userRepo := repository.NewUserRepository(db)
	svc := NewAuthService(userRepo, nil, config.JWTConfig{})
	return svc.WithLedger(ledgerService.New(db))
}

var renameTestSeq int64

func seedRenameUser(t *testing.T, db *gorm.DB, balance int) *model.User {
	t.Helper()
	renameTestSeq++
	tag := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.FormatInt(renameTestSeq, 10)
	u := &model.User{
		Name:  "rc" + tag[len(tag)-12:],
		Email: "rc-" + tag + "@test.local",
	}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_ = ledgertest.Purge(db, u.ID)
		db.Exec("DELETE FROM users WHERE id = ?", u.ID)
	})
	ledgertest.Fund(t, ledgerService.New(db), u.ID, int64(balance))
	if err := db.First(u, u.ID).Error; err != nil {
		t.Fatalf("reload seeded user: %v", err)
	}
	return u
}

func renameState(t *testing.T, db *gorm.DB, id uint) (name string, balance int, charges int64) {
	t.Helper()
	var u model.User
	if err := db.First(&u, id).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	charges, err := ledgerService.New(db).CountUserTransfersTx(context.Background(), db, id, ledgerModel.ReasonNameChange)
	if err != nil {
		t.Fatalf("count charges: %v", err)
	}
	ledgerBalance, err := ledgerService.New(db).UserBalance(context.Background(), id)
	if err != nil {
		t.Fatalf("ledger balance: %v", err)
	}
	if ledgerBalance != int64(u.Moemoepoint) {
		t.Fatalf("users.moemoepoint=%d drifted from the ledger's %d", u.Moemoepoint, ledgerBalance)
	}
	return u.Name, u.Moemoepoint, charges
}

func rename(svc *AuthService, uuid, to string) error {
	_, err := svc.UpdateProfile(context.Background(), uuid, &dto.UpdateProfileRequest{Name: &to})
	return err
}

// The charge and the rename must be one transaction. The forum's old endpoint
// charged for a rename, its Go rewrite proxied to PATCH /auth/me, and the charge
// was silently lost — so every assertion here is paired: the name AND the
// balance, never one without the other.
func TestRenameChargesTheBalanceAndTheNameTogether(t *testing.T) {
	db := renameTestDB(t)
	svc := renameTestService(t, db)
	cost := 17

	t.Run("a rename charges once", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		want := u.Name + "x"
		if err := rename(svc, u.UUID, want); err != nil {
			t.Fatalf("rename: %v", err)
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != want || balance != 100-cost || charges != 1 {
			t.Fatalf("got name=%q balance=%d charges=%d, want %q/%d/1", name, balance, charges, want, 100-cost)
		}
	})

	t.Run("retrying the same rename does not charge twice", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		want := u.Name + "x"
		for i := 0; i < 3; i++ {
			if err := rename(svc, u.UUID, want); err != nil {
				t.Fatalf("rename attempt %d: %v", i, err)
			}
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != want || balance != 100-cost || charges != 1 {
			t.Fatalf("got name=%q balance=%d charges=%d, want one charge only", name, balance, charges)
		}
	})

	// Keying the charge on (user, target name) would look stable and would hand
	// this fourth rename out for free, because it repeats the first key.
	t.Run("renaming back and forth charges every time", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		a, b := u.Name+"a", u.Name+"b"
		for i, to := range []string{a, b, a, b} {
			if err := rename(svc, u.UUID, to); err != nil {
				t.Fatalf("rename %d to %q: %v", i, to, err)
			}
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != b || balance != 100-4*cost || charges != 4 {
			t.Fatalf("got name=%q balance=%d charges=%d, want %q/%d/4", name, balance, charges, b, 100-4*cost)
		}
	})

	t.Run("an unaffordable rename does not happen at all", func(t *testing.T) {
		u := seedRenameUser(t, db, cost-1)
		err := rename(svc, u.UUID, u.Name+"x")
		if !errors.Is(err, errors.ErrMoemoepointInsufficient) {
			t.Fatalf("got %v, want ErrMoemoepointInsufficient", err)
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != u.Name || balance != cost-1 || charges != 0 {
			t.Fatalf("refused rename left state changed: name=%q balance=%d charges=%d", name, balance, charges)
		}
	})

	t.Run("a name someone else holds is rejected before any charge", func(t *testing.T) {
		taken := seedRenameUser(t, db, 0)
		u := seedRenameUser(t, db, 100)
		err := rename(svc, u.UUID, taken.Name)
		if !errors.Is(err, errors.ErrAuthNameExists) {
			t.Fatalf("got %v, want ErrAuthNameExists", err)
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != u.Name || balance != 100 || charges != 0 {
			t.Fatalf("rejected rename left state changed: name=%q balance=%d charges=%d", name, balance, charges)
		}
	})

	t.Run("keeping your own name is not a rename", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		if err := rename(svc, u.UUID, u.Name); err != nil {
			t.Fatalf("rename to same name: %v", err)
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != u.Name || balance != 100 || charges != 0 {
			t.Fatalf("same-name update charged: name=%q balance=%d charges=%d", name, balance, charges)
		}
	})

	// The other profile fields used to be written by a second statement after
	// the charge had already committed; they now share its transaction, so a
	// request that carries both must apply both.
	t.Run("a rename carrying other fields applies all of them", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		want := u.Name + "x"
		bio := "charged rename"
		if _, err := svc.UpdateProfile(context.Background(), u.UUID,
			&dto.UpdateProfileRequest{Name: &want, Bio: &bio}); err != nil {
			t.Fatalf("update: %v", err)
		}
		var got model.User
		if err := db.First(&got, u.ID).Error; err != nil {
			t.Fatalf("reload: %v", err)
		}
		if got.Name != want || got.Bio != bio || got.Moemoepoint != 100-cost {
			t.Fatalf("got name=%q bio=%q balance=%d", got.Name, got.Bio, got.Moemoepoint)
		}
	})

	// The separating case. While the charge committed on its own and the other
	// fields were written afterwards, a failure in that second write left the
	// user renamed and charged while the request reported failure. bio is
	// varchar(107), so an over-long one is a real database error arriving after
	// the point where the charge used to be final.
	t.Run("a failure after the charge rolls the charge back", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		tooLong := make([]byte, 200)
		for i := range tooLong {
			tooLong[i] = 'x'
		}
		bio := string(tooLong)
		want := u.Name + "x"
		if _, err := svc.UpdateProfile(context.Background(), u.UUID,
			&dto.UpdateProfileRequest{Name: &want, Bio: &bio}); err == nil {
			t.Fatal("expected the over-long bio to fail the update")
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != u.Name || balance != 100 || charges != 0 {
			t.Fatalf("failed update left the rename applied: name=%q balance=%d charges=%d", name, balance, charges)
		}
	})

	t.Run("a profile edit without a name is free", func(t *testing.T) {
		u := seedRenameUser(t, db, 100)
		bio := "no rename here"
		if _, err := svc.UpdateProfile(context.Background(), u.UUID,
			&dto.UpdateProfileRequest{Bio: &bio}); err != nil {
			t.Fatalf("update: %v", err)
		}
		name, balance, charges := renameState(t, db, u.ID)
		if name != u.Name || balance != 100 || charges != 0 {
			t.Fatalf("bio-only edit charged: name=%q balance=%d charges=%d", name, balance, charges)
		}
	})
}

// A ledger row must stay readable as "why was this user charged", which is what
// a nonce idempotency key destroys.
func TestRenameChargeIsAuditable(t *testing.T) {
	db := renameTestDB(t)
	svc := renameTestService(t, db)

	u := seedRenameUser(t, db, 100)
	from, to := u.Name, u.Name+"x"
	if err := rename(svc, u.UUID, to); err != nil {
		t.Fatalf("rename: %v", err)
	}

	var charge struct {
		IdempotencyKey string
		SourceApp      string
		ActorUserID    uint
		Note           string
		Amount         int64
	}
	if err := db.Raw(`
		SELECT t.idempotency_key, t.source_app, t.actor_user_id, t.note, e.amount
		FROM ledger_transfers t
		JOIN ledger_entries e ON e.transfer_id = t.id
		JOIN ledger_accounts a ON a.id = e.account_id
		WHERE a.kind = 'user' AND a.user_id = ? AND t.reason = ?`,
		u.ID, ledgerModel.ReasonNameChange).Scan(&charge).Error; err != nil {
		t.Fatalf("load charge: %v", err)
	}
	wantKey := fmt.Sprintf("oauth:name_change:%d:1", u.ID)
	if charge.IdempotencyKey != wantKey {
		t.Errorf("idempotency key = %q, want %q", charge.IdempotencyKey, wantKey)
	}
	if charge.Amount != -17 || charge.SourceApp != "oauth" || charge.ActorUserID != u.ID {
		t.Errorf("amount=%d source_app=%q actor=%d", charge.Amount, charge.SourceApp, charge.ActorUserID)
	}
	if charge.Note != from+" → "+to {
		t.Errorf("note = %q, want %q", charge.Note, from+" → "+to)
	}
}
