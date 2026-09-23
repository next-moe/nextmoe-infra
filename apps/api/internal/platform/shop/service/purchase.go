package service

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"time"

	ledgerModel "api/internal/platform/ledger/model"
	ledgerService "api/internal/platform/ledger/service"
	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ledgerSource = "shop"

type OrderView struct {
	model.Order
	Rewards []RewardView `json:"rewards"`
	Price   int64        `json:"price"`
}

type Purchased struct {
	Order   OrderView `json:"order"`
	Balance int64     `json:"balance"`
	Replay  bool      `json:"replay"`
}

func sinkFor(items []model.Item) string {
	for _, it := range items {
		if it.SiteID != nil {
			return fmt.Sprintf("shop:site:%d", *it.SiteID)
		}
	}
	return ledgerSource
}

func orderTransferKey(orderID int64) string { return fmt.Sprintf("order:%d", orderID) }

func (s *Shop) Purchase(ctx context.Context, userID uint, offerID int64, idemKey string) (*Purchased, error) {
	idemKey = strings.TrimSpace(idemKey)
	if idemKey == "" || len(idemKey) > 64 {
		return nil, errors.NewWithCode(errors.ErrMissingParam)
	}
	var out *Purchased
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var prior []model.Order
		if err := tx.Where("user_id = ? AND idempotency_key = ?", userID, idemKey).Limit(1).Find(&prior).Error; err != nil {
			return err
		}
		if len(prior) > 0 {
			if prior[0].OfferID != offerID {
				return errors.NewWithCode(errors.ErrShopIdemConflict)
			}
			view, err := s.orderView(tx, prior[0])
			if err != nil {
				return err
			}
			balance, err := s.ledger.UserBalance(ctx, userID)
			if err != nil {
				return err
			}
			out = &Purchased{Order: *view, Balance: balance, Replay: true}
			return nil
		}

		offer, err := s.loadOffer(tx, offerID, true)
		if err != nil {
			return err
		}
		now := s.now()
		if offer.Status != model.OfferActive || offer.SiteID != nil ||
			(offer.StartsAt != nil && offer.StartsAt.After(now)) || (offer.EndsAt != nil && !offer.EndsAt.After(now)) {
			return errors.NewWithCode(errors.ErrShopOfferUnavailable)
		}
		if offer.Stock != nil && offer.Sold >= *offer.Stock {
			return errors.NewWithCode(errors.ErrShopSoldOut)
		}
		if offer.PerUserLimit > 0 {
			var bought int64
			if err := tx.Model(&model.Order{}).
				Where("user_id = ? AND offer_id = ? AND status = ?", userID, offer.ID, model.OrderCompleted).
				Count(&bought).Error; err != nil {
				return err
			}
			if bought >= int64(offer.PerUserLimit) {
				return errors.NewWithCode(errors.ErrShopLimitReached)
			}
		}

		var costs []model.Cost
		var rewards []model.Reward
		_ = json.Unmarshal(offer.Costs, &costs)
		_ = json.Unmarshal(offer.Rewards, &rewards)
		price := priceOf(costs)
		items := make([]model.Item, 0, len(rewards))
		names := make([]string, 0, len(rewards))
		for _, r := range rewards {
			it, err := s.loadItem(tx, r.ItemID, false)
			if err != nil {
				return err
			}
			if it.Status != model.ItemPublished {
				return errors.NewWithCode(errors.ErrShopOfferUnavailable)
			}
			owned, err := activeEntitlement(tx, userID, it.ID, now, false)
			if err != nil {
				return err
			}
			if owned != nil && owned.ExpiresAt == nil {
				return errors.New(errors.ErrShopAlreadyOwned, "你已经永久拥有「"+it.Name+"」")
			}
			items = append(items, *it)
			names = append(names, it.Name)
		}

		order := model.Order{
			UserID: userID, IdempotencyKey: idemKey, RecipientUserID: userID, OfferID: offer.ID,
			Costs: offer.Costs, Rewards: offer.Rewards, Status: model.OrderCompleted, CreatedAt: now,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}
		posted, err := s.ledger.PostTx(ctx, tx, ledgerService.Transfer{
			Reason:         ledgerModel.ReasonPurchase,
			SourceApp:      ledgerSource,
			IdempotencyKey: orderTransferKey(order.ID),
			Ref:            fmt.Sprintf("shop_offer:%d", offer.ID),
			ActorUserID:    userID,
			Note:           strings.Join(names, "、"),
			Funded:         true,
			Legs: []ledgerService.Leg{
				{Account: ledgerService.UserAccount(userID), Amount: -price},
				{Account: ledgerService.SinkAccount(sinkFor(items)), Amount: price},
			},
		})
		if err != nil {
			return err
		}
		order.TransferID = &posted.TransferID
		if err := tx.Model(&order).Update("transfer_id", posted.TransferID).Error; err != nil {
			return err
		}
		for _, r := range rewards {
			if err := grantTx(tx, grant{
				userID: userID, itemID: r.ItemID, days: r.DurationDays, source: model.SourcePurchase,
				orderID: &order.ID, now: now,
			}); err != nil {
				return err
			}
		}
		if err := tx.Model(offer).Update("sold", gorm.Expr("sold + 1")).Error; err != nil {
			return err
		}
		view, err := s.orderView(tx, order)
		if err != nil {
			return err
		}
		out = &Purchased{Order: *view, Balance: posted.UserBalance(userID)}
		return nil
	})
	return out, err
}

