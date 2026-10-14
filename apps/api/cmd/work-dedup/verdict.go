package main

import (
	"sort"
	"time"
)

type bucket string

const (
	bucketAuto           bucket = "auto"
	bucketOutOfScope     bucket = "out-of-scope"
	bucketAnchorConflict bucket = "anchor-conflict"
	bucketRelConflict    bucket = "release-conflict"
	bucketBothKungal     bucket = "both-kungal"
	bucketHiddenClaim    bucket = "hidden-claim"
	bucketDateClash      bucket = "date-clash"
	bucketBare           bucket = "bare"
	bucketBridged        bucket = "bridged"
	bucketAliasOnly      bucket = "alias-only"
	bucketRefCI          bucket = "ref-case"
)

// dateNearDays tolerates cross-source date drift: sources record different
// edition dates for the same work (download vs package), measured at up to
// ~1 month apart on eyeballed true pairs, while true remakes sit years apart.
const dateNearDays = 62

func datesNear(a, b *time.Time) bool {
	if a == nil || b == nil {
		return false
	}
	delta := a.Sub(*b)
	if delta < 0 {
		delta = -delta
	}
	return delta <= dateNearDays*24*time.Hour
}

type mergeGroup struct {
	survivor int64
	sources  []int64
	sample   string
}

func classifyPair(r pairRow) bucket {
	if r.LaneA == "dlsite" && r.LaneB == "dlsite" {
		return bucketOutOfScope
	}
	corroborated := r.RefOverlapCI || r.LabelOverlap || r.SharedNorms >= 2 || datesNear(r.DateA, r.DateB)
	dateClash := r.DateA != nil && r.DateB != nil && r.DateA.Year() != r.DateB.Year()
	switch {
	case r.AnchorConflict:
		return bucketAnchorConflict
	case r.RelConflict:
		return bucketRelConflict
	case r.LaneA == "kungal" && r.LaneB == "kungal":
		return bucketBothKungal
	case r.HiddenA || r.HiddenB:
		// Merging into hidden 61101 took a 42,200-view game off every read face;
		// its hide was a stale backfill, but a moderation ban reads the same, so
		// neither direction is safe without a person.
		return bucketHiddenClaim
	case r.RefOverlap:
		// works 51010/222325 shared bangumi subject 524476 yet were filed manual because the year check ran first.
		return bucketAuto
	case r.SharedNorms == 0:
		return bucketRefCI
	case r.SharedOfficial == 0:
		return bucketAliasOnly
	case dateClash:
		return bucketDateClash
	case !corroborated:
		return bucketBare
	}
	return bucketAuto
}

// classify buckets every pair, then walks the connected components of the
// auto pairs to pick one survivor per group. A component holding two claimed
// kungal works was bridged together by an import mint's titles — merging
// anything there picks between two claims, so the whole component is demoted
// to the manual queue.
func classify(rows []pairRow) ([]bucket, []mergeGroup) {
	verdicts := make([]bucket, len(rows))
	uf := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		r, ok := uf[x]
		if !ok || r == x {
			uf[x] = x
			return x
		}
		root := find(r)
		uf[x] = root
		return root
	}
	union := func(a, b int64) { uf[find(a)] = find(b) }

	type workFacts struct {
		kungal  bool
		anchors int
		name    string
	}
	facts := map[int64]workFacts{}
	for i, r := range rows {
		verdicts[i] = classifyPair(r)
		if verdicts[i] != bucketAuto {
			continue
		}
		facts[r.A] = workFacts{kungal: r.LaneA == "kungal", anchors: r.AnchorsA, name: r.NameA}
		facts[r.B] = workFacts{kungal: r.LaneB == "kungal", anchors: r.AnchorsB, name: r.NameB}
		union(r.A, r.B)
	}

	members := map[int64][]int64{}
	for id := range facts {
		root := find(id)
		members[root] = append(members[root], id)
	}
	demoted := map[int64]bool{}
	var groups []mergeGroup
	for root, ids := range members {
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		kungal := 0
		for _, id := range ids {
			if facts[id].kungal {
				kungal++
			}
		}
		if kungal > 1 {
			demoted[root] = true
			continue
		}
		survivor := ids[0]
		for _, id := range ids[1:] {
			s, c := facts[survivor], facts[id]
			if (c.kungal && !s.kungal) || (c.kungal == s.kungal && c.anchors > s.anchors) {
				survivor = id
			}
		}
		sources := make([]int64, 0, len(ids)-1)
		for _, id := range ids {
			if id != survivor {
				sources = append(sources, id)
			}
		}
		groups = append(groups, mergeGroup{survivor: survivor, sources: sources, sample: facts[survivor].name})
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].survivor < groups[j].survivor })

	for i, r := range rows {
		if verdicts[i] == bucketAuto && demoted[find(r.A)] {
			verdicts[i] = bucketBridged
		}
	}
	return verdicts, groups
}
