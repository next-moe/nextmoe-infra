package service

import (
	"context"
	"time"

	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type OwnedItem struct {
	Item       ItemView   `json:"item"`
	Source     string     `json:"source"`
	AcquiredAt time.Time  `json:"acquired_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Active     bool       `json:"active"`
}

type Inventory struct {
	Balance int64           `json:"balance"`
	Items   []OwnedItem     `json:"items"`
	Loadout []model.Loadout `json:"loadout"`
	Orders  []OrderView     `json:"orders"`
}

func (s *Shop) Inventory(ctx context.Context, userID uint) (*Inventory, error) {
	db := s.db.WithContext(ctx)
	balance, err := s.ledger.UserBalance(ctx, userID)
	if err != nil {
		return nil, err
	}
	inv := &Inventory{Balance: balance, Items: []OwnedItem{}, Loadout: []model.Loadout{}, Orders: []OrderView{}}

	var ents []model.Entitlement
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("acquired_at DESC").Find(&ents).Error; err != nil {
		return nil, err
	}
	now := s.now()
	for _, e := range ents {
		it, err := s.loadItem(db, e.ItemID, false)
		if err != nil {
			return nil, err
		}
		inv.Items = append(inv.Items, OwnedItem{
			Item: s.itemView(*it), Source: e.Source, AcquiredAt: e.AcquiredAt,
			ExpiresAt: e.ExpiresAt, Active: e.Active(now),
		})
	}
	if err := db.Where("user_id = ?", userID).Order("site_id, slot").Find(&inv.Loadout).Error; err != nil {
		return nil, err
	}
	var orders []model.Order
	if err := db.Where("user_id = ?", userID).Order("id DESC").Limit(50).Find(&orders).Error; err != nil {
		return nil, err
	}
	for _, o := range orders {
		v, err := s.orderView(db, o)
		if err != nil {
			return nil, err
		}
		inv.Orders = append(inv.Orders, *v)
	}
	return inv, nil
}

func (s *Shop) Equip(ctx context.Context, userID uint, slot string, siteID uint, itemID *int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if siteID != model.EverySite {
			var n int64
			if err := tx.Table("sites").Where("id = ?", siteID).Count(&n).Error; err != nil {
				return err
			}
			if n == 0 {
				return errors.New(errors.ErrShopInvalidItem, "站点不存在")
			}
		}
		if itemID == nil {
			return tx.Where("user_id = ? AND slot = ? AND site_id = ?", userID, slot, siteID).
				Delete(&model.Loadout{}).Error
		}
		it, err := s.loadItem(tx, *itemID, false)
		if err != nil {
			return err
		}
		if itemSlot, ok := model.SlotOf(it.Kind); !ok || itemSlot != slot {
			return errors.New(errors.ErrShopInvalidItem, "这件物品不能戴在这个位置")
		}
		owned, err := activeEntitlement(tx, userID, it.ID, s.now(), false)
		if err != nil {
			return err
		}
		if owned == nil {
			return errors.NewWithCode(errors.ErrShopNotOwned)
		}
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "slot"}, {Name: "site_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"item_id", "updated_at"}),
		}).Create(&model.Loadout{UserID: userID, Slot: slot, SiteID: siteID, ItemID: it.ID, UpdatedAt: s.now()}).Error
	})
}

type wornRow struct {
	UserID uint
	Slot   string
	SiteID uint
	ItemID int64
	Name   string
	Kind   string
	Render []byte
}

// CosmeticsFor resolves what each user wears as seen from siteID. A
// site-specific choice wins over the everywhere default; a loadout whose
// entitlement lapsed or was revoked is skipped, so the default shows instead.
func (s *Shop) CosmeticsFor(ctx context.Context, userIDs []uint, siteID uint) (map[uint]*model.Cosmetics, error) {
	out := make(map[uint]*model.Cosmetics, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	var rows []wornRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT l.user_id, l.slot, l.site_id, i.id AS item_id, i.name, i.kind, i.render
		FROM shop_loadouts l
		JOIN shop_entitlements e ON e.user_id = l.user_id AND e.item_id = l.item_id
		  AND e.revoked_at IS NULL AND (e.expires_at IS NULL OR e.expires_at > ?)
		JOIN shop_items i ON i.id = l.item_id AND i.status IN ?
		WHERE l.user_id IN ? AND l.site_id IN ?`,
		s.now(), []string{model.ItemPublished, model.ItemRetired}, userIDs,
		[]uint{model.EverySite, siteID}).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	type slotKey struct {
		user uint
		slot string
	}
	worn := map[slotKey]*wornRow{}
	for i := range rows {
		r := &rows[i]
		k := slotKey{r.UserID, r.Slot}
		if prev := worn[k]; prev == nil || prev.SiteID == model.EverySite {
			worn[k] = r
		}
	}
	for k, r := range worn {
		it := model.Item{ID: r.ItemID, Name: r.Name, Kind: r.Kind, Render: datatypes.JSON(r.Render)}
		d := s.decoration(&it)
		if d == nil {
			continue
		}
		c := out[k.user]
		if c == nil {
			c = &model.Cosmetics{}
			out[k.user] = c
		}
		if k.slot == model.SlotAvatarFrame {
			c.AvatarFrame = d
		}
	}
	return out, nil
}
