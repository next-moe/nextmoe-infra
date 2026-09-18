package egworks

import "time"

const foldWindow = 2 * 365 * 24 * time.Hour

// A shared title is not enough to fold two games into one work: 27 of the 207
// same-title groups among unanchored EG games on 2026-09-18 spanned brands, and
// some were different games (CARAT 1992 and 1998, touch 2006 and 2022). A fold
// hides the second game for good, while two works with one name are paired by
// the dedup census, so a fold needs the same brand or release dates within two
// years.
func foldable(a, b game) bool {
	if a.BrandID != 0 && a.BrandID == b.BrandID {
		return true
	}
	ta, errA := time.Parse("2006-01-02", a.Sellday)
	tb, errB := time.Parse("2006-01-02", b.Sellday)
	if errA != nil || errB != nil {
		return false
	}
	d := ta.Sub(tb)
	if d < 0 {
		d = -d
	}
	return d <= foldWindow
}

func groupByKeyLists(keys [][]string, compatible func(i, j int) bool) [][]int {
	n := len(keys)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	members := map[string][]int{}
	for i, ks := range keys {
		for _, k := range ks {
			for _, j := range members[k] {
				if compatible(i, j) {
					union(i, j)
				}
			}
			members[k] = append(members[k], i)
		}
	}
	buckets := map[int][]int{}
	var order []int
	for i := 0; i < n; i++ {
		r := find(i)
		if _, ok := buckets[r]; !ok {
			order = append(order, r)
		}
		buckets[r] = append(buckets[r], i)
	}
	out := make([][]int, 0, len(order))
	for _, r := range order {
		out = append(out, buckets[r])
	}
	return out
}

func choosePrimary(cands []mintCand) mintCand {
	best := cands[0]
	for _, c := range cands[1:] {
		if primaryLess(c.game, best.game) {
			best = c
		}
	}
	return best
}

func primaryLess(a, b game) bool {
	if a.Transplant != b.Transplant {
		return !a.Transplant
	}
	aPC, bPC := a.Model == "PC", b.Model == "PC"
	if aPC != bPC {
		return aPC
	}
	aEmpty, bEmpty := a.Sellday == "", b.Sellday == ""
	if aEmpty != bEmpty {
		return !aEmpty
	}
	if a.Sellday != b.Sellday {
		return a.Sellday < b.Sellday
	}
	return a.ID < b.ID
}
