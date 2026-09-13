package service

import (
	"context"
	stderrors "errors"

	"api/internal/platform/auth/model"
	"api/internal/platform/auth/repository"
	"api/pkg/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MoemoepointService struct {
	db       *gorm.DB
	userRepo *repository.UserRepository
}

func NewMoemoepointService(db *gorm.DB, userRepo *repository.UserRepository) *MoemoepointService {
	return &MoemoepointService{db: db, userRepo: userRepo}
}

type AdjustParams struct {
	UserID         uint
	Delta          int
	Reason         string
	SourceApp      string
	Ref            string
	ActorUserID    uint
	IdempotencyKey string
	Note           string

	// RequireNonNegative rejects the adjustment when it would leave the balance
	// below zero. It is off by default because the ledger deliberately permits a
	// negative balance so that a reversal or a claw-back is never blocked
	// (docs/integration/oauth/06-moemoepoint.md §3.1). Only a *paid action* opts
	// in: "you may not spend what you do not have" is a different rule from
	// "take back what was wrongly given", and conflating them would make every
	// correction fail on the users who most need it.
	RequireNonNegative bool
}

type AdjustResult struct {
	Balance int  `json:"balance"`
	Applied bool `json:"applied"`
	LogID   int64 `json:"-"`
}

const maxAbsDelta = 1_000_000

func (s *MoemoepointService) Adjust(ctx context.Context, p AdjustParams) (*AdjustResult, error) {
	var result *AdjustResult
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		result, err = s.AdjustTx(ctx, tx, p)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// AdjustTx is Adjust on a transaction the caller owns, for an action that must
// be atomic with the charge it makes. Adjust opening its own transaction was
// once the only entry point, and a paid action written against it could only
// charge in a *separate* transaction from the thing being paid for — which is
// how a rename came to be able to commit without its charge, or charge without
// renaming. Every grant and deduction still goes through this one body
// (docs/integration/oauth/06-moemoepoint.md §3): a second writer would have to
// re-derive the idempotency, the bounds and the reason whitelist, and the day
// one of those rules changes is the day the two copies stop agreeing.
func (s *MoemoepointService) AdjustTx(ctx context.Context, tx *gorm.DB, p AdjustParams) (*AdjustResult, error) {
	if p.Delta == 0 || p.Delta > maxAbsDelta || p.Delta < -maxAbsDelta {
		return nil, errors.NewWithCode(errors.ErrMoemoepointInvalidDelta)
	}
	if !model.IsValidMoemoepointReason(p.Reason) {
		return nil, errors.NewWithCode(errors.ErrMoemoepointInvalidReason)
	}
	if p.IdempotencyKey == "" {
		return nil, errors.NewWithCode(errors.ErrMissingParam)
	}
	tx = tx.WithContext(ctx)

	var existing model.MoemoepointLog
	e := tx.Where("idempotency_key = ?", p.IdempotencyKey).First(&existing).Error
	if e == nil {
		if !sameAdjust(&existing, p) {
			return nil, errors.NewWithCode(errors.ErrMoemoepointIdemConflict)
		}
		var u model.User
		if err := tx.Select("moemoepoint").First(&u, p.UserID).Error; err != nil {
			return nil, mapUserErr(err)
		}
		return &AdjustResult{Balance: u.Moemoepoint, Applied: false, LogID: existing.ID}, nil
	}
	if !stderrors.Is(e, gorm.ErrRecordNotFound) {
		return nil, e
	}

	var u model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&u, p.UserID).Error; err != nil {
		return nil, mapUserErr(err)
	}
	newBalance := u.Moemoepoint + p.Delta
	if p.RequireNonNegative && newBalance < 0 {
		return nil, errors.NewWithCode(errors.ErrMoemoepointInsufficient)
	}

	log := model.MoemoepointLog{
		UserID:         p.UserID,
		Delta:          p.Delta,
		Reason:         p.Reason,
		SourceApp:      p.SourceApp,
		Ref:            p.Ref,
		ActorUserID:    p.ActorUserID,
		IdempotencyKey: p.IdempotencyKey,
		Note:           p.Note,
	}
	res := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true,
	}).Create(&log)
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		var winner model.MoemoepointLog
		if err := tx.Where("idempotency_key = ?", p.IdempotencyKey).First(&winner).Error; err != nil {
			return nil, err
		}
		if !sameAdjust(&winner, p) {
			return nil, errors.NewWithCode(errors.ErrMoemoepointIdemConflict)
		}
		return &AdjustResult{Balance: u.Moemoepoint, Applied: false, LogID: winner.ID}, nil
	}
	if err := tx.Model(&model.User{}).Where("id = ?", p.UserID).
		Update("moemoepoint", newBalance).Error; err != nil {
		return nil, err
	}
	return &AdjustResult{Balance: newBalance, Applied: true, LogID: log.ID}, nil
}

// NextReasonSeqTx numbers a user's charges of one reason so a repeatable paid
// action can build a stable idempotency key (§4 requires the caller to generate
// one). Keying such a charge on its content instead — user plus target name —
// looks stable and is a loophole: renaming A→B→A→B would find the first key
// already present and hand out the fourth rename for free. The caller must
// already hold the user row lock; the count is only stable underneath it.
func (s *MoemoepointService) NextReasonSeqTx(ctx context.Context, tx *gorm.DB, userID uint, reason string) (int64, error) {
	var n int64
	if err := tx.WithContext(ctx).Model(&model.MoemoepointLog{}).
		Where("user_id = ? AND reason = ?", userID, reason).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n + 1, nil
}

func (s *MoemoepointService) GetBalance(ctx context.Context, userID uint) (int, error) {
	var u model.User
	if err := s.db.WithContext(ctx).Select("moemoepoint").First(&u, userID).Error; err != nil {
		return 0, mapUserErr(err)
	}
	return u.Moemoepoint, nil
}

func (s *MoemoepointService) GetLog(ctx context.Context, userID uint, limit int, beforeID int64, reason string) ([]model.MoemoepointLog, bool, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	q := s.db.WithContext(ctx).Where("user_id = ?", userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	if reason != "" {
		q = q.Where("reason = ?", reason)
	}
	var rows []model.MoemoepointLog
	if err := q.Order("id DESC").Limit(limit + 1).Find(&rows).Error; err != nil {
		return nil, false, err
	}
	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}
	return rows, hasMore, nil
}

func (s *MoemoepointService) SourceNames(ctx context.Context, sourceApps []string) map[string]string {
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
	if err := s.db.WithContext(ctx).Table("oauth_clients").
		Select("id, name").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ID] = r.Name
	}
	return out
}

func (s *MoemoepointService) UserIDByUUID(ctx context.Context, uuid string) (uint, error) {
	u, err := s.userRepo.FindByUUID(ctx, uuid)
	if err != nil {
		return 0, errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return u.ID, nil
}

func sameAdjust(existing *model.MoemoepointLog, p AdjustParams) bool {
	return existing.UserID == p.UserID &&
		existing.Delta == p.Delta &&
		existing.Reason == p.Reason &&
		existing.Ref == p.Ref &&
		existing.SourceApp == p.SourceApp &&
		existing.ActorUserID == p.ActorUserID &&
		existing.Note == p.Note
}

func mapUserErr(err error) error {
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return errors.NewWithCode(errors.ErrAuthUserNotFound)
	}
	return err
}
