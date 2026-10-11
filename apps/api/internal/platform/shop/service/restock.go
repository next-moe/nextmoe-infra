package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"api/internal/platform/notice"
	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const NoticeTopicRestock = "shop_restock"

func (s *Shop) supplyLeft(tx *gorm.DB, o *model.Offer) (left int, limited bool, err error) {
	var rewards []model.Reward
	_ = json.Unmarshal(o.Rewards, &rewards)
	for _, r := range rewards {
		it, err := s.loadItem(tx, r.ItemID, false)
		if err != nil {
			return 0, false, err
		}
		if classOf(it.Kind) == classCode {
			pool, err := s.sellableCodes(tx, []int64{it.ID})
			return pool[it.ID], true, err
		}
	}
	if o.Stock != nil {
		return max(*o.Stock-o.Sold, 0), true, nil
	}
	return 0, false, nil
}

func (s *Shop) onSale(tx *gorm.DB, o *model.Offer, now time.Time) (bool, error) {
	if o.Status != model.OfferActive || (o.StartsAt != nil && o.StartsAt.After(now)) ||
		(o.EndsAt != nil && !o.EndsAt.After(now)) {
		return false, nil
	}
	left, limited, err := s.supplyLeft(tx, o)
	return !limited || left > 0, err
}

func offersOfItem(tx *gorm.DB, itemID int64) ([]model.Offer, error) {
	var rows []model.Offer
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("rewards @> ?", datatypes.JSON(mustJSON([]map[string]int64{{"item_id": itemID}}))).
		Order("id").Find(&rows).Error
	return rows, err
}

func (s *Shop) noticeIfBack(tx *gorm.DB, o *model.Offer, wasOnSale bool, now time.Time) error {
	if wasOnSale {
		return nil
	}
	on, err := s.onSale(tx, o, now)
	if err != nil || !on {
		return err
	}
	return s.raiseRestock(tx, o, now)
}

func (s *Shop) raiseRestock(tx *gorm.DB, o *model.Offer, now time.Time) error {
	q := tx.Table("shop_restock_alerts AS a").Select("a.user_id, a.site_id, a.inbox").
		Joins("JOIN users u ON u.id = a.user_id AND u.anonymized_at IS NULL").
		Where("a.offer_id = ?", o.ID)
	if o.PerUserLimit > 0 {
		full := tx.Model(&model.Order{}).Select("user_id").
			Where("offer_id = ? AND status = ?", o.ID, model.OrderCompleted)
		if o.LimitPeriod == model.LimitMonth {
			full = full.Where("created_at >= ?", monthStart(now))
		}
		q = q.Where("a.user_id NOT IN (?)", full.Group("user_id").Having("count(*) >= ?", o.PerUserLimit))
	}
	var subs []model.RestockAlert
	if err := q.Order("a.created_at, a.user_id").Scan(&subs).Error; err != nil || len(subs) == 0 {
		return err
	}
	title, err := s.restockTitle(tx, o)
	if err != nil {
		return err
	}
	siteIDs := make([]uint, 0, len(subs))
	for _, a := range subs {
		siteIDs = append(siteIDs, a.SiteID)
	}
	sites, err := sitesByID(tx, siteIDs)
	if err != nil {
		return err
	}
	ref := fmt.Sprintf("offer:%d", o.ID)
	rows := make([]notice.Notice, len(subs))
	for i, a := range subs {
		url := ""
		if site := sites[a.SiteID]; site != nil && site.Domain != "" {
			url = "https://" + site.Domain + "/shop"
		}
		rows[i] = notice.Notice{
			Site: a.Inbox, UserID: int64(a.UserID), Topic: NoticeTopicRestock, Ref: ref, Title: title, URL: url,
		}
	}
	return notice.RaiseTx(tx, rows)
}

func (s *Shop) restockTitle(tx *gorm.DB, o *model.Offer) (string, error) {
	var rewards []model.Reward
	_ = json.Unmarshal(o.Rewards, &rewards)
	names := make([]string, 0, len(rewards))
	for _, r := range rewards {
		it, err := s.loadItem(tx, r.ItemID, false)
		if err != nil {
			return "", err
		}
		names = append(names, it.Name)
	}
	name := strings.Join(names, "、")
	if utf8.RuneCountInString(name) > 100 {
		name = string([]rune(name)[:99]) + "…"
	}
	return "你订阅的「" + name + "」补货了", nil
}

func (s *Shop) SubscribeRestock(ctx context.Context, userID uint, offerID int64, site uint, inbox string) error {
	if inbox == "" {
		return errors.New(errors.ErrShopNotStorefront, "这个站点没有接入站内信,不能订阅补货提醒")
	}
	db := s.db.WithContext(ctx)
	o, err := s.loadOffer(db, offerID, false)
	if err != nil {
		return err
	}
	if o.Status != model.OfferActive || !sellsAt(o.SiteID, site) {
		return errors.NewWithCode(errors.ErrShopOfferUnavailable)
	}
	_, limited, err := s.supplyLeft(db, o)
	if err != nil {
		return err
	}
	if !limited {
		return invalidOffer("这件商品不限量,不需要补货提醒")
	}
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "offer_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"site_id", "inbox"}),
	}).Create(&model.RestockAlert{UserID: userID, OfferID: offerID, SiteID: site, Inbox: inbox}).Error
}

func (s *Shop) UnsubscribeRestock(ctx context.Context, userID uint, offerID int64) error {
	return s.db.WithContext(ctx).Where("user_id = ? AND offer_id = ?", userID, offerID).
		Delete(&model.RestockAlert{}).Error
}

func restockAlerts(db *gorm.DB, userID uint) ([]int64, error) {
	ids := []int64{}
	err := db.Model(&model.RestockAlert{}).Where("user_id = ?", userID).Order("offer_id").Pluck("offer_id", &ids).Error
	return ids, err
}

func codesAddedAt(tx *gorm.DB, itemIDs []int64) (map[int64]time.Time, error) {
	out := make(map[int64]time.Time, len(itemIDs))
	if len(itemIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ItemID int64
		At     time.Time
	}
	if err := tx.Model(&model.Code{}).Select("item_id, max(created_at) AS at").
		Where("item_id IN ?", itemIDs).Group("item_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ItemID] = r.At
	}
	return out, nil
}

func waitingFor(tx *gorm.DB, itemID int64) (int, error) {
	var n int64
	err := tx.Table("shop_restock_alerts AS a").
		Joins("JOIN shop_offers o ON o.id = a.offer_id").
		Where("o.rewards @> ?", datatypes.JSON(mustJSON([]map[string]int64{{"item_id": itemID}}))).
		Distinct("a.user_id").Count(&n).Error
	return int(n), err
}
