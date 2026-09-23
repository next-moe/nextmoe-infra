package service

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"time"

	"api/internal/platform/ledger/model"
	"api/pkg/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxLegAmount = 1_000_000

type AccountRef struct {
	Kind   string
	UserID uint
	Code   string
}

func UserAccount(id uint) AccountRef      { return AccountRef{Kind: model.KindUser, UserID: id} }
func IssuerAccount(app string) AccountRef { return AccountRef{Kind: model.KindIssuer, Code: app} }
func SinkAccount(app string) AccountRef   { return AccountRef{Kind: model.KindSink, Code: app} }

type Leg struct {
	Account AccountRef
	Amount  int64
}

type Transfer struct {
	Asset          string
	Reason         string
	SourceApp      string
	IdempotencyKey string
	Ref            string
	ActorUserID    uint
	Note           string
	Legs           []Leg
	// Funded refuses the transfer when it would leave a user it debits below
	// zero. A claw-back is deliberately not funded: taking back what was
	// wrongly given must not fail on exactly the users who spent it.
	Funded bool

	reverses *int64
}

type Posted struct {
	TransferID int64
	Applied    bool
	balances   map[AccountRef]int64
}

func (p *Posted) UserBalance(id uint) int64 { return p.balances[UserAccount(id)] }

type Ledger struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Ledger {
	return &Ledger{db: db}
}

func (l *Ledger) Post(ctx context.Context, t Transfer) (*Posted, error) {
	var out *Posted
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = l.PostTx(ctx, tx, t)
		return err
	})
	return out, err
}

func (l *Ledger) PostTx(ctx context.Context, tx *gorm.DB, t Transfer) (*Posted, error) {
	if t.Asset == "" {
		t.Asset = model.AssetMoemoepoint
	}
	if err := t.validate(); err != nil {
		return nil, err
	}
	tx = tx.WithContext(ctx)

	// Users before accounts, each set in id order: renaming holds the users
	// row when it charges, so taking the account first here would deadlock
	// against it.
	if err := lockUsers(tx, t.userIDs()); err != nil {
		return nil, err
	}

	existing, err := findTransfer(tx, t.SourceApp, t.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return replay(tx, existing, t)
	}

	accounts, err := lockAccounts(tx, t.Asset, t.refs())
	if err != nil {
		return nil, err
	}
	balances := make(map[AccountRef]int64, len(t.Legs))
	for _, leg := range t.Legs {
		next := accounts[leg.Account].Balance + leg.Amount
		if t.Funded && leg.Account.Kind == model.KindUser && leg.Amount < 0 && next < 0 {
			return nil, errors.NewWithCode(errors.ErrMoemoepointInsufficient)
		}
		balances[leg.Account] = next
	}

	now := time.Now()
	row := model.Transfer{
		Asset:          t.Asset,
		Reason:         t.Reason,
		SourceApp:      t.SourceApp,
		IdempotencyKey: t.IdempotencyKey,
		Ref:            t.Ref,
		ActorUserID:    t.ActorUserID,
		Note:           t.Note,
		ReversesID:     t.reverses,
		CreatedAt:      now,
	}
	res := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_app"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(&row)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		winner, err := findTransfer(tx, t.SourceApp, t.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if winner == nil {
			return nil, fmt.Errorf("ledger: transfer %s/%s vanished after a conflict", t.SourceApp, t.IdempotencyKey)
		}
		return replay(tx, winner, t)
	}

	entries := make([]model.Entry, len(t.Legs))
	for i, leg := range t.Legs {
		entries[i] = model.Entry{
			TransferID:   row.ID,
			AccountID:    accounts[leg.Account].ID,
			Amount:       leg.Amount,
			BalanceAfter: balances[leg.Account],
			CreatedAt:    now,
		}
	}
	if err := tx.Create(&entries).Error; err != nil {
		return nil, err
	}
	for _, leg := range t.Legs {
		if err := tx.Model(&model.Account{}).Where("id = ?", accounts[leg.Account].ID).
			Updates(map[string]any{"balance": balances[leg.Account], "updated_at": now}).Error; err != nil {
			return nil, err
		}
		if leg.Account.Kind == model.KindUser && t.Asset == model.AssetMoemoepoint {
			if err := mirrorUserBalance(tx, leg.Account.UserID, balances[leg.Account], now); err != nil {
				return nil, err
			}
		}
	}
	return &Posted{TransferID: row.ID, Applied: true, balances: balances}, nil
}

