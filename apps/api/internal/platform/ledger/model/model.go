package model

import "time"

const AssetMoemoepoint = "moemoepoint"

const (
	KindUser   = "user"
	KindIssuer = "issuer"
	KindSink   = "sink"
)

const (
	ReasonAdminGrant      = "admin_grant"
	ReasonAdminDeduct     = "admin_deduct"
	ReasonMigration       = "migration"
	ReasonOpeningBalance  = "opening_balance"
	ReasonRegisterGift    = "register_gift"
	ReasonContentApproved = "content_approved"
	ReasonContentRemoved  = "content_removed"
	ReasonDailyCheckin    = "daily_checkin"
	ReasonLiked           = "liked"
	ReasonNameChange      = "name_change"
	ReasonSpend           = "spend"
	ReasonReversal        = "reversal"
)

func IsValidReason(r string) bool {
	switch r {
	case ReasonAdminGrant, ReasonAdminDeduct, ReasonMigration, ReasonOpeningBalance,
		ReasonRegisterGift, ReasonContentApproved, ReasonContentRemoved,
		ReasonDailyCheckin, ReasonLiked, ReasonNameChange, ReasonSpend, ReasonReversal:
		return true
	}
	return false
}

type Account struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	Asset     string    `gorm:"size:32;not null;uniqueIndex:uidx_ledger_accounts_owner,priority:1" json:"asset"`
	Kind      string    `gorm:"size:16;not null;uniqueIndex:uidx_ledger_accounts_owner,priority:2" json:"kind"`
	UserID    uint      `gorm:"not null;default:0;uniqueIndex:uidx_ledger_accounts_owner,priority:3" json:"user_id"`
	Code      string    `gorm:"size:64;not null;default:'';uniqueIndex:uidx_ledger_accounts_owner,priority:4" json:"code"`
	Balance   int64     `gorm:"not null;default:0" json:"balance"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Account) TableName() string { return "ledger_accounts" }

type Transfer struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	Asset          string    `gorm:"size:32;not null" json:"asset"`
	Reason         string    `gorm:"size:40;not null;index" json:"reason"`
	SourceApp      string    `gorm:"size:50;not null;uniqueIndex:uidx_ledger_transfers_idem,priority:1" json:"source_app"`
	IdempotencyKey string    `gorm:"size:128;not null;uniqueIndex:uidx_ledger_transfers_idem,priority:2" json:"-"`
	Ref            string    `gorm:"size:80;not null;default:''" json:"ref"`
	ActorUserID    uint      `gorm:"not null;default:0" json:"actor_user_id"`
	Note           string    `gorm:"size:255;not null;default:''" json:"note"`
	ReversesID     *int64    `gorm:"uniqueIndex" json:"reverses_id,omitempty"`
	LegacyLogID    *int64    `gorm:"uniqueIndex" json:"-"`
	CreatedAt      time.Time `json:"created_at"`
}

func (Transfer) TableName() string { return "ledger_transfers" }

type Entry struct {
	ID           int64     `gorm:"primaryKey;index:idx_ledger_entries_account,priority:2" json:"id"`
	TransferID   int64     `gorm:"not null;index" json:"transfer_id"`
	AccountID    int64     `gorm:"not null;index:idx_ledger_entries_account,priority:1" json:"account_id"`
	Amount       int64     `gorm:"not null" json:"amount"`
	BalanceAfter int64     `gorm:"not null" json:"balance_after"`
	CreatedAt    time.Time `json:"created_at"`
}

func (Entry) TableName() string { return "ledger_entries" }

// LegacyLog is the single-entry moemoepoint log the ledger replaced on
// 2026-09-23. Nothing writes it any more; ImportLegacy reads it, which is why
// cmd/migrate still creates it on a fresh database.
type LegacyLog struct {
	ID             int64  `gorm:"primaryKey"`
	UserID         uint   `gorm:"not null;index"`
	Delta          int    `gorm:"not null"`
	Reason         string `gorm:"size:40;not null;index"`
	SourceApp      string `gorm:"size:32;not null"`
	Ref            string `gorm:"size:80;default:''"`
	ActorUserID    uint   `gorm:"not null;default:0"`
	IdempotencyKey string `gorm:"size:128;not null;uniqueIndex"`
	Note           string `gorm:"size:255;default:''"`
	CreatedAt      time.Time
}

func (LegacyLog) TableName() string { return "moemoepoint_log" }

func AllModels() []any {
	return []any{&LegacyLog{}, &Account{}, &Transfer{}, &Entry{}}
}
