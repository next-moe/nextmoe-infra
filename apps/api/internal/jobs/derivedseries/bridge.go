package derivedseries

import (
	"sort"
	"strings"

	"api/internal/jobs/seriesorder"

	"golang.org/x/text/unicode/norm"
)

const crossoverGuardRunes = 2

func crossoverBridges(works []int64, edgesByWork map[int64][]seriesorder.Edge,
	titles map[int64]string) []int64 {
	hasStrong := map[int64]bool{}
	outWeak := map[int64][]int64{}
	sd := map[int64]int64{}
	for _, w := range works {
		sd[w] = w
	}
	var find func(int64) int64
	find = func(x int64) int64 {
		if sd[x] != x {
			sd[x] = find(sd[x])
		}
		return sd[x]
	}
	for _, w := range works {
		for _, e := range edgesByWork[w] {
			// remake_of stays out of the bridge test both ways. Counted as strong, it stopped
			// ニトロプラスブラスターズ being cut and welded the 十二神器 lines into one component;
			// counted as weak, it cut "Stay." (side story of ONE, remake of another ONE) as a bridge.
			if e.A != w || e.Type == seriesorder.RelRemakeOf {
				continue
			}
			if StrongEdgeTypes[e.Type] {
				hasStrong[e.A], hasStrong[e.B] = true, true
				ra, rb := find(e.A), find(e.B)
				if ra != rb {
					if rb < ra {
						ra, rb = rb, ra
					}
					sd[rb] = ra
				}
			} else {
				outWeak[e.A] = append(outWeak[e.A], e.B)
			}
		}
	}

	var bridges []int64
	for _, w := range works {
		if hasStrong[w] || len(outWeak[w]) < 2 {
			continue
		}
		roots := map[int64]struct{}{}
		for _, tgt := range outWeak[w] {
			roots[find(tgt)] = struct{}{}
		}
		if len(roots) < 2 {
			continue
		}
		if sameFranchise(outWeak[w], titles) {
			continue
		}
		bridges = append(bridges, w)
	}
	sort.Slice(bridges, func(i, j int) bool { return bridges[i] < bridges[j] })
	return bridges
}

func sameFranchise(targets []int64, titles map[int64]string) bool {
	folded := make([]string, 0, len(targets))
	for _, t := range targets {
		if s := strings.ToLower(norm.NFKC.String(titles[t])); strings.TrimSpace(s) != "" {
			folded = append(folded, s)
		}
	}
	for i := 0; i < len(folded); i++ {
		for j := i + 1; j < len(folded); j++ {
			if commonPrefixRunes(folded[i], folded[j]) < crossoverGuardRunes {
				return false
			}
		}
	}
	return true
}

func commonPrefixRunes(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	n := 0
	for n < len(ra) && n < len(rb) && ra[n] == rb[n] {
		n++
	}
	return n
}

func pruneBridgeEdges(edgesByWork map[int64][]seriesorder.Edge, bridges []int64) (map[int64][]seriesorder.Edge, int) {
	cut := map[int64]bool{}
	for _, b := range bridges {
		cut[b] = true
	}
	out := make(map[int64][]seriesorder.Edge, len(edgesByWork))
	dropped := map[[2]int64]bool{}
	for w, es := range edgesByWork {
		keep := es[:0:0]
		for _, e := range es {
			if !StrongEdgeTypes[e.Type] && cut[e.A] {
				dropped[[2]int64{e.A, e.B}] = true
				continue
			}
			keep = append(keep, e)
		}
		out[w] = keep
	}
	return out, len(dropped)
}