// users.moemoepoint is what /auth/me, userinfo, the admin user list and its
// sort read. It is written here and nowhere else, in the same transaction as
// the account, so it cannot drift from it.
func mirrorUserBalance(tx *gorm.DB, userID uint, balance int64, now time.Time) error {
	return tx.Exec(`UPDATE users SET moemoepoint = ?, updated_at = ? WHERE id = ?`,
		balance, now, userID).Error
}

func (t *Transfer) validate() error {
	if !model.IsValidReason(t.Reason) {
		return errors.NewWithCode(errors.ErrMoemoepointInvalidReason)
	}
	if t.IdempotencyKey == "" || t.SourceApp == "" {
		return errors.NewWithCode(errors.ErrMissingParam)
	}
	if len(t.Legs) < 2 {
		return fmt.Errorf("ledger: a transfer needs at least two legs, got %d", len(t.Legs))
	}
	var sum int64
	seen := make(map[AccountRef]struct{}, len(t.Legs))
	for _, leg := range t.Legs {
		if leg.Amount == 0 || leg.Amount > maxLegAmount || leg.Amount < -maxLegAmount {
			return errors.NewWithCode(errors.ErrMoemoepointInvalidDelta)
		}
		if _, dup := seen[leg.Account]; dup {
			return fmt.Errorf("ledger: account %+v appears twice in one transfer", leg.Account)
		}
		seen[leg.Account] = struct{}{}
		sum += leg.Amount
	}
	if sum != 0 {
		return fmt.Errorf("ledger: transfer legs sum to %d, not 0", sum)
	}
	return nil
}

func (t *Transfer) refs() []AccountRef {
	out := make([]AccountRef, len(t.Legs))
	for i, leg := range t.Legs {
		out[i] = leg.Account
	}
	return out
}

