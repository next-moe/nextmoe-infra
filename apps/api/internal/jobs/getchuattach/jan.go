package getchuattach

import "sort"

func rungJanVNDB(it item, snap snapshot, st *Stats) (plannedAction, bool) {
	if it.JAN == 0 {
		return plannedAction{}, false
	}
	hits := snap.janVNDB[it.JAN]
	if len(hits) == 0 {
		return plannedAction{}, false
	}
	works := withoutRejected(uniqueHitWorks(hits), it.GetchuID, snap, st)
	if len(works) != 1 {
		return plannedAction{}, false
	}
	w := works[0]
	// JAN→VNDB anchored the item's own work 14,523 times and another 29 on the
	// 2026-09-18 hold-out. An EG answer without that work names its twin (386
	// hold-out items): neither side is proven, so the item waits for the merge.
	egWorks := uniqueIDs(snap.janEG[it.JAN])
	if len(egWorks) > 0 && !containsID(egWorks, w) {
		st.JanConflict++
		return plannedAction{}, true
	}
	relID := lowestRelease(hits, w)
	st.JanVNDB++
	st.Attached++
	return plannedAction{
		Action: actionAnchor, GetchuID: it.GetchuID, WorkID: w, ReleaseID: relID,
		MatchedBy: ruleJanVNDB, Hits: []int64{w},
	}, true
}

func rungJanEG(it item, snap snapshot, st *Stats) (plannedAction, bool) {
	if it.JAN == 0 {
		return plannedAction{}, false
	}
	works := withoutRejected(uniqueIDs(snap.janEG[it.JAN]), it.GetchuID, snap, st)
	if len(works) != 1 {
		return plannedAction{}, false
	}
	st.JanEG++
	st.Attached++
	return attachOn(it, works[0], ruleJanEG, snap), true
}

func uniqueHitWorks(hits []releaseHit) []int64 {
	var ids []int64
	for _, h := range hits {
		ids = append(ids, h.WorkID)
	}
	out := uniqueIDs(ids)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func lowestRelease(hits []releaseHit, workID int64) int64 {
	var id int64
	for _, h := range hits {
		if h.WorkID != workID {
			continue
		}
		if id == 0 || h.ReleaseID < id {
			id = h.ReleaseID
		}
	}
	return id
}

func containsID(ids []int64, want int64) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
