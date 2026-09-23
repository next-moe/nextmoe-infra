// Package ledgertest seeds and removes ledger state for DB-backed suites that
// share one test database.
package ledgertest

import (
	"context"
	"fmt"
	"testing"

	authModel "api/internal/platform/auth/model"
	"api/internal/platform/ledger/model"
	"api/internal/platform/ledger/service"
	siteModel "api/internal/platform/site/model"

	"gorm.io/gorm"
)

func Migrate(db *gorm.DB) error {
	if err := db.AutoMigrate(append([]any{&authModel.User{}, &siteModel.Role{}}, model.AllModels()...)...); err != nil {
		return err
	}
	// Other suites sharing this database insert users with explicit ids and
	// never advance the sequence, so the next generated id can land on a row
	// that exists: "duplicate key value violates unique constraint users_pkey"
	// failed the shop suite in CI on 2026-09-23.
	return db.Exec(`SELECT setval(pg_get_serial_sequence('users', 'id'),
		GREATEST((SELECT COALESCE(MAX(id), 0) FROM users), 1))`).Error
}

// Fund gives a test user a starting balance the way production would, through
// the ledger; writing users.moemoepoint directly leaves an account the next
// transfer opens at zero.
func Fund(t *testing.T, l *service.Ledger, userID uint, amount int64) {
	t.Helper()
	if amount == 0 {
		return
	}
	if _, err := l.Award(context.Background(), service.Award{
		UserID:         userID,
		Delta:          amount,
		Reason:         model.ReasonAdminGrant,
		SourceApp:      "oauth",
		IdempotencyKey: fmt.Sprintf("test:fund:%d", userID),
	}); err != nil {
		t.Fatalf("fund user %d: %v", userID, err)
	}
}

// Purge removes every transfer a user took part in, whole, and re-derives the
// system balances those transfers touched, so a suite that runs later against
// the same database still finds a ledger whose transfers balance.
func Purge(db *gorm.DB, userIDs ...uint) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var transferIDs []int64
		if err := tx.Raw(`
			SELECT DISTINCT e.transfer_id FROM ledger_entries e
			JOIN ledger_accounts a ON a.id = e.account_id
			WHERE a.kind = ? AND a.user_id IN ?`, model.KindUser, userIDs).
			Scan(&transferIDs).Error; err != nil {
			return err
		}
		if len(transferIDs) > 0 {
			if err := tx.Exec(`DELETE FROM ledger_entries WHERE transfer_id IN ?`, transferIDs).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DELETE FROM ledger_transfers WHERE id IN ?`, transferIDs).Error; err != nil {
				return err
			}
		}
		if err := tx.Exec(`DELETE FROM ledger_accounts WHERE kind = ? AND user_id IN ?`,
			model.KindUser, userIDs).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM moemoepoint_log WHERE user_id IN ?`, userIDs).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE ledger_accounts a
			SET balance = COALESCE((SELECT SUM(e.amount) FROM ledger_entries e WHERE e.account_id = a.id), 0)
			WHERE a.kind <> ?`, model.KindUser).Error
	})
}