func (t *Transfer) userIDs() []uint {
	var ids []uint
	for _, leg := range t.Legs {
		if leg.Account.Kind == model.KindUser {
			ids = append(ids, leg.Account.UserID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

func lockUsers(tx *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	var found []uint
	if err := tx.Raw(`SELECT id FROM users WHERE id IN ? AND deleted_at IS NULL ORDER BY id FOR UPDATE`, ids).
		Scan(&found).Error; err != nil {
		return err
	}
	if len(found) != len(ids) {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return nil
}

func lockAccounts(tx *gorm.DB, asset string, refs []AccountRef) (map[AccountRef]*model.Account, error) {
	keys := make([][]any, len(refs))
	for i, r := range refs {
		keys[i] = []any{r.Kind, r.UserID, r.Code}
	}
	lock := func() (map[AccountRef]*model.Account, error) {
		var rows []model.Account
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("asset = ? AND (kind, user_id, code) IN ?", asset, keys).
			Order("id").Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[AccountRef]*model.Account, len(rows))
		for i := range rows {
			a := &rows[i]
			out[AccountRef{Kind: a.Kind, UserID: a.UserID, Code: a.Code}] = a
		}
		return out, nil
	}
	out, err := lock()
	if err != nil || len(out) == len(refs) {
		return out, err
	}
	var missing []model.Account
	for _, r := range refs {
		if _, ok := out[r]; !ok {
			missing = append(missing, model.Account{Asset: asset, Kind: r.Kind, UserID: r.UserID, Code: r.Code})
		}
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&missing).Error; err != nil {
		return nil, err
	}
	if out, err = lock(); err != nil {
		return nil, err
	}
	if len(out) != len(refs) {
		return nil, fmt.Errorf("ledger: resolved %d of %d accounts", len(out), len(refs))
	}
	return out, nil
}

func findTransfer(tx *gorm.DB, sourceApp, key string) (*model.Transfer, error) {
	var rows []model.Transfer
	if err := tx.Where("source_app = ? AND idempotency_key = ?", sourceApp, key).
		Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

type legRow struct {
	Kind   string
	UserID uint
	Code   string
	Amount int64
}

func transferLegs(tx *gorm.DB, transferID int64) ([]legRow, error) {
	var rows []legRow
	err := tx.Raw(`
		SELECT a.kind, a.user_id, a.code, e.amount
		FROM ledger_entries e JOIN ledger_accounts a ON a.id = e.account_id
		WHERE e.transfer_id = ?`, transferID).Scan(&rows).Error
	return rows, err
}

func replay(tx *gorm.DB, existing *model.Transfer, t Transfer) (*Posted, error) {
	legs, err := transferLegs(tx, existing.ID)
	if err != nil {
		return nil, err
	}
	sameReversal := t.reverses != nil && existing.ReversesID != nil && *existing.ReversesID == *t.reverses
	same := sameReversal || existing.Reason == t.Reason && existing.Ref == t.Ref &&
		existing.Note == t.Note && existing.ActorUserID == t.ActorUserID
	want := make(map[AccountRef]int64, len(t.Legs))
	for _, leg := range t.Legs {
		want[leg.Account] = leg.Amount
	}
	if len(legs) != len(t.Legs) {
		same = false
	}
	for _, r := range legs {
		if want[AccountRef{Kind: r.Kind, UserID: r.UserID, Code: r.Code}] != r.Amount {
			same = false
		}
	}
	if !same {
		return nil, errors.NewWithCode(errors.ErrMoemoepointIdemConflict)
	}
	balances, err := currentBalances(tx, existing.Asset, t.refs())
	if err != nil {
		return nil, err
	}
	return &Posted{TransferID: existing.ID, Applied: false, balances: balances}, nil
}

func currentBalances(tx *gorm.DB, asset string, refs []AccountRef) (map[AccountRef]int64, error) {
	keys := make([][]any, len(refs))
	for i, r := range refs {
		keys[i] = []any{r.Kind, r.UserID, r.Code}
	}
	var rows []model.Account
	if err := tx.Where("asset = ? AND (kind, user_id, code) IN ?", asset, keys).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[AccountRef]int64, len(rows))
	for _, a := range rows {
		out[AccountRef{Kind: a.Kind, UserID: a.UserID, Code: a.Code}] = a.Balance
	}
	return out, nil
}

type Reversal struct {
	SourceApp      string
	IdempotencyKey string
	PartyUserID    uint
	ActorUserID    uint
	Note           string
}

// A transfer is reversed at most once: the reversal's key is derived from the
// original, so asking again, with any note, answers with the first reversal.
func (l *Ledger) ReverseTx(ctx context.Context, tx *gorm.DB, r Reversal) (*Posted, error) {
	tx = tx.WithContext(ctx)
	original, err := findTransfer(tx, r.SourceApp, r.IdempotencyKey)
	if err != nil {
		return nil, err
	}
	if original == nil {
		return nil, errors.NewWithCode(errors.ErrMoemoepointTransferNotFound)
	}
	if original.Reason == model.ReasonReversal {
		return nil, errors.NewWithCode(errors.ErrMoemoepointNotReversible)
	}
	legs, err := transferLegs(tx, original.ID)
	if err != nil {
		return nil, err
	}
	t := Transfer{
		Asset:          original.Asset,
		Reason:         model.ReasonReversal,
		SourceApp:      original.SourceApp,
		IdempotencyKey: "reversal:" + strconv.FormatInt(original.ID, 10),
		Ref:            original.Ref,
		ActorUserID:    r.ActorUserID,
		Note:           r.Note,
		reverses:       &original.ID,
	}
	party := r.PartyUserID == 0
	for _, leg := range legs {
		ref := AccountRef{Kind: leg.Kind, UserID: leg.UserID, Code: leg.Code}
		if ref == UserAccount(r.PartyUserID) {
			party = true
		}
		t.Legs = append(t.Legs, Leg{Account: ref, Amount: -leg.Amount})
	}
	if !party {
		return nil, errors.NewWithCode(errors.ErrMoemoepointTransferNotFound)
	}

	return l.PostTx(ctx, tx, t)
}

func (l *Ledger) Reverse(ctx context.Context, r Reversal) (*Posted, error) {
	var out *Posted
	err := l.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		out, err = l.ReverseTx(ctx, tx, r)
		return err
	})
	return out, err
}
