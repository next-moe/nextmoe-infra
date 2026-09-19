package service

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"api/internal/platform/store/model"

	"gorm.io/gorm"
)

type Grant struct {
	FaceValue int `json:"face_value"`
	Count     int `json:"count"`
}

// SplitRow is one site's line in a batch's split: the proposal on a draft,
// the frozen share plus what it actually received on a published batch.
type SplitRow struct {
	ClientID           string  `json:"client_id"`
	Name               string  `json:"name"`
	SettlementEligible bool    `json:"settlement_eligible"`
	Uniques            int64   `json:"uniques"`
	SharePPM           int64   `json:"share_ppm"`
	EntitledPoints     int64   `json:"entitled_points"`
	Grants             []Grant `json:"grants"`
	AllocatedPoints    int64   `json:"allocated_points"`
}

type AdminCoupon struct {
	model.Coupon
	AppName string `json:"app_name"`
}

type BatchDetail struct {
	BatchSummary
	CouponList []AdminCoupon `json:"coupon_list"`
	Split      []SplitRow    `json:"split"`
}

func (s *Service) CouponBatchDetail(ctx context.Context, id int64, apps []AdminApp) (*BatchDetail, error) {
	var batch model.CouponBatch
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&batch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrBatchNotFound
	}
	if err != nil {
		return nil, err
	}
	var coupons []model.Coupon
	if err := s.db.WithContext(ctx).Where("batch_id = ?", id).
		Order("face_value DESC, expires_on ASC NULLS LAST, id ASC").Find(&coupons).Error; err != nil {
		return nil, err
	}
	counts, err := s.valueCounts(ctx, []int64{id})
	if err != nil {
		return nil, err
	}

	byID := make(map[string]AdminApp, len(apps))
	for _, a := range apps {
		byID[a.ClientID] = a
	}
	out := &BatchDetail{
		BatchSummary: summarize(batch, counts[id]),
		CouponList:   make([]AdminCoupon, len(coupons)),
	}
	for i, c := range coupons {
		out.CouponList[i] = AdminCoupon{Coupon: c}
		if c.ClientID != nil {
			out.CouponList[i].AppName = byID[*c.ClientID].Name
		}
	}

	if batch.Status == model.BatchPublished {
		out.Split, err = s.publishedSplit(ctx, id, coupons, byID)
	} else {
		out.Split, err = s.proposedSplit(ctx, batch, coupons, apps)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// proposedSplit lists every site with clicks in the period: eligible sites with
// the allocation Allocate proposes, the others with nothing so the operator
// sees who was left out and why.
func (s *Service) proposedSplit(ctx context.Context, batch model.CouponBatch, coupons []model.Coupon, apps []AdminApp) ([]SplitRow, error) {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ClientID
	}
	uniques, err := s.clientUniques(ctx, ids, batch.PeriodFrom, batch.PeriodTo)
	if err != nil {
		return nil, err
	}
	pool := map[int]int{}
	var total int64
	for _, c := range coupons {
		pool[c.FaceValue]++
		total += int64(c.FaceValue)
	}
	var claims []Claim
	for _, a := range apps {
		if a.SettlementEligible {
			claims = append(claims, Claim{ClientID: a.ClientID, Uniques: uniques[a.ClientID]})
		}
	}
	ent := Entitlements(claims, total)
	grants := Allocate(claims, pool)

	rows := []SplitRow{}
	for _, a := range apps {
		if uniques[a.ClientID] == 0 && len(grants[a.ClientID]) == 0 {
			continue
		}
		row := SplitRow{
			ClientID: a.ClientID, Name: a.Name, SettlementEligible: a.SettlementEligible,
			Uniques: uniques[a.ClientID], Grants: []Grant{},
		}
		if a.SettlementEligible {
			row.SharePPM = ent[a.ClientID].SharePPM
			row.EntitledPoints = ent[a.ClientID].Points
			row.Grants = grantList(grants[a.ClientID])
			row.AllocatedPoints = grantPoints(row.Grants)
		}
		rows = append(rows, row)
	}
	sortSplit(rows)
	return rows, nil
}

func (s *Service) publishedSplit(ctx context.Context, id int64, coupons []model.Coupon, byID map[string]AdminApp) ([]SplitRow, error) {
	var shares []model.CouponShare
	if err := s.db.WithContext(ctx).Where("batch_id = ?", id).Find(&shares).Error; err != nil {
		return nil, err
	}
	received := map[string]map[int]int{}
	for _, c := range coupons {
		if c.ClientID == nil {
			continue
		}
		if received[*c.ClientID] == nil {
			received[*c.ClientID] = map[int]int{}
		}
		received[*c.ClientID][c.FaceValue]++
	}
	rows := make([]SplitRow, 0, len(shares))
	for _, sh := range shares {
		name := sh.AppName
		if a, ok := byID[sh.ClientID]; ok && a.Name != "" {
			name = a.Name
		}
		grants := grantList(received[sh.ClientID])
		rows = append(rows, SplitRow{
			ClientID: sh.ClientID, Name: name, SettlementEligible: true,
			Uniques: sh.Uniques, SharePPM: sh.SharePPM, EntitledPoints: sh.EntitledPoints,
			Grants: grants, AllocatedPoints: grantPoints(grants),
		})
	}
	sortSplit(rows)
	return rows, nil
}

func sortSplit(rows []SplitRow) {
	slices.SortFunc(rows, func(a, b SplitRow) int {
		if a.SettlementEligible != b.SettlementEligible {
			if a.SettlementEligible {
				return -1
			}
			return 1
		}
		return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(a.ClientID, b.ClientID))
	})
}

func grantList(byFace map[int]int) []Grant {
	out := make([]Grant, 0, len(byFace))
	for face, n := range byFace {
		if n > 0 {
			out = append(out, Grant{FaceValue: face, Count: n})
		}
	}
	slices.SortFunc(out, func(a, b Grant) int { return cmp.Compare(b.FaceValue, a.FaceValue) })
	return out
}

func grantPoints(grants []Grant) int64 {
	var p int64
	for _, g := range grants {
		p += int64(g.FaceValue) * int64(g.Count)
	}
	return p
}
