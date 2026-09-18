package importer

import "sort"

type DLsitePrimaryKey struct {
	HasJPN bool
	YMD    string
	Workno string
}

func DLsitePrimaryLess(a, b DLsitePrimaryKey) bool {
	if a.HasJPN != b.HasJPN {
		return a.HasJPN
	}
	if a.YMD != b.YMD {
		if a.YMD == "" {
			return false
		}
		if b.YMD == "" {
			return true
		}
		return a.YMD < b.YMD
	}
	return a.Workno < b.Workno
}

func DLsiteUnionFind(nodes []string, links [][2]string) [][]string {
	u := newDLUF()
	for _, n := range nodes {
		if n != "" {
			u.add(n)
		}
	}
	for _, e := range links {
		if e[0] == "" || e[1] == "" {
			continue
		}
		u.add(e[0])
		u.add(e[1])
		u.union(e[0], e[1])
	}
	buckets := map[string][]string{}
	for n := range u.parent {
		r := u.find(n)
		buckets[r] = append(buckets[r], n)
	}
	out := make([][]string, 0, len(buckets))
	for _, g := range buckets {
		sort.Strings(g)
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func DLsiteTransitiveSets(groupKeys [][]string, compatible func(i, j int) bool) [][]int {
	n := len(groupKeys)
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
		if ra == rb {
			return
		}
		if ra < rb {
			parent[rb] = ra
			return
		}
		parent[ra] = rb
	}
	keyTo := map[string][]int{}
	for i, keys := range groupKeys {
		seen := map[string]struct{}{}
		for _, k := range keys {
			if k == "" {
				continue
			}
			if _, ok := seen[k]; ok {
				continue
			}
			seen[k] = struct{}{}
			keyTo[k] = append(keyTo[k], i)
		}
	}
	for _, gs := range keyTo {
		for i := 1; i < len(gs); i++ {
			for j := 0; j < i; j++ {
				if compatible(gs[i], gs[j]) {
					union(gs[i], gs[j])
				}
			}
		}
	}
	buckets := map[int][]int{}
	for i := 0; i < n; i++ {
		r := find(i)
		buckets[r] = append(buckets[r], i)
	}
	out := make([][]int, 0, len(buckets))
	for _, s := range buckets {
		sort.Ints(s)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

type dlUF struct {
	parent map[string]string
}

func newDLUF() *dlUF {
	return &dlUF{parent: map[string]string{}}
}

func (u *dlUF) add(x string) {
	if _, ok := u.parent[x]; !ok {
		u.parent[x] = x
	}
}

func (u *dlUF) find(x string) string {
	for u.parent[x] != x {
		u.parent[x] = u.parent[u.parent[x]]
		x = u.parent[x]
	}
	return x
}

func (u *dlUF) union(a, b string) {
	ra, rb := u.find(a), u.find(b)
	if ra == rb {
		return
	}
	if ra < rb {
		u.parent[rb] = ra
		return
	}
	u.parent[ra] = rb
}
