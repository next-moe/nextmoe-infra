package service

import (
	"context"
	"time"

	"api/internal/platform/store/model"

	"gorm.io/datatypes"
)

type OwnerCoupon struct {
	ID          int64      `json:"id"`
	BatchID     int64      `json:"batch_id"`
	BatchName   string     `json:"batch_name"`
	FaceValue   int        `json:"face_value"`
	Code        string     `json:"code"`
	ExpiresOn   *string    `json:"expires_on"`
	DeliveredAt *time.Time `json:"delivered_at"`
	PublishedAt *time.Time `json:"published_at"`
}

type OwnerShare struct {
	BatchID         int64                               `json:"batch_id"`
	BatchName       string                              `json:"batch_name"`
	PeriodFrom      string                              `json:"period_from"`
	PeriodTo        string                              `json:"period_to"`
	PublishedAt     *time.Time                          `json:"published_at"`
	Apps            datatypes.JSONSlice[model.ShareApp] `json:"apps"`
	Uniques         int64                               `json:"uniques"`
	SharePPM        int64                               `json:"share_ppm"`
	EntitledPoints  int64                               `json:"entitled_points"`
	AllocatedPoints int64                               `json:"allocated_points"`
}

type OwnerCoupons struct {
	Coupons []OwnerCoupon `json:"coupons"`
	Shares  []OwnerShare  `json:"shares"`
}

// OwnerCoupons lists what published batches gave one developer account.
func (s *Service) OwnerCoupons(ctx context.Context, userID uint) (*OwnerCoupons, error) {
	out := &OwnerCoupons{Coupons: []OwnerCoupon{}, Shares: []OwnerShare{}}
	err := s.db.WithContext(ctx).Raw(`
		SELECT c.id, c.batch_id, b.name AS batch_name, c.face_value, c.code,
		       c.expires_on, c.delivered_at, b.published_at
		  FROM store_coupons c
		  JOIN store_coupon_batches b ON b.id = c.batch_id
		 WHERE b.status = ? AND c.user_id = ?
		 ORDER BY b.published_at DESC, c.face_value DESC, c.expires_on ASC NULLS LAST, c.id`,
		model.BatchPublished, userID).Scan(&out.Coupons).Error
	if err != nil {
		return nil, err
	}
	err = s.db.WithContext(ctx).Raw(`
		SELECT s.batch_id, b.name AS batch_name, b.period_from, b.period_to, b.published_at,
		       s.apps, s.uniques, s.share_ppm, s.entitled_points, s.allocated_points
		  FROM store_coupon_shares s
		  JOIN store_coupon_batches b ON b.id = s.batch_id
		 WHERE b.status = ? AND s.user_id = ?
		 ORDER BY b.published_at DESC, s.batch_id DESC`,
		model.BatchPublished, userID).Scan(&out.Shares).Error
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SetCouponDelivered records whether the owner has handed a coupon on to one
// of their users. Only a published coupon given to this account is found.
func (s *Service) SetCouponDelivered(ctx context.Context, userID uint, couponID int64, delivered bool, now time.Time) error {
	var at *time.Time
	if delivered {
		at = &now
	}
	res := s.db.WithContext(ctx).Exec(`
		UPDATE store_coupons c
		   SET delivered_at = CASE WHEN ?::timestamptz IS NULL THEN NULL
		                           ELSE COALESCE(c.delivered_at, ?::timestamptz) END
		  FROM store_coupon_batches b
		 WHERE c.id = ? AND b.id = c.batch_id AND b.status = ? AND c.user_id = ?`,
		at, at, couponID, model.BatchPublished, userID)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrCouponNotFound
	}
	return nil
}
