package service

import (
	"cmp"
	"context"
	"slices"
	"time"

	"api/internal/platform/store/model"
)

const (
	MaxAdminRangeDays = 366
	adminTopLinks     = 100
)

// AdminApp is one site that has minted store links, as the operator console
// needs it. devapi owns the row; the caller resolves these fields for it.
type AdminApp struct {
	ClientID           string
	Name               string
	OwnerUserID        *uint
	SettlementEligible bool
}

type AdminUsageApp struct {
	ClientID           string `json:"client_id"`
	Name               string `json:"name"`
	OwnerUserID        *uint  `json:"owner_user_id"`
	SettlementEligible bool   `json:"settlement_eligible"`
	Links              int64  `json:"links"`
	Total              int64  `json:"total"`
	Uniques            int64  `json:"uniques"`
	Bots               int64  `json:"bots"`
	// SharePPM is this site's part of every site's uniques in the range, in
	// parts per million; settlement shares count eligible sites only.
	SharePPM int64 `json:"share_ppm"`
}

type AdminUsage struct {
	From      string           `json:"from"`
	To        string           `json:"to"`
	Total     int64            `json:"total"`
	Uniques   int64            `json:"uniques"`
	Bots      int64            `json:"bots"`
	LinkCount int64            `json:"link_count"`
	Daily     []OwnerUsageDay  `json:"daily"`
	ByApp     []AdminUsageApp  `json:"by_app"`
	TopLinks  []OwnerUsageLink `json:"top_links"`
}

// ResolveAdminRange defaults to the current JST calendar month up to today.
func ResolveAdminRange(now time.Time, rawFrom, rawTo string) (from, to string, err error) {
	jst := now.In(model.JST())
	from = model.JSTDay(time.Date(jst.Year(), jst.Month(), 1, 0, 0, 0, 0, model.JST()))
	to = model.JSTDay(now)
	if rawFrom != "" {
		from = rawFrom
	}
	if rawTo != "" {
		to = rawTo
	}
	if err := checkRange(from, to, MaxAdminRangeDays); err != nil {
		return "", "", err
	}
	return from, to, nil
}

func checkRange(from, to string, maxDays int) error {
	fromT, ok := model.ParseJSTDay(from)
	if !ok {
		return ErrInvalidRange
	}
	toT, ok := model.ParseJSTDay(to)
	if !ok || toT.Before(fromT) || model.DaySpan(fromT, toT) > maxDays {
		return ErrInvalidRange
	}
	return nil
}

// StoreClientIDs lists every site that has minted a purchase or coupon link.
func (s *Service) StoreClientIDs(ctx context.Context) ([]string, error) {
	var ids []string
	err := s.db.WithContext(ctx).Raw(`
		SELECT client_id FROM store_purchase_links
		UNION
		SELECT client_id FROM store_coupon_links
		ORDER BY client_id`).Scan(&ids).Error
	return ids, err
}

func (s *Service) AdminUsage(ctx context.Context, apps []AdminApp, from, to string) (*AdminUsage, error) {
	out := &AdminUsage{
		From: from, To: to,
		Daily:    denseDays(from, to),
		ByApp:    []AdminUsageApp{},
		TopLinks: []OwnerUsageLink{},
	}
	ids := make([]string, len(apps))
	names := make(map[string]string, len(apps))
	for i, a := range apps {
		ids[i] = a.ClientID
		names[a.ClientID] = a.Name
	}
	tally, err := s.tallyRange(ctx, ids, names, from, to, out.Daily)
	if err != nil {
		return nil, err
	}
	out.Total, out.Uniques, out.Bots = tally.total, tally.uniques, tally.bots

	for _, a := range apps {
		t := tally.apps[a.ClientID]
		row := AdminUsageApp{
			ClientID: a.ClientID, Name: a.Name, OwnerUserID: a.OwnerUserID,
			SettlementEligible: a.SettlementEligible,
			Links:              t.Links, Total: t.Total, Uniques: t.Uniques, Bots: t.Bots,
		}
		if out.Uniques > 0 {
			row.SharePPM = divRound(t.Uniques*1_000_000, out.Uniques)
		}
		out.LinkCount += row.Links
		out.ByApp = append(out.ByApp, row)
	}
	slices.SortFunc(out.ByApp, func(a, b AdminUsageApp) int {
		return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(b.Total, a.Total), cmp.Compare(a.ClientID, b.ClientID))
	})

	links := tally.links
	slices.SortFunc(links, func(a, b OwnerUsageLink) int {
		return cmp.Or(cmp.Compare(b.Uniques, a.Uniques), cmp.Compare(b.Total, a.Total), compareLinks(a, b))
	})
	out.TopLinks = links[:min(len(links), adminTopLinks)]
	return out, nil
}

// clientUniques sums each site's de-duplicated clicks over [from, to].
func (s *Service) clientUniques(ctx context.Context, clientIDs []string, from, to string) (map[string]int64, error) {
	tally, err := s.tallyRange(ctx, clientIDs, nil, from, to, nil)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(tally.apps))
	for id, a := range tally.apps {
		out[id] = a.Uniques
	}
	return out, nil
}
