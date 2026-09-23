package service

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"time"
	"unicode/utf8"

	ledgerModel "api/internal/platform/ledger/model"
	"api/internal/platform/settings/keys"
	"api/internal/platform/shop/model"
	"api/pkg/errors"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ItemInput struct {
	Kind        string          `json:"kind"`
	SiteID      *uint           `json:"site_id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Render      json.RawMessage `json:"render"`
}

type ItemView struct {
	model.Item
	Preview *model.Decoration `json:"preview,omitempty"`
}

func invalidItem(msg string) error { return errors.New(errors.ErrShopInvalidItem, msg) }

func invalidOffer(msg string) error { return errors.New(errors.ErrShopInvalidOffer, msg) }

func (s *Shop) decoration(it *model.Item) *model.Decoration {
	if _, ok := kinds[it.Kind]; !ok {
		return nil
	}
	var r model.Render
	if json.Unmarshal(it.Render, &r) != nil || r.Static == "" {
		return nil
	}
	return &model.Decoration{
		ItemID: it.ID, Name: it.Name,
		StaticURL: s.assetURL(r.Static), AnimatedURL: s.assetURL(r.Animated),
	}
}

func (s *Shop) itemView(it model.Item) ItemView {
	return ItemView{Item: it, Preview: s.decoration(&it)}
}

func (s *Shop) validateRender(tx *gorm.DB, kind string, raw json.RawMessage) (datatypes.JSON, error) {
	spec, err := specOf(kind)
	if err != nil {
		return nil, err
	}
	var r model.Render
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil || r.Static == "" {
		return nil, invalidItem(spec.label + "需要一张静态图")
	}
	static, err := assetByKey(tx, r.Static)
	if err != nil {
		return nil, err
	}
	if static == nil || static.Animated || !spec.static.accepts(static.ContentType) {
		return nil, invalidItem("静态图必须是按" + spec.label + "规格上传的素材")
	}
	if err := spec.static.fits(static.Width, static.Height); err != nil {
		return nil, err
	}
	if r.Animated != "" {
		anim, err := assetByKey(tx, r.Animated)
		if err != nil {
			return nil, err
		}
		if anim == nil || !anim.Animated {
			return nil, invalidItem("动图必须是已上传的动态 WebP 素材")
		}
		if anim.Width != static.Width || anim.Height != static.Height {
			return nil, invalidItem("动图和静态图的尺寸必须一致")
		}
		if anim.Bytes > spec.animated.maxBytes {
			return nil, invalidItem(spec.label + "的动图超过了大小上限")
		}
	}
	out, _ := json.Marshal(r)
	return datatypes.JSON(out), nil
}

func assetByKey(tx *gorm.DB, key string) (*model.Asset, error) {
	var rows []model.Asset
	if err := tx.Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func checkItemText(name, description string) error {
	if n := utf8.RuneCountInString(name); n == 0 || n > 32 {
		return invalidItem("名称需要 1 到 32 个字")
	}
	if utf8.RuneCountInString(description) > 120 {
		return invalidItem("简介不能超过 120 个字")
	}
	return nil
}

func (s *Shop) CreateItem(ctx context.Context, in ItemInput, by uint) (*ItemView, error) {
	if err := checkItemText(in.Name, in.Description); err != nil {
		return nil, err
	}
	db := s.db.WithContext(ctx)
	render, err := s.validateRender(db, in.Kind, in.Render)
	if err != nil {
		return nil, err
	}
	it := model.Item{
		Kind: in.Kind, SiteID: in.SiteID, Status: model.ItemDraft,
		Name: in.Name, Description: in.Description, Render: render, CreatedBy: by,
	}
	if err := db.Create(&it).Error; err != nil {
		return nil, err
	}
	v := s.itemView(it)
	return &v, nil
}

func (s *Shop) loadItem(tx *gorm.DB, id int64, lock bool) (*model.Item, error) {
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var it model.Item
	err := q.First(&it, id).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.NewWithCode(errors.ErrShopItemNotFound)
	}
	return &it, err
}

func (s *Shop) UpdateItem(ctx context.Context, id int64, in ItemInput) (*ItemView, error) {
	if err := checkItemText(in.Name, in.Description); err != nil {
		return nil, err
	}
	var out ItemView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		it, err := s.loadItem(tx, id, true)
		if err != nil {
			return err
		}
		it.Name, it.Description = in.Name, in.Description
		if it.Status == model.ItemDraft || it.Status == model.ItemReview {
			render, err := s.validateRender(tx, it.Kind, in.Render)
			if err != nil {
				return err
			}
			it.Render, it.SiteID = render, in.SiteID
		} else if len(in.Render) > 0 {
			render, err := s.validateRender(tx, it.Kind, in.Render)
			if err != nil {
				return err
			}
			if string(render) != string(it.Render) {
				return errors.New(errors.ErrShopInvalidTransition, "已发布的物品不能更换素材")
			}
		}
		if err := tx.Save(it).Error; err != nil {
			return err
		}
		out = s.itemView(*it)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

type ItemAction string

const (
	ItemSubmit  ItemAction = "submit"
	ItemReject  ItemAction = "reject"
	ItemPublish ItemAction = "publish"
	ItemRetire  ItemAction = "retire"
	ItemRelist  ItemAction = "relist"
)

func (a ItemAction) NeedsPublisher() bool { return a != ItemSubmit }

func (s *Shop) TransitionItem(ctx context.Context, id int64, action ItemAction) (*ItemView, error) {
	var out ItemView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		it, err := s.loadItem(tx, id, true)
		if err != nil {
			return err
		}
		from := map[ItemAction][]string{
			ItemSubmit:  {model.ItemDraft},
			ItemReject:  {model.ItemReview},
			ItemPublish: {model.ItemDraft, model.ItemReview},
			ItemRetire:  {model.ItemPublished},
			ItemRelist:  {model.ItemRetired},
		}[action]
		to := map[ItemAction]string{
			ItemSubmit: model.ItemReview, ItemReject: model.ItemDraft, ItemPublish: model.ItemPublished,
			ItemRetire: model.ItemRetired, ItemRelist: model.ItemPublished,
		}[action]
		if !contains(from, it.Status) {
			return errors.NewWithCode(errors.ErrShopInvalidTransition)
		}
		it.Status = to
		if to == model.ItemPublished && it.PublishedAt == nil {
			now := s.now()
			it.PublishedAt = &now
		}
		if err := tx.Save(it).Error; err != nil {
			return err
		}
		out = s.itemView(*it)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *Shop) DeleteItem(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		it, err := s.loadItem(tx, id, true)
		if err != nil {
			return err
		}
		if it.Status != model.ItemDraft || it.PublishedAt != nil {
			return errors.New(errors.ErrShopInvalidTransition, "发布过的物品只能下架,不能删除")
		}
		var offers int64
		if err := tx.Model(&model.Offer{}).
			Where("rewards @> ?", datatypes.JSON(mustJSON([]map[string]int64{{"item_id": id}}))).
			Count(&offers).Error; err != nil {
			return err
		}
		if offers > 0 {
			return errors.New(errors.ErrShopInvalidTransition, "还有商品引用这件物品")
		}
		return tx.Delete(&model.Item{}, id).Error
	})
}

func (s *Shop) ListItems(ctx context.Context, status string) ([]ItemView, error) {
	q := s.db.WithContext(ctx).Order("id DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []model.Item
	if err := q.Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ItemView, len(rows))
	for i, it := range rows {
		out[i] = s.itemView(it)
	}
	return out, nil
}

type OfferInput struct {
	SiteID       *uint          `json:"site_id"`
	Price        int64          `json:"price"`
	Rewards      []model.Reward `json:"rewards"`
	StartsAt     *time.Time     `json:"starts_at"`
	EndsAt       *time.Time     `json:"ends_at"`
	PerUserLimit int            `json:"per_user_limit"`
	Stock        *int           `json:"stock"`
	SortOrder    int            `json:"sort_order"`
}

type RewardView struct {
	Item         ItemView `json:"item"`
	DurationDays int      `json:"duration_days,omitempty"`
}

type SiteRef struct {
	ID     uint   `json:"id"`
	Name   string `json:"name"`
	Domain string `json:"domain"`
}

type OfferView struct {
	ID           int64        `json:"id"`
	SiteID       *uint        `json:"site_id"`
	Site         *SiteRef     `json:"site,omitempty"`
	Status       string       `json:"status"`
	Price        int64        `json:"price"`
	Costs        []model.Cost `json:"costs"`
	Rewards      []RewardView `json:"rewards"`
	StartsAt     *time.Time   `json:"starts_at"`
	EndsAt       *time.Time   `json:"ends_at"`
	PerUserLimit int          `json:"per_user_limit"`
	Stock        *int         `json:"stock"`
	Sold         int          `json:"sold"`
	SortOrder    int          `json:"sort_order"`
	CreatedAt    time.Time    `json:"created_at"`
}

func (s *Shop) validateOffer(tx *gorm.DB, in OfferInput) ([]model.Item, error) {
	if in.Price < keys.ShopMinPrice.Get() {
		return nil, errors.New(errors.ErrShopPriceBelowMinimum, "价格不能低于商店最低价")
	}
	if in.Price > 1_000_000 {
		return nil, invalidOffer("价格过高")
	}
	if len(in.Rewards) == 0 || len(in.Rewards) > 10 {
		return nil, invalidOffer("一件商品需要包含 1 到 10 件物品")
	}
	if in.StartsAt != nil && in.EndsAt != nil && !in.EndsAt.After(*in.StartsAt) {
		return nil, invalidOffer("结束时间必须晚于开始时间")
	}
	if in.PerUserLimit < 0 || (in.Stock != nil && *in.Stock < 0) {
		return nil, invalidOffer("限购和库存不能为负数")
	}
	items := make([]model.Item, 0, len(in.Rewards))
	seen := map[int64]bool{}
	for _, r := range in.Rewards {
		if seen[r.ItemID] {
			return nil, invalidOffer("同一件物品不能重复出现")
		}
		seen[r.ItemID] = true
		if r.DurationDays < 0 || r.DurationDays > 3650 {
			return nil, invalidOffer("有效期需要在 0 到 3650 天之间,0 表示永久")
		}
		it, err := s.loadItem(tx, r.ItemID, false)
		if err != nil {
			return nil, err
		}
		if it.Status == model.ItemRetired {
			return nil, invalidOffer("已下架的物品不能再上架出售")
		}
		if it.SiteID != nil && (in.SiteID == nil || *in.SiteID != *it.SiteID) {
			return nil, invalidOffer("站点独有的物品只能在它自己的站点出售")
		}
		items = append(items, *it)
	}
	return items, nil
}

func offerCosts(price int64) datatypes.JSON {
	return datatypes.JSON(mustJSON([]model.Cost{{Asset: ledgerModel.AssetMoemoepoint, Amount: price}}))
}

func (s *Shop) CreateOffer(ctx context.Context, in OfferInput, by uint) (*OfferView, error) {
	var out *OfferView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := s.validateOffer(tx, in); err != nil {
			return err
		}
		o := model.Offer{
			SiteID: in.SiteID, Status: model.OfferDraft, Costs: offerCosts(in.Price),
			Rewards: datatypes.JSON(mustJSON(in.Rewards)), StartsAt: in.StartsAt, EndsAt: in.EndsAt,
			PerUserLimit: in.PerUserLimit, Stock: in.Stock, SortOrder: in.SortOrder, CreatedBy: by,
		}
		if err := tx.Create(&o).Error; err != nil {
			return err
		}
		var err error
		out, err = s.offerView(tx, o)
		return err
	})
	return out, err
}

func (s *Shop) loadOffer(tx *gorm.DB, id int64, lock bool) (*model.Offer, error) {
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var o model.Offer
	err := q.First(&o, id).Error
	if stderrors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.NewWithCode(errors.ErrShopOfferUnavailable)
	}
	return &o, err
}

func (s *Shop) UpdateOffer(ctx context.Context, id int64, in OfferInput, canPublish bool) (*OfferView, error) {
	var out *OfferView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := s.loadOffer(tx, id, true)
		if err != nil {
			return err
		}
		if o.Status == model.OfferRetired {
			return errors.New(errors.ErrShopInvalidTransition, "已下架的商品不能再修改")
		}
		if o.Status == model.OfferActive && !canPublish {
			return errors.NewWithCode(errors.ErrForbidden)
		}
		items, err := s.validateOffer(tx, in)
		if err != nil {
			return err
		}
		if o.Status == model.OfferActive {
			for _, it := range items {
				if it.Status != model.ItemPublished {
					return invalidOffer("在售商品里的物品都必须已发布")
				}
			}
		}
		o.SiteID, o.Costs, o.Rewards = in.SiteID, offerCosts(in.Price), datatypes.JSON(mustJSON(in.Rewards))
		o.StartsAt, o.EndsAt, o.PerUserLimit, o.Stock, o.SortOrder = in.StartsAt, in.EndsAt, in.PerUserLimit, in.Stock, in.SortOrder
		if err := tx.Save(o).Error; err != nil {
			return err
		}
		out, err = s.offerView(tx, *o)
		return err
	})
	return out, err
}

type OfferAction string

const (
	OfferActivate OfferAction = "activate"
	OfferRetire   OfferAction = "retire"
)

func (s *Shop) TransitionOffer(ctx context.Context, id int64, action OfferAction) (*OfferView, error) {
	var out *OfferView
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		o, err := s.loadOffer(tx, id, true)
		if err != nil {
			return err
		}
		switch {
		case action == OfferActivate && (o.Status == model.OfferDraft || o.Status == model.OfferRetired):
			var rewards []model.Reward
			_ = json.Unmarshal(o.Rewards, &rewards)
			for _, r := range rewards {
				it, err := s.loadItem(tx, r.ItemID, false)
				if err != nil {
					return err
				}
				if it.Status != model.ItemPublished {
					return invalidOffer("商品里的物品都发布之后才能上架")
				}
			}
			o.Status = model.OfferActive
		case action == OfferRetire && o.Status == model.OfferActive:
			o.Status = model.OfferRetired
		default:
			return errors.NewWithCode(errors.ErrShopInvalidTransition)
		}
		if err := tx.Save(o).Error; err != nil {
			return err
		}
		out, err = s.offerView(tx, *o)
		return err
	})
	return out, err
}

func (s *Shop) ListOffers(ctx context.Context, status string) ([]OfferView, error) {
	db := s.db.WithContext(ctx)
	q := db.Order("sort_order DESC, id DESC")
	if status != "" {
		q = q.Where("status = ?", status)
	}
	var rows []model.Offer
	if err := q.Limit(500).Find(&rows).Error; err != nil {
		return nil, err
	}
	return s.offerViews(db, rows)
}

func (s *Shop) Catalog(ctx context.Context) ([]OfferView, error) {
	db := s.db.WithContext(ctx)
	now := s.now()
	var rows []model.Offer
	if err := db.Where("status = ?", model.OfferActive).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at > ?)", now, now).
		Order("sort_order DESC, id DESC").Find(&rows).Error; err != nil {
		return nil, err
	}
	views, err := s.offerViews(db, rows)
	if err != nil {
		return nil, err
	}
	out := views[:0]
	for _, v := range views {
		sellable := true
		for _, r := range v.Rewards {
			sellable = sellable && r.Item.Status == model.ItemPublished
		}
		if sellable {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Shop) offerView(tx *gorm.DB, o model.Offer) (*OfferView, error) {
	views, err := s.offerViews(tx, []model.Offer{o})
	if err != nil {
		return nil, err
	}
	return &views[0], nil
}

func (s *Shop) offerViews(tx *gorm.DB, offers []model.Offer) ([]OfferView, error) {
	var ids []int64
	rewardsOf := make([][]model.Reward, len(offers))
	for i, o := range offers {
		_ = json.Unmarshal(o.Rewards, &rewardsOf[i])
		for _, r := range rewardsOf[i] {
			ids = append(ids, r.ItemID)
		}
	}
	items := map[int64]model.Item{}
	if len(ids) > 0 {
		var rows []model.Item
		if err := tx.Where("id IN ?", ids).Find(&rows).Error; err != nil {
			return nil, err
		}
		for _, it := range rows {
			items[it.ID] = it
		}
	}
	var siteIDs []uint
	for _, o := range offers {
		if o.SiteID != nil {
			siteIDs = append(siteIDs, *o.SiteID)
		}
	}
	sites, err := sitesByID(tx, siteIDs)
	if err != nil {
		return nil, err
	}
	out := make([]OfferView, len(offers))
	for i, o := range offers {
		var costs []model.Cost
		_ = json.Unmarshal(o.Costs, &costs)
		v := OfferView{
			ID: o.ID, SiteID: o.SiteID, Status: o.Status, Price: priceOf(costs), Costs: costs,
			StartsAt: o.StartsAt, EndsAt: o.EndsAt, PerUserLimit: o.PerUserLimit, Stock: o.Stock,
			Sold: o.Sold, SortOrder: o.SortOrder, CreatedAt: o.CreatedAt,
		}
		if o.SiteID != nil {
			v.Site = sites[*o.SiteID]
		}
		for _, r := range rewardsOf[i] {
			v.Rewards = append(v.Rewards, RewardView{Item: s.itemView(items[r.ItemID]), DurationDays: r.DurationDays})
		}
		out[i] = v
	}
	return out, nil
}

func priceOf(costs []model.Cost) int64 {
	for _, c := range costs {
		if c.Asset == ledgerModel.AssetMoemoepoint {
			return c.Amount
		}
	}
	return 0
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func sitesByID(tx *gorm.DB, ids []uint) (map[uint]*SiteRef, error) {
	out := map[uint]*SiteRef{}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []SiteRef
	if err := tx.Table("sites").Select("id, name, domain").Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = &rows[i]
	}
	return out, nil
}

func (s *Shop) Sites(ctx context.Context) ([]SiteRef, error) {
	var rows []SiteRef
	err := s.db.WithContext(ctx).Table("sites").Select("id, name, domain").Order("id").Scan(&rows).Error
	return rows, err
}
