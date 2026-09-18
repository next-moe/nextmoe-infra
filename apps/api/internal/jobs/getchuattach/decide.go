package getchuattach

import (
	"fmt"
	"sort"
	"time"

	"api/internal/platform/catalog/titlekey"
)

const (
	actionAttach = "attach"
	actionAnchor = "anchor"
	actionMint   = "mint"

	ruleJanVNDB     = "rule:getchu-jan-vndb"
	ruleJanEG       = "rule:getchu-jan-eg"
	ruleTitleDate   = "rule:getchu-title+date"
	ruleTitleCut    = "rule:getchu-titlecut+date"
	ruleEGBrand     = "rule:getchu-eg-brand+date"
	ruleEGBrandNear = "rule:getchu-eg-brand+near-date"
	ruleWorkImport  = "rule:getchu-work-import"

	maxCandidates = 3

	nearWindow = 31 * 24 * time.Hour

	minEditionRunes = 8
	minCensorRunes  = 6
)

type plannedAction struct {
	Action     string
	GetchuID   string
	GetchuIDs  []string
	WorkID     int64
	ReleaseID  int64
	MatchedBy  string
	Hits       []int64
	ReleasedY  *int16
	ReleasedM  *int16
	ReleasedD  *int16
	Members    []item
	Quarantine bool
	Primary    item
}

func decide(snap snapshot, pop []item) ([]plannedAction, Stats) {
	planned, leftover, st := decideAttach(snap, pop)
	minted := planMints(snap, leftover, &st)
	planned = append(planned, minted...)
	sortPlanned(planned)
	return planned, st
}

func decideAttach(snap snapshot, pop []item) ([]plannedAction, []item, Stats) {
	st := Stats{Population: len(pop)}
	var planned []plannedAction
	var leftover []item
	for _, it := range pop {
		if p, stop := rungJanVNDB(it, snap, &st); stop {
			if p.Action != "" {
				planned = append(planned, p)
			}
			continue
		}
		if p, stop := rungJanEG(it, snap, &st); stop {
			if p.Action != "" {
				planned = append(planned, p)
			}
			continue
		}
		if screened(it, &st) {
			continue
		}
		if p, ok := rungTitleDate(it, snap, &st); ok {
			planned = append(planned, p)
			continue
		}
		if p, ok := rungTitleCut(it, snap, &st); ok {
			planned = append(planned, p)
			continue
		}
		if p, ok := rungEGBrandDate(it, snap, &st); ok {
			planned = append(planned, p)
			continue
		}
		leftover = append(leftover, it)
	}
	return planned, leftover, st
}

func sortPlanned(planned []plannedAction) {
	sort.Slice(planned, func(i, j int) bool {
		a, b := planned[i], planned[j]
		aid, bid := a.sortID(), b.sortID()
		if aid != bid {
			return aid < bid
		}
		if a.Action != b.Action {
			return a.Action < b.Action
		}
		return a.WorkID < b.WorkID
	})
}

func (p plannedAction) sortID() string {
	if p.GetchuID != "" {
		return p.GetchuID
	}
	if len(p.GetchuIDs) > 0 {
		return p.GetchuIDs[0]
	}
	return p.Primary.GetchuID
}

func titleHits(it item, snap snapshot) []int64 {
	seen := map[int64]struct{}{}
	var hits []int64
	for _, k := range titlekey.Keys(it.Title) {
		for _, w := range snap.titleIndex[k] {
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			hits = append(hits, w)
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i] < hits[j] })
	return hits
}

func withoutRejected(hits []int64, getchuID string, snap snapshot, st *Stats) []int64 {
	out := hits[:0:0]
	for _, w := range hits {
		if _, hit := snap.rejected[rejKey(w, getchuID)]; hit {
			st.RejectedSkips++
			continue
		}
		out = append(out, w)
	}
	return out
}

// Only a matching release date corroborates. On the 2026-09-18 hold-out
// (14,931 items anchored on VNDB works) title+date was right 7,887 times and
// wrong 81; a matching brand label was right 31 times and wrong 108, and a
// matching Bangumi date 22 and 247 — mostly a same-brand sequel or a
// per-volume Bangumi entry.
func corroborate(it item, workID int64, snap snapshot) string {
	day := catalogDay(it.ReleaseDate)
	if day == "" {
		return ""
	}
	for _, d := range snap.workDates[workID] {
		if d == day {
			return ruleTitleDate
		}
	}
	return ""
}

// parseGetchuDate reads Getchu's YYYY/MM/DD. Getchu writes 0001/01/01 for an
// unknown date (67 fetched items, 2026-09-18), which is no date at all.
func parseGetchuDate(s string) (time.Time, error) {
	t, err := time.Parse("2006/01/02", s)
	if err != nil {
		return t, err
	}
	if t.Year() < 1970 {
		return time.Time{}, fmt.Errorf("placeholder date %q", s)
	}
	return t, nil
}

func catalogDay(getchuDate string) string {
	t, err := parseGetchuDate(getchuDate)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func releaseParts(getchuDate string, now time.Time) (y, m, d *int16) {
	t, err := parseGetchuDate(getchuDate)
	if err != nil {
		return nil, nil, nil
	}
	if t.After(now.AddDate(2, 0, 0)) {
		return nil, nil, nil
	}
	yy, mm, dd := int16(t.Year()), int16(t.Month()), int16(t.Day())
	return &yy, &mm, &dd
}

func attachOn(it item, workID int64, rule string, snap snapshot) plannedAction {
	y, m, d := releaseParts(it.ReleaseDate, snap.now)
	return plannedAction{
		Action: actionAttach, GetchuID: it.GetchuID, WorkID: workID,
		MatchedBy: rule, Hits: []int64{workID},
		ReleasedY: y, ReleasedM: m, ReleasedD: d,
	}
}

func uniqueIDs(in []int64) []int64 {
	seen := map[int64]struct{}{}
	var out []int64
	for _, id := range in {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
