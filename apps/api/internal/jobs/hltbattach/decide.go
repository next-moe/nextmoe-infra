package hltbattach

import (
	"sort"
	"strconv"
	"time"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"
)

const (
	actionAttach  = "attach"
	ruleTitleDate = "rule:hltb-title+date"
)

type plannedAction struct {
	Action    string
	HltbID    int64
	WorkID    int64
	LinkKind  int16
	MatchedBy string
	Hits      []int64
}

func decide(snap snapshot, pop []game) ([]plannedAction, Stats) {
	st := Stats{Population: len(pop)}
	var planned []plannedAction
	for _, g := range pop {
		hits := withoutRejected(titleHits(g, snap), g.ID, snap, &st)
		switch {
		case len(hits) == 0:
			st.NoHit++
		case len(hits) > 1:
			st.MultiHit++
		case !dateMatch(g, hits[0], snap):
			st.Uncorroborated++
		default:
			// A unique titlekey hit (name or alias) whose full release date
			// equals release_world or release_jp named the anchored work
			// 1,016 times and another work twice among 2,168 Steam-anchored
			// games; the same rule attaches 1,664 unanchored games
			// (measured 2026-09-18).
			st.Attached++
			planned = append(planned, plannedAction{
				Action: actionAttach, HltbID: g.ID, WorkID: hits[0],
				LinkKind: model.LinkKindProbable, MatchedBy: ruleTitleDate,
				Hits: hits,
			})
		}
	}
	sort.Slice(planned, func(i, j int) bool { return planned[i].HltbID < planned[j].HltbID })
	return planned, st
}

func titleHits(g game, snap snapshot) []int64 {
	seen := map[int64]struct{}{}
	var hits []int64
	for _, name := range append([]string{g.Name}, splitAliases(g.Alias)...) {
		for _, k := range titlekey.Keys(name) {
			for _, w := range snap.titleIndex[k] {
				if _, dup := seen[w]; dup {
					continue
				}
				seen[w] = struct{}{}
				hits = append(hits, w)
			}
		}
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i] < hits[j] })
	return hits
}

func withoutRejected(hits []int64, hltbID int64, snap snapshot, st *Stats) []int64 {
	ext := strconv.FormatInt(hltbID, 10)
	out := hits[:0:0]
	for _, w := range hits {
		if _, hit := snap.rejected[rejKey(w, ext)]; hit {
			st.RejectedSkips++
			continue
		}
		out = append(out, w)
	}
	return out
}

func dateMatch(g game, workID int64, snap snapshot) bool {
	dates := knownDates(g.World, g.Japan)
	if len(dates) == 0 {
		return false
	}
	want := map[string]struct{}{}
	for _, d := range dates {
		want[d] = struct{}{}
	}
	for _, d := range snap.workDates[workID] {
		if _, ok := want[d]; ok {
			return true
		}
	}
	return false
}

func knownDates(world, jp string) []string {
	var out []string
	seen := map[string]struct{}{}
	for _, raw := range []string{world, jp} {
		t, err := time.Parse("2006-01-02", raw)
		if err != nil {
			continue
		}
		d := t.Format("2006-01-02")
		if _, dup := seen[d]; dup {
			continue
		}
		seen[d] = struct{}{}
		out = append(out, d)
	}
	return out
}
