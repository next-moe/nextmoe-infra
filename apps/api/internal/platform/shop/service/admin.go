package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"time"

	ledgerService "api/internal/platform/ledger/service"
	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type EntitlementView struct {
	model.Entitlement
	Item   ItemView `json:"item"`
	Active bool     `json:"active"`
}

type UserShop struct {
	Balance      int64             `json:"balance"`
	Entitlements []EntitlementView `json:"entitlements"`
	Loadout      []model.Loadout   `json:"loadout"`
	Orders       []OrderView       `json:"orders"`
}

func (s *Shop) UserShop(ctx context.Context, userID uint) (*UserShop, error) {
	db := s.db.WithContext(ctx)
	inv, err := s.Inventory(ctx, userID)
	if err != nil {
		return nil, err
	}
	var ents []model.Entitlement
	if err := db.Where("user_id = ?", userID).Order("id DESC").Find(&ents).Error; err != nil {
		return nil, err
	}
	out := &UserShop{Balance: inv.Balance, Loadout: inv.Loadout, Orders: inv.Orders, Entitlements: []EntitlementView{}}
	now := s.now()
	for _, e := range ents {
		it, err := s.loadItem(db, e.ItemID, false)
		if err != nil {
			return nil, err
		}
		out.Entitlements = append(out.Entitlements, EntitlementView{Entitlement: e, Item: s.itemView(*it), Active: e.Active(now)})
	}
	return out, nil
}

func (s *Shop) Grant(ctx context.Context, userID uint, itemID int64, days int, note string, by uint) error {
	if days < 0 || days > 3650 {
		return errors.New(errors.ErrShopInvalidItem, "有效期需要在 0 到 3650 天之间,0 表示永久")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var users int64
		if err := tx.Table("users").Where("id = ? AND deleted_at IS NULL", userID).Count(&users).Error; err != nil {
			return err
		}
		if users == 0 {
			return errors.NewWithCode(errors.ErrAuthUserNotFound)
		}
		it, err := s.loadItem(tx, itemID, false)
		if err != nil {
			return err
		}
		if it.Status != model.ItemPublished && it.Status != model.ItemRetired {
			return errors.New(errors.ErrShopInvalidTransition, "只能发放已发布的物品")
		}
		return grantTx(tx, grant{
			userID: userID, itemID: itemID, days: days, source: model.SourceGrant,
			grantedBy: by, note: note, now: s.now(),
		})
	})
}

func revokeTx(tx *gorm.DB, e *model.Entitlement, now time.Time) error {
	if err := tx.Model(e).Update("revoked_at", now).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? AND item_id = ?", e.UserID, e.ItemID).Delete(&model.Loadout{}).Error
}

func (s *Shop) Revoke(ctx context.Context, entitlementID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e model.Entitlement
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&e, entitlementID).Error
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.NewWithCode(errors.ErrShopNotOwned)
		}
		if err != nil {
			return err
		}
		if e.RevokedAt != nil {
			return nil
		}
		return revokeTx(tx, &e, s.now())
	})
}

// Refund gives the points back and takes back only what this order handed
// out: an entitlement a later purchase or grant renewed stays with the user.
func (s *Shop) Refund(ctx context.Context, orderID int64, by uint, note string) (*OrderView, error) {
	var out *OrderView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var o model.Order
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&o, orderID).Error
		if stderrors.Is(err, gorm.ErrRecordNotFound) {
			return errors.NewWithCode(errors.ErrShopOrderNotFound)
		}
		if err != nil {
			return err
		}
		if o.Status != model.OrderCompleted {
			return errors.New(errors.ErrShopInvalidTransition, "这笔订单已经退过款了")
		}
		posted, err := s.ledger.ReverseTx(ctx, tx, ledgerService.Reversal{
			SourceApp: ledgerSource, IdempotencyKey: orderTransferKey(o.ID),
			PartyUserID: o.UserID, ActorUserID: by, Note: note,
		})
		if err != nil {
			return err
		}
		now := s.now()
		o.Status, o.RefundTransferID, o.RefundedAt = model.OrderRefunded, &posted.TransferID, &now
		if err := tx.Save(&o).Error; err != nil {
			return err
		}
		var rewards []model.Reward
		_ = json.Unmarshal(o.Rewards, &rewards)
		for _, r := range rewards {
			e, err := findEntitlement(tx, o.RecipientUserID, r.ItemID, true)
			if err != nil {
				return err
			}
			if e != nil && e.RevokedAt == nil && e.OrderID != nil && *e.OrderID == o.ID {
				if err := revokeTx(tx, e, now); err != nil {
					return err
				}
			}
		}
		if err := tx.Model(&model.Offer{}).Where("id = ? AND sold > 0", o.OfferID).
			Update("sold", gorm.Expr("sold - 1")).Error; err != nil {
			return err
		}
		out, err = s.orderView(tx, o)
		return err
	})
	return out, err
}