type grant struct {
	userID    uint
	itemID    int64
	days      int
	source    string
	orderID   *int64
	grantedBy uint
	note      string
	now       time.Time
}

func activeEntitlement(tx *gorm.DB, userID uint, itemID int64, now time.Time, lock bool) (*model.Entitlement, error) {
	e, err := findEntitlement(tx, userID, itemID, lock)
	if err != nil || e == nil || !e.Active(now) {
		return nil, err
	}
	return e, nil
}

func findEntitlement(tx *gorm.DB, userID uint, itemID int64, lock bool) (*model.Entitlement, error) {
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var e model.Entitlement
	err := q.Where("user_id = ? AND item_id = ?", userID, itemID).First(&e).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

// A timed grant on an item the user already holds extends what is left
// rather than restarting it, and a permanent grant makes it permanent.
func grantTx(tx *gorm.DB, g grant) error {
	var expires *time.Time
	if g.days > 0 {
		t := g.now.AddDate(0, 0, g.days)
		expires = &t
	}
	e, err := findEntitlement(tx, g.userID, g.itemID, true)
	if err != nil {
		return err
	}
	if e == nil {
		return tx.Create(&model.Entitlement{
			UserID: g.userID, ItemID: g.itemID, Source: g.source, OrderID: g.orderID,
			GrantedBy: g.grantedBy, Note: g.note, AcquiredAt: g.now, ExpiresAt: expires,
		}).Error
	}
	if e.Active(g.now) {
		if e.ExpiresAt == nil {
			return errors.NewWithCode(errors.ErrShopAlreadyOwned)
		}
		if expires != nil {
			extended := e.ExpiresAt.AddDate(0, 0, g.days)
			expires = &extended
		}
	} else {
		e.AcquiredAt = g.now
	}
	e.RevokedAt, e.ExpiresAt = nil, expires
	e.Source, e.OrderID, e.GrantedBy, e.Note = g.source, g.orderID, g.grantedBy, g.note
	return tx.Save(e).Error
}

func (s *Shop) orderView(tx *gorm.DB, o model.Order) (*OrderView, error) {
	var costs []model.Cost
	var rewards []model.Reward
	_ = json.Unmarshal(o.Costs, &costs)
	_ = json.Unmarshal(o.Rewards, &rewards)
	v := &OrderView{Order: o, Price: priceOf(costs)}
	for _, r := range rewards {
		var it model.Item
		if err := tx.First(&it, r.ItemID).Error; err != nil && !stderrors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
		v.Rewards = append(v.Rewards, RewardView{Item: s.itemView(it), DurationDays: r.DurationDays})
	}
	return v, nil
}
