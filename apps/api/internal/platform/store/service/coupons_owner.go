package service

import (
	"context"
	"time"

	"api/internal/platform/store/model"
)

type OwnerCoupon struct {
	ID          int64      `json:"id"`
	BatchID     int64      `json:"batch_id"`
	BatchName   string     `json:"batch_name"`
	ClientID    string     `json:"client_id"`
	AppName     string     `json:"app_name"`
	FaceValue   int        `json:"face_value"`
	Code        string     `json:"code"`
	ExpiresOn   *string    `json:"expires_on"`
	DeliveredAt *time.Time `json:"delivered_at"`
	PublishedAt *time.Time `json:"published_at"`
}

type OwnerShare struct {
	BatchID         int64      `json:"batch_id"`
	BatchName       string     `json:"batch_name"`
	PeriodFrom      string     `json:"period_from"`
	PeriodTo        string     `json:"period_to"`
	PublishedAt     *time.Time `json:"published_at"`
	ClientID        string     `json:"client_id"`
	AppName         string     `json:"app_name"`
	Uniques         int64      `json:"uniques"`
	SharePPM        int64      `json:"share_ppm"`
	EntitledPoints  int64      `json:"entitled_points"`
	AllocatedPoints int64      `json:"allocated_points"`
}

type OwnerCoupons struct {
	Coupons []OwnerCoupon `json:"coupons"`
	Shares  []OwnerShare  `json:"shares"`
}

// OwnerCoupons lists what published batches gave the owner's applications.
func (s *Service) OwnerCoupons(ctx context.Context, apps []OwnerApp) (*OwnerCoupons, error) {
	out := &OwnerCoupons{Coupons: []OwnerCoupon{}, Shares: []OwnerShare{}}
	if len(apps) == 0 {
		return out, nil
	}
	ids := make([]string, len(apps))
	names := make(map[string]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ClientID
		names[a.ClientID] = a.Name
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.id, c.batch_id, b.name AS batch_name, c.client_id, c.face_value, c.code,
		       c.expires_on, c.delivered_at, b.published_at
		  FROM store_coupons c
		  JOIN store_coupon_batches b ON b.id = c.batch_id
		 WHERE b.status = ? AND c.client_id IN ?
		 ORDER BY b.published_at DESC, c.face_value DESC, c.expires_on ASC NULLS LAST, c.id`,
		model.BatchPublished, ids).Scan(&out.Coupons).Error
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Raw(`
		SELECT s.batch_id, b.name AS batch_name, b.period_from, b.period_to, b.published_at,
		       s.client_id, s.uniques, s.share_ppm, s.entitled_points, s.allocated_points
		  FROM store_coupon_shares s
		  JOIN store_coupon_batches b ON b.id = s.batch_id
		 WHERE b.status = ? AND s.client_id IN ?
		 ORDER BY b.published_at DESC, s.client_id`,
		model.BatchPublished, ids).Scan(&out.Shares).Error
	if err != nil {
		return nil, err
	}
	for i := range out.Coupons {
		out.Coupons[i].AppName = names[out.Coupons[i].ClientID]
	}
	for i := range out.Shares {
		out.Shares[i].AppName = names[out.Shares[i].ClientID]
	}
	return out, nil
}

// SetCouponDelivered records whether the owner has handed a coupon on to one
// of their users. Only a published coupon of one of the owner's apps is found.
func (s *Service) SetCouponDelivered(ctx context.Context, apps []OwnerApp, couponID int64, delivered bool, now time.Time) error {
	if len(apps) == 0 {
		return ErrCouponNotFound
	}
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ClientID
	}
	var at *time.Time
	if delivered {
		at = &now
	}
	res := s.db.WithContext(ctx).Exec(`
		UPDATE store_coupons c
		   SET delivered_at = CASE WHEN ?::timestamptz IS NULL THEN NULL
		                           ELSE COALESCE(c.delivered_at, ?::timestamptz) END
		  FROM store_coupon_batches b
		 WHERE c.id = ? AND b.id = c.batch_id AND b.status = ? AND c.client_id IN ?`,
		at, at, couponID, model.BatchPublished, ids)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCouponNotFound
	}
	return nil
}
