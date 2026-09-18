package getchuattach

import "time"

const foldWindow = 2 * 365 * 24 * time.Hour

func foldableItems(a, b item) bool {
	if a.BrandID == 0 || a.BrandID != b.BrandID {
		return false
	}
	ta, errA := parseGetchuDate(a.ReleaseDate)
	tb, errB := parseGetchuDate(b.ReleaseDate)
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
			if k == "" {
				continue
			}
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
