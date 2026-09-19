package service

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"time"

	"api/internal/platform/store/model"
)

const (
	DefaultOwnerUsageDays = 30
	MaxOwnerUsageDays     = MaxStatsRangeDays
)

// OwnerApp is the slice of an application the portal panel needs. The full row
// belongs to devapi, and the caller passes only these two fields so the store
// domain does not reach into the developer-platform tables.
type OwnerApp struct {
	ClientID string
	Name     string
}

type OwnerUsageDay struct {
	Day     string `json:"day"`
	Total   int64  `json:"total"`
	Uniques int64  `json:"uniques"`
	Bots    int64  `json:"bots"`
}

type OwnerUsageApp struct {
	ClientID string `json:"client_id"`
	Name     string `json:"name"`
	Links    int64  `json:"links"`
	Total    int64  `json:"total"`
	Uniques  int64  `json:"uniques"`
	Bots     int64  `json:"bots"`
}

type OwnerUsageLink struct {
	ClientID   string  `json:"client_id"`
	AppName    string  `json:"app_name"`
	Kind       string  `json:"kind"`
	ProductID  *string `json:"product_id"`
	CampaignID *int64  `json:"campaign_id"`
	Total      int64   `json:"total"`
	Uniques    int64   `json:"uniques"`
	Bots       int64   `json:"bots"`
}

type OwnerUsageSummary struct {
	Days      int              `json:"days"`
	Since     string           `json:"since"`
	Until     string           `json:"until"`
	Total     int64            `json:"total"`
	Uniques   int64            `json:"uniques"`
	Bots      int64            `json:"bots"`
	LinkCount int64            `json:"link_count"`
	Daily     []OwnerUsageDay  `json:"daily"`
	ByApp     []OwnerUsageApp  `json:"by_app"`
	ByLink    []OwnerUsageLink `json:"by_link"`
}

func ClampOwnerUsageDays(days int) int {
	if days <= 0 {
		return DefaultOwnerUsageDays
	}
	if days > MaxOwnerUsageDays {
		return MaxOwnerUsageDays
	}
	return days
}

func (s *Service) OwnerUsage(ctx context.Context, apps []OwnerApp, days int) (*OwnerUsageSummary, error) {
	days = ClampOwnerUsageDays(days)
	now := time.Now()
	until := model.JSTDay(now)
	since := model.JSTDay(now.AddDate(0, 0, -(days - 1)))

	out := &OwnerUsageSummary{
		Days: days, Since: since, Until: until,
		Daily:  denseDays(since, until),
		ByApp:  []OwnerUsageApp{},
		ByLink: []OwnerUsageLink{},
	}
	if len(apps) == 0 {
		return out, nil
	}

	names := make(map[string]string, len(apps))
	clientIDs := make([]string, len(apps))
	for i, a := range apps {
		clientIDs[i] = a.ClientID
		names[a.ClientID] = a.Name
	}
	tally, err := s.tallyRange(ctx, clientIDs, names, since, until, out.Daily)
	if err != nil {
		return nil, err
	}
	out.Total, out.Uniques, out.Bots = tally.total, tally.uniques, tally.bots

	for _, a := range apps {
		row := tally.apps[a.ClientID]
		row.ClientID, row.Name = a.ClientID, a.Name
		if row.Links == 0 && row.Total == 0 {
			continue
		}
		out.LinkCount += row.Links
		out.ByApp = append(out.ByApp, row)
	}
	slices.SortFunc(out.ByApp, func(a, b OwnerUsageApp) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), cmp.Compare(a.ClientID, b.ClientID))
	})

	out.ByLink = tally.links
	slices.SortFunc(out.ByLink, func(a, b OwnerUsageLink) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), compareLinks(a, b))
	})
	return out, nil
}

// rangeTally is the click counts of a set of sites over a closed JST-day range.
type rangeTally struct {
	total, uniques, bots int64
	apps                 map[string]OwnerUsageApp
	links                []OwnerUsageLink
}

