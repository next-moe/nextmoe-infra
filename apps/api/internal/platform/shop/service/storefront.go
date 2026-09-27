package service

import (
	"context"

	"api/internal/platform/shop/model"

	"gorm.io/gorm"
)

func sellsAt(offerSite *uint, storefront uint) bool {
	return storefront == model.EverySite || offerSite == nil || *offerSite == storefront
}

type Storefront struct {
	Site   SiteRef     `json:"site"`
	Offers []OfferView `json:"offers"`
}

func (s *Shop) Storefront(ctx context.Context, site uint) (*Storefront, error) {
	out := &Storefront{Offers: []OfferView{}}
	if err := s.db.WithContext(ctx).Table("sites").Select("id, name, domain").
		Where("id = ?", site).Take(&out.Site).Error; err != nil {
		return nil, err
	}
	offers, err := s.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	for _, o := range offers {
		if sellsAt(o.SiteID, site) {
			out.Offers = append(out.Offers, o)
		}
	}
	return out, nil
}

func (s *Shop) limitUsed(db *gorm.DB, userID uint) (map[int64]int, error) {
	var rows []struct {
		OfferID int64
		N       int
	}
	err := db.Raw(`
		SELECT o.offer_id, count(*) AS n
		FROM shop_orders o
		JOIN shop_offers f ON f.id = o.offer_id AND f.status = ? AND f.per_user_limit > 0
		WHERE o.user_id = ? AND o.status = ? AND (f.limit_period <> ? OR o.created_at >= ?)
		GROUP BY o.offer_id`,
		model.OfferActive, userID, model.OrderCompleted, model.LimitMonth, monthStart(s.now())).Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, r := range rows {
		out[r.OfferID] = r.N
	}
	return out, nil
}
