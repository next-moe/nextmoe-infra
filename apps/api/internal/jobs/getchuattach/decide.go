package getchuattach

import (
	"sort"
	"time"

	"api/internal/platform/catalog/titlekey"
)

const (
	actionAttach = "attach"

	ruleTitleDate = "rule:getchu-title+date"
)

type plannedAction struct {
	Action    string
	GetchuID  string
	WorkID    int64
	MatchedBy string
	Hits      []int64
	ReleasedY *int16
	ReleasedM *int16
	ReleasedD *int16
}

func decide(snap snapshot, pop []item) ([]plannedAction, Stats) {
	st := Stats{Population: len(pop)}
	var planned []plannedAction
	for _, it := range pop {
		hits := withoutRejected(titleHits(it, snap), it.GetchuID, snap, &st)
		switch {
		case len(hits) == 0:
			// Decision 2026-08-05 (still binding): Getchu never mints a work. Of
			// 3,028 unanchored items with no title hit (measured 2026-09-18) the
			// mass are KOEI/SEGA PC titles, art books, and a few visual novels
			// not in the catalog yet.
			st.NoHit++
		case len(hits) > 1:
			st.MultiHit++
		default:
			w := hits[0]
			rule := corroborate(it, w, snap)
			if rule == "" {
				// A unique hit whose date does not match (867 of 5,619
				// unanchored, measured 2026-09-18) is mostly another package of
				// the same game — 初回版, DVD-ROM版, 限定版 — but not reliably.
				st.Uncorroborated++
				continue
			}
			y, m, d := releaseParts(it.ReleaseDate, snap.now)
			planned = append(planned, plannedAction{
				Action: actionAttach, GetchuID: it.GetchuID, WorkID: w,
				MatchedBy: rule, Hits: []int64{w},
				ReleasedY: y, ReleasedM: m, ReleasedD: d,
			})
			st.Attached++
		}
	}
	sort.Slice(planned, func(i, j int) bool {
		if planned[i].GetchuID != planned[j].GetchuID {
			return planned[i].GetchuID < planned[j].GetchuID
		}
		return planned[i].WorkID < planned[j].WorkID
	})
	return planned, st
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
// per-volume Bangumi entry. Getchu never mints, so a skipped item costs nothing.
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

func catalogDay(getchuDate string) string {
	t, err := time.Parse("2006/01/02", getchuDate)
	if err != nil {
		return ""
	}
	return t.Format("2006-01-02")
}

func releaseParts(getchuDate string, now time.Time) (y, m, d *int16) {
	t, err := time.Parse("2006/01/02", getchuDate)
	if err != nil {
		return nil, nil, nil
	}
	if t.After(now.AddDate(2, 0, 0)) {
		return nil, nil, nil
	}
	yy, mm, dd := int16(t.Year()), int16(t.Month()), int16(t.Day())
	return &yy, &mm, &dd
}