// tallyRange adds every link-day of clientIDs in [since, until] into daily (in
// place, matched by day) and returns the per-site and per-link sums. Every
// client ID gets an apps entry, its link count filled even without clicks.
func (s *Service) tallyRange(ctx context.Context, clientIDs []string, names map[string]string, since, until string, daily []OwnerUsageDay) (*rangeTally, error) {
	out := &rangeTally{apps: make(map[string]OwnerUsageApp, len(clientIDs)), links: []OwnerUsageLink{}}
	if len(clientIDs) == 0 {
		return out, nil
	}
	linkCounts, err := s.linkCounts(ctx, clientIDs)
	if err != nil {
		return nil, err
	}
	rows, err := s.ownerStatRows(ctx, clientIDs, since, until)
	if err != nil {
		return nil, err
	}

	byDay := make(map[string]int, len(daily))
	for i, d := range daily {
		byDay[d.Day] = i
	}
	for _, id := range clientIDs {
		out.apps[id] = OwnerUsageApp{ClientID: id, Name: names[id], Links: linkCounts[id]}
	}
	perLink := map[string]*OwnerUsageLink{}

	for _, r := range rows {
		out.total += r.Total
		out.uniques += r.Uniques
		out.bots += r.Bots
		if i, ok := byDay[r.Day]; ok {
			daily[i].Total += r.Total
			daily[i].Uniques += r.Uniques
			daily[i].Bots += r.Bots
		}

		app := out.apps[r.ClientID]
		app.Total += r.Total
		app.Uniques += r.Uniques
		app.Bots += r.Bots
		out.apps[r.ClientID] = app

		key := r.ClientID + "\x00" + r.Kind + "\x00" + derefString(r.ProductID) + "\x00" + derefInt64(r.CampaignID)
		link, ok := perLink[key]
		if !ok {
			link = &OwnerUsageLink{
				ClientID: r.ClientID, AppName: names[r.ClientID], Kind: r.Kind,
				ProductID: r.ProductID, CampaignID: r.CampaignID,
			}
			perLink[key] = link
		}
		link.Total += r.Total
		link.Uniques += r.Uniques
		link.Bots += r.Bots
	}
	for _, l := range perLink {
		out.links = append(out.links, *l)
	}
	return out, nil
}

func compareLinks(a, b OwnerUsageLink) int {
	return cmp.Or(
		cmp.Compare(a.ClientID, b.ClientID),
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(derefString(a.ProductID), derefString(b.ProductID)),
		cmp.Compare(derefInt64(a.CampaignID), derefInt64(b.CampaignID)),
	)
}

type ownerStatRow struct {
	ClientID   string
	Kind       string
	ProductID  *string
	CampaignID *int64
	Day        string
	Total      int64
	Uniques    int64
	Bots       int64
}

const ownerStatsSQL = `
SELECT p.client_id, 'purchase' AS kind, p.product_id AS product_id, NULL::bigint AS campaign_id,
       s.day, s.total, s.uniques, s.bots
  FROM store_link_daily_stats s
  JOIN store_purchase_links p ON p.alias = s.alias
 WHERE p.client_id IN ? AND s.day >= ? AND s.day <= ?
UNION ALL
SELECT c.client_id, 'coupon' AS kind, NULL::text AS product_id, c.campaign_id,
       s.day, s.total, s.uniques, s.bots
  FROM store_link_daily_stats s
  JOIN store_coupon_links c ON c.alias = s.alias
 WHERE c.client_id IN ? AND s.day >= ? AND s.day <= ?`

func (s *Service) ownerStatRows(ctx context.Context, clientIDs []string, from, to string) ([]ownerStatRow, error) {
	var rows []ownerStatRow
	err := s.db.WithContext(ctx).
		Raw(ownerStatsSQL, clientIDs, from, to, clientIDs, from, to).
		Scan(&rows).Error
	return rows, err
}

const linkCountSQL = `
SELECT client_id, count(*) AS n FROM store_purchase_links WHERE client_id IN ? GROUP BY client_id
UNION ALL
SELECT client_id, count(*) AS n FROM store_coupon_links WHERE client_id IN ? GROUP BY client_id`

func (s *Service) linkCounts(ctx context.Context, clientIDs []string) (map[string]int64, error) {
	var rows []struct {
		ClientID string
		N        int64
	}
	if err := s.db.WithContext(ctx).Raw(linkCountSQL, clientIDs, clientIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, r := range rows {
		out[r.ClientID] += r.N
	}
	return out, nil
}

// denseDays lists every JST day of the closed range [since, until], zeroed.
func denseDays(since, until string) []OwnerUsageDay {
	from, ok := model.ParseJSTDay(since)
	if !ok {
		return []OwnerUsageDay{}
	}
	to, ok := model.ParseJSTDay(until)
	if !ok {
		return []OwnerUsageDay{}
	}
	out := make([]OwnerUsageDay, 0, model.DaySpan(from, to))
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		out = append(out, OwnerUsageDay{Day: model.JSTDay(d)})
	}
	return out
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func derefInt64(v *int64) string {
	if v == nil {
		return ""
	}
	return strconv.FormatInt(*v, 10)
}
