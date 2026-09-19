package service

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"api/internal/platform/store/model"

	"gorm.io/gorm"
)

type Grant struct {
	FaceValue int `json:"face_value"`
	Count     int `json:"count"`
}

// SplitRow is one developer account's line in a batch's split: the proposal
// on a draft, the frozen share plus what it actually received on a published
// batch.
type SplitRow struct {
	UserID          uint             `json:"user_id"`
	Name            string           `json:"name"`
	Apps            []model.ShareApp `json:"apps"`
	Uniques         int64            `json:"uniques"`
	SharePPM        int64            `json:"share_ppm"`
	EntitledPoints  int64            `json:"entitled_points"`
	Grants          []Grant          `json:"grants"`
	AllocatedPoints int64            `json:"allocated_points"`
}

// SplitApp is an application with clicks in a batch's period that counts
// toward nobody's share: it is off the roster, or has no owner to receive.
type SplitApp struct {
	ClientID           string `json:"client_id"`
	Name               string `json:"name"`
	OwnerUserID        *uint  `json:"owner_user_id"`
	SettlementEligible bool   `json:"settlement_eligible"`
	Uniques            int64  `json:"uniques"`
}

type AdminCoupon struct {
	model.Coupon
	UserName string `json:"user_name"`
}

type BatchDetail struct {
	BatchSummary
	CouponList []AdminCoupon `json:"coupon_list"`
	Split      []SplitRow    `json:"split"`
	Excluded   []SplitApp    `json:"excluded"`
}

type claimant struct {
	name    string
	apps    []model.ShareApp
	uniques int64
}

// claimantsOf groups the settlement-eligible applications by owner: an
// account's claim is the sum of its eligible applications' uniques. Every
// eligible owner is a claimant, clicks or not, so a grant to one validates.
func claimantsOf(apps []AdminApp, uniques map[string]int64) (map[uint]*claimant, []Claim, []SplitApp) {
	users := map[uint]*claimant{}
	excluded := []SplitApp{}
	for _, a := range apps {
		if !a.SettlementEligible || a.OwnerUserID == nil {
			if uniques[a.ClientID] > 0 {
				excluded = append(excluded, SplitApp{
					ClientID: a.ClientID, Name: a.Name, OwnerUserID: a.OwnerUserID,
					SettlementEligible: a.SettlementEligible, Uniques: uniques[a.ClientID],
				})
			}
			continue
		}
		u := users[*a.OwnerUserID]
		if u == nil {
			u = &claimant{name: ownerName(a), apps: []model.ShareApp{}}
			users[*a.OwnerUserID] = u
		}
		if n := uniques[a.ClientID]; n > 0 {
			u.apps = append(u.apps, model.ShareApp{ClientID: a.ClientID, Name: a.Name, Uniques: n})
			u.uniques += n
		}
	}
	claims := make([]Claim, 0, len(users))
	for id, u := range users {
		slices.SortFunc(u.apps, func(a, b model.ShareApp) int {
			return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(a.ClientID, b.ClientID))
		})
		claims = append(claims, Claim{UserID: id, Uniques: u.uniques})
	}
	slices.SortFunc(claims, func(a, b Claim) int { return cmp.Compare(a.UserID, b.UserID) })
	slices.SortFunc(excluded, func(a, b SplitApp) int {
		return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(a.ClientID, b.ClientID))
	})
	return users, claims, excluded
}

func ownerName(a AdminApp) string {
	if a.OwnerName != "" {
		return a.OwnerName
	}
	return fmt.Sprintf("用户 #%d", *a.OwnerUserID)
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

	out := &BatchDetail{
		BatchSummary: summarize(batch, counts[id]),
		CouponList:   make([]AdminCoupon, len(coupons)),
		Excluded:     []SplitApp{},
	}
	if batch.Status == model.BatchPublished {
		out.Split, err = s.publishedSplit(ctx, id, coupons, apps)
	} else {
		out.Split, out.Excluded, err = s.proposedSplit(ctx, batch, coupons, apps)
	}
	if err != nil {
		return nil, err
	}

	names := map[uint]string{}
	for _, row := range out.Split {
		names[row.UserID] = row.Name
	}
	for i, c := range coupons {
		out.CouponList[i] = AdminCoupon{Coupon: c}
		if c.UserID != nil {
			out.CouponList[i].UserName = names[*c.UserID]
		}
	}
	return out, nil
}

// proposedSplit is what Allocate proposes for every account with clicks in the
// period, next to the applications whose clicks count for nobody so the
// operator sees who was left out and why.
func (s *Service) proposedSplit(ctx context.Context, batch model.CouponBatch, coupons []model.Coupon, apps []AdminApp) ([]SplitRow, []SplitApp, error) {
	uniques, err := s.clientUniques(ctx, clientIDsOf(apps), batch.PeriodFrom, batch.PeriodTo)
	if err != nil {
		return nil, nil, err
	}
	pool := map[int]int{}
	var total int64
	for _, c := range coupons {
		pool[c.FaceValue]++
		total += int64(c.FaceValue)
	}
	users, claims, excluded := claimantsOf(apps, uniques)
	ent := Entitlements(claims, total)
	grants := Allocate(claims, pool)

	rows := []SplitRow{}
	for _, c := range claims {
		if c.Uniques == 0 {
			continue
		}
		g := grantList(grants[c.UserID])
		rows = append(rows, SplitRow{
			UserID: c.UserID, Name: users[c.UserID].name, Apps: users[c.UserID].apps,
			Uniques: c.Uniques, SharePPM: ent[c.UserID].SharePPM, EntitledPoints: ent[c.UserID].Points,
			Grants: g, AllocatedPoints: grantPoints(g),
		})
	}
	sortSplit(rows)
	return rows, excluded, nil
}

func (s *Service) publishedSplit(ctx context.Context, id int64, coupons []model.Coupon, apps []AdminApp) ([]SplitRow, error) {
	var shares []model.CouponShare
	if err := s.db.WithContext(ctx).Where("batch_id = ?", id).Find(&shares).Error; err != nil {
		return nil, err
	}
	live := map[uint]string{}
	for _, a := range apps {
		if a.OwnerUserID != nil && a.OwnerName != "" {
			live[*a.OwnerUserID] = a.OwnerName
		}
	}
	received := map[uint]map[int]int{}
	for _, c := range coupons {
		if c.UserID == nil {
			continue
		}
		if received[*c.UserID] == nil {
			received[*c.UserID] = map[int]int{}
		}
		received[*c.UserID][c.FaceValue]++
	}
	rows := make([]SplitRow, 0, len(shares))
	for _, sh := range shares {
		counted := []model.ShareApp(sh.Apps)
		if counted == nil {
			counted = []model.ShareApp{}
		}
		grants := grantList(received[sh.UserID])
		rows = append(rows, SplitRow{
			UserID: sh.UserID, Name: cmp.Or(live[sh.UserID], sh.UserName), Apps: counted,
			Uniques: sh.Uniques, SharePPM: sh.SharePPM, EntitledPoints: sh.EntitledPoints,
			Grants: grants, AllocatedPoints: grantPoints(grants),
		})
	}
	sortSplit(rows)
	return rows, nil
}

func clientIDsOf(apps []AdminApp) []string {
	ids := make([]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ClientID
	}
	return ids
}

func sortSplit(rows []SplitRow) {
	slices.SortFunc(rows, func(a, b SplitRow) int {
		return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(a.UserID, b.UserID))
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
