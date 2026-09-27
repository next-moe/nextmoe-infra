package service

import (
	"context"
	"regexp"
	"strings"
	"time"

	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var codeZone = time.FixedZone("JST", 9*3600)

const minCodeShelfDays = 3

var dayLabel = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func (s *Shop) codeCutoff() string {
	return s.now().In(codeZone).AddDate(0, 0, minCodeShelfDays).Format(time.DateOnly)
}

func (s *Shop) sellable(tx *gorm.DB) *gorm.DB {
	return tx.Model(&model.Code{}).
		Where("order_id IS NULL AND (expires_on IS NULL OR expires_on >= ?)", s.codeCutoff())
}

func (s *Shop) sellableCodes(tx *gorm.DB, itemIDs []int64) (map[int64]int, error) {
	out := make(map[int64]int, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	for _, id := range itemIDs {
		out[id] = 0
	}
	var rows []struct {
		ItemID int64
		N      int
	}
	if err := s.sellable(tx).Select("item_id, count(*) AS n").Where("item_id IN ?", itemIDs).
		Group("item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ItemID] = r.N
	}
	return out, nil
}

func (s *Shop) claimCode(tx *gorm.DB, itemID int64, userID uint, orderID int64, now time.Time) error {
	var picked []model.Code
	if err := s.sellable(tx).Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("item_id = ?", itemID).Order("expires_on, id").Limit(1).Find(&picked).Error; err != nil {
		return err
	}
	if len(picked) == 0 {
		return errors.NewWithCode(errors.ErrShopSoldOut)
	}
	return tx.Model(&picked[0]).Updates(map[string]any{"order_id": orderID, "user_id": userID, "sold_at": now}).Error
}

type CodeView struct {
	ItemID    int64   `json:"item_id"`
	Code      string  `json:"code"`
	ExpiresOn *string `json:"expires_on"`
}

func orderCodes(tx *gorm.DB, orderID int64) ([]CodeView, error) {
	var rows []model.Code
	if err := tx.Where("order_id = ?", orderID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]CodeView, len(rows))
	for i, c := range rows {
		out[i] = CodeView{ItemID: c.ItemID, Code: c.Code, ExpiresOn: c.ExpiresOn}
	}
	return out, nil
}

type CodePool struct {
	Total     int          `json:"total"`
	Sellable  int          `json:"sellable"`
	Sold      int          `json:"sold"`
	Codes     []model.Code `json:"codes"`
	ShelfDays int          `json:"shelf_days"`
}

func (s *Shop) codeItem(tx *gorm.DB, itemID int64) error {
	it, err := s.loadItem(tx, itemID, false)
	if err != nil {
		return err
	}
	if classOf(it.Kind) != classCode {
		return invalidItem("只有兑换码类物品才有码池")
	}
	return nil
}

func (s *Shop) CodePool(ctx context.Context, itemID int64) (*CodePool, error) {
	db := s.db.WithContext(ctx)
	if err := s.codeItem(db, itemID); err != nil {
		return nil, err
	}
	pool := &CodePool{Codes: []model.Code{}, ShelfDays: minCodeShelfDays}
	if err := db.Where("item_id = ?", itemID).Order("order_id IS NOT NULL, expires_on, id").
		Find(&pool.Codes).Error; err != nil {
		return nil, err
	}
	left, err := s.sellableCodes(db, []int64{itemID})
	if err != nil {
		return nil, err
	}
	pool.Total, pool.Sellable = len(pool.Codes), left[itemID]
	for _, c := range pool.Codes {
		if c.OrderID != nil {
			pool.Sold++
		}
	}
	return pool, nil
}

type AddCodesInput struct {
	ItemID    int64    `json:"item_id"`
	Codes     []string `json:"codes"`
	ExpiresOn *string  `json:"expires_on"`
}

type AddedCodes struct {
	Added      int      `json:"added"`
	Duplicates []string `json:"duplicates"`
}

func (s *Shop) AddCodes(ctx context.Context, in AddCodesInput, by uint) (*AddedCodes, error) {
	if in.ExpiresOn != nil {
		if _, err := time.Parse(time.DateOnly, *in.ExpiresOn); err != nil || !dayLabel.MatchString(*in.ExpiresOn) {
			return nil, invalidItem("过期日期格式应为 YYYY-MM-DD")
		}
	}
	seen := map[string]bool{}
	var fresh []string
	for _, c := range in.Codes {
		c = strings.TrimSpace(c)
		if c == "" || seen[c] {
			continue
		}
		if len(c) > 200 {
			return nil, invalidItem("兑换码不能超过 200 个字符")
		}
		seen[c] = true
		fresh = append(fresh, c)
	}
	if len(fresh) == 0 || len(fresh) > 1000 {
		return nil, invalidItem("一次需要添加 1 到 1000 个兑换码")
	}
	out := &AddedCodes{Duplicates: []string{}}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.codeItem(tx, in.ItemID); err != nil {
			return err
		}
		var existing []string
		if err := tx.Model(&model.Code{}).Where("code IN ?", fresh).Pluck("code", &existing).Error; err != nil {
			return err
		}
		taken := map[string]bool{}
		for _, c := range existing {
			taken[c] = true
		}
		rows := make([]model.Code, 0, len(fresh))
		for _, c := range fresh {
			if taken[c] {
				out.Duplicates = append(out.Duplicates, c)
				continue
			}
			rows = append(rows, model.Code{ItemID: in.ItemID, Code: c, ExpiresOn: in.ExpiresOn, AddedBy: by})
		}
		out.Added = len(rows)
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Shop) DeleteCode(ctx context.Context, id int64) error {
	res := s.db.WithContext(ctx).Where("id = ? AND order_id IS NULL", id).Delete(&model.Code{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New(errors.ErrShopInvalidTransition, "兑换码不存在或已经卖出")
	}
	return nil
}
