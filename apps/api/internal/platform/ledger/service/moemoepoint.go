package service

import (
	"context"
	"time"

	"api/internal/platform/ledger/model"
	"api/pkg/errors"

	"gorm.io/gorm"
)

type Award struct {
	UserID         uint
	Delta          int64
	Reason         string
	SourceApp      string
	Ref            string
	ActorUserID    uint
	IdempotencyKey string
	Note           string
}

func (a Award) transfer() Transfer {
	return Transfer{
		Reason:         a.Reason,
		SourceApp:      a.SourceApp,
		IdempotencyKey: a.IdempotencyKey,
		Ref:            a.Ref,
		ActorUserID:    a.ActorUserID,
		Note:           a.Note,
		Legs: []Leg{
			{Account: UserAccount(a.UserID), Amount: a.Delta},
			{Account: IssuerAccount(a.SourceApp), Amount: -a.Delta},
		},
	}
}

type Charge struct {
	UserID         uint
	Amount         int64
	Reason         string
	SourceApp      string
	Ref            string
	ActorUserID    uint
	IdempotencyKey string
	Note           string
}

func (c Charge) transfer() Transfer {
	return Transfer{
		Reason:         c.Reason,
		SourceApp:      c.SourceApp,
		IdempotencyKey: c.IdempotencyKey,
		Ref:            c.Ref,
		ActorUserID:    c.ActorUserID,
		Note:           c.Note,
		Funded:         true,
		Legs: []Leg{
			{Account: UserAccount(c.UserID), Amount: -c.Amount},
			{Account: SinkAccount(c.SourceApp), Amount: c.Amount},
		},
	}
}

func (l *Ledger) Award(ctx context.Context, a Award) (*Posted, error) {
	return l.Post(ctx, a.transfer())
}

func (l *Ledger) Charge(ctx context.Context, c Charge) (*Posted, error) {
	if c.Amount <= 0 {
		return nil, errors.NewWithCode(errors.ErrMoemoepointInvalidDelta)
	}
	return l.Post(ctx, c.transfer())
}

func (l *Ledger) ChargeTx(ctx context.Context, tx *gorm.DB, c Charge) (*Posted, error) {
	if c.Amount <= 0 {
		return nil, errors.NewWithCode(errors.ErrMoemoepointInvalidDelta)
	}
	return l.PostTx(ctx, tx, c.transfer())
}

func (l *Ledger) userAccountID(tx *gorm.DB, userID uint) (int64, error) {
	var ids []int64
	err := tx.Model(&model.Account{}).Where("asset = ? AND kind = ? AND user_id = ? AND code = ''",
		model.AssetMoemoepoint, model.KindUser, userID).Limit(1).Pluck("id", &ids).Error
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	return ids[0], nil
}

// CountUserTransfersTx numbers a user's transfers of one reason so a
// repeatable paid action can build a stable idempotency key. Keying such a
// charge on its content instead — user plus target name — looks stable and is
// a loophole: renaming A→B→A→B would find the first key already present and
// hand out the fourth rename for free. The caller must already hold the user
// row lock; the count is only stable underneath it.
func (l *Ledger) CountUserTransfersTx(ctx context.Context, tx *gorm.DB, userID uint, reason string) (int64, error) {
	tx = tx.WithContext(ctx)
	accountID, err := l.userAccountID(tx, userID)
	if err != nil || accountID == 0 {
		return 0, err
	}
	var n int64
	err = tx.Table("ledger_entries e").
		Joins("JOIN ledger_transfers t ON t.id = e.transfer_id").
		Where("e.account_id = ? AND t.reason = ?", accountID, reason).
		Count(&n).Error
	return n, err
}

func (l *Ledger) UserBalance(ctx context.Context, userID uint) (int64, error) {
	var row struct {
		ID      uint
		Balance int64
	}
	err := l.db.WithContext(ctx).Raw(`
		SELECT u.id, COALESCE(a.balance, 0) AS balance
		FROM users u
		LEFT JOIN ledger_accounts a
		  ON a.asset = ? AND a.kind = ? AND a.user_id = u.id AND a.code = ''
		WHERE u.id = ? AND u.deleted_at IS NULL`,
		model.AssetMoemoepoint, model.KindUser, userID).Scan(&row).Error
	if err != nil {
		return 0, err
	}
	if row.ID == 0 {
		return 0, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return row.Balance, nil
}

type HistoryItem struct {
	ID           int64
	Delta        int64
	BalanceAfter int64
	Reason       string
	SourceApp    string
	Ref          string
	Note         string
	ActorUserID  uint
	CreatedAt    time.Time
}

func (l *Ledger) UserHistory(ctx context.Context, userID uint, limit int, beforeID int64, reason string) ([]HistoryItem, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	db := l.db.WithContext(ctx)
	accountID, err := l.userAccountID(db, userID)
	if err != nil || accountID == 0 {
		return nil, false, err
	}
	q := db.Table("ledger_entries e").
		Select(`e.id, e.amount AS delta, e.balance_after, t.reason, t.source_app,
			t.ref, t.note, t.actor_user_id, e.created_at`).
		Joins("JOIN ledger_transfers t ON t.id = e.transfer_id").
		Where("e.account_id = ?", accountID)
	if beforeID > 0 {
		q = q.Where("e.id < ?", beforeID)
	}
	if reason != "" {
		q = q.Where("t.reason = ?", reason)
	}
	var rows []HistoryItem
	if err := q.Order("e.id DESC").Limit(limit + 1).Scan(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

func (l *Ledger) SourceNames(ctx context.Context, sourceApps []string) map[string]string {
	seen := make(map[string]struct{}, len(sourceApps))
	ids := make([]string, 0, len(sourceApps))
	for _, a := range sourceApps {
		if a == "" || a == "oauth" {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		ids = append(ids, a)
	}
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out
	}
	var rows []struct {
		ID   string
		Name string
	}
	if err := l.db.WithContext(ctx).Table("oauth_clients").
		Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out
}
