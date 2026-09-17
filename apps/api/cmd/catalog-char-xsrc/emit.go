package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"api/internal/jobs/personadj"
)

const autoConfidence = 0.95

type emitStats struct {
	Pairs, Judged, Merge, Distinct, Unsure  int
	AutoPairs, Review, InstanceDeferred     int
	Groups, SameSourceDeferred, GroupsFinal int
	QualifiedHeld, DeclinedDeferred         int
}

func runEmit(pairsPath, verdictsPath, worklistPath, reviewPath string, out io.Writer) error {
	pairs, err := loadPairs(pairsPath)
	if err != nil {
		return err
	}
	verdicts, err := personadj.LoadVerdicts(verdictsPath)
	if err != nil {
		return err
	}
	byKey := make(map[string]personadj.Verdict, len(verdicts))
	for _, v := range verdicts {
		byKey[v.Key] = v
	}

	st := emitStats{Pairs: len(pairs)}
	parent := map[int64]int64{}
	var find func(int64) int64
	find = func(x int64) int64 {
		if p, ok := parent[x]; ok && p != x {
			r := find(p)
			parent[x] = r
			return r
		}
		if _, ok := parent[x]; !ok {
			parent[x] = x
		}
		return parent[x]
	}
	union := func(a, b int64) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[rb] = ra
		}
	}

	var review []string
	declined := map[[2]int64]bool{}
	for _, p := range pairs {
		key := fmt.Sprintf("xsrc:%d:%d", p.A, p.B)
		v, ok := byKey[key]
		if !ok {
			continue
		}
		st.Judged++
		if v.Verdict != personadj.VerdictMerge || p.Instance || p.Qualified || v.Confidence < autoConfidence {
			declined[pairID(p.A, p.B)] = true
		}
		switch v.Verdict {
		case personadj.VerdictMerge:
			st.Merge++
			switch {
			case p.Qualified:
				st.QualifiedHeld++
				review = append(review, reviewLine(p, v, "限定名条目"))
			case p.Instance:
				st.InstanceDeferred++
				review = append(review, reviewLine(p, v, "instance 侧不进自动道"))
			case v.Confidence >= autoConfidence:
				st.AutoPairs++
				union(p.A, p.B)
			default:
				st.Review++
				review = append(review, reviewLine(p, v, "低置信"))
			}
		case personadj.VerdictDistinct:
			st.Distinct++
		default:
			st.Unsure++
			st.Review++
			review = append(review, reviewLine(p, v, "unsure"))
		}
	}

	members := map[int64][]int64{}
	for id := range parent {
		r := find(id)
		members[r] = append(members[r], id)
	}
	declinedRoot := map[int64]bool{}
	for k := range declined {
		_, okA := parent[k[0]]
		_, okB := parent[k[1]]
		if okA && okB && find(k[0]) == find(k[1]) {
			declinedRoot[find(k[0])] = true
		}
	}
	infoBySide := map[int64]sideInfo{}
	richBySide := map[int64]richness{}
	for _, p := range pairs {
		infoBySide[p.A] = sideInfo{Sources: p.ASources, Name: p.AName}
		infoBySide[p.B] = sideInfo{Sources: p.BSources, Name: p.BName}
		richBySide[p.A] = p.ARich
		richBySide[p.B] = p.BRich
	}

	wl, err := os.Create(worklistPath)
	if err != nil {
		return err
	}
	defer wl.Close()
	enc := json.NewEncoder(wl)

	roots := make([]int64, 0, len(members))
	for r := range members {
		roots = append(roots, r)
	}
	sort.Slice(roots, func(i, j int) bool { return roots[i] < roots[j] })
	for _, r := range roots {
		ids := members[r]
		st.Groups++
		seen := map[string]bool{}
		sameSource := false
		for _, id := range ids {
			for _, s := range infoBySide[id].Sources {
				if seen[s] {
					sameSource = true
				}
				seen[s] = true
			}
		}
		if sameSource || declinedRoot[r] {
			var names []string
			for _, id := range ids {
				names = append(names, fmt.Sprintf("%d %s(%s)", id,
					infoBySide[id].Name, strings.Join(infoBySide[id].Sources, "+")))
			}
			why := "组内同源 defer: "
			if sameSource {
				st.SameSourceDeferred++
			} else {
				st.DeclinedDeferred++
				why = "组内含已否决对 defer: "
			}
			review = append(review, why+strings.Join(names, " / "))
			continue
		}
		survivor, sources := pickSurvivor(ids, richBySide)
		if err := enc.Encode(map[string]any{
			"class": "character", "survivor": survivor, "sources": sources,
		}); err != nil {
			return err
		}
		st.GroupsFinal++
	}

	if reviewPath != "" {
		if err := os.WriteFile(reviewPath, []byte(strings.Join(review, "\n")+"\n"), 0o644); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "pairs=%d judged=%d merge=%d distinct=%d unsure=%d auto_pairs=%d "+
		"review=%d instance_deferred=%d qualified_held=%d groups=%d same_source_deferred=%d "+
		"declined_deferred=%d groups_emitted=%d\n",
		st.Pairs, st.Judged, st.Merge, st.Distinct, st.Unsure, st.AutoPairs,
		st.Review, st.InstanceDeferred, st.QualifiedHeld, st.Groups, st.SameSourceDeferred,
		st.DeclinedDeferred, st.GroupsFinal)
	return nil
}

type sideInfo struct {
	Sources []string
	Name    string
}

func pickSurvivor(ids []int64, rich map[int64]richness) (int64, []int64) {
	sorted := append([]int64(nil), ids...)
	sort.Slice(sorted, func(i, j int) bool {
		ra, rb := rich[sorted[i]], rich[sorted[j]]
		if ra.Img != rb.Img {
			return ra.Img
		}
		if ra.NAliases != rb.NAliases {
			return ra.NAliases > rb.NAliases
		}
		return sorted[i] < sorted[j]
	})
	return sorted[0], sorted[1:]
}

func reviewLine(p pairMeta, v personadj.Verdict, why string) string {
	return fmt.Sprintf("[%s] tier=%d %d %s(%s) <-> %d %s(%s) verdict=%s conf=%.2f %s",
		why, p.Tier, p.A, p.AName, strings.Join(p.ASources, "+"),
		p.B, p.BName, strings.Join(p.BSources, "+"), v.Verdict, v.Confidence, v.Reason)
}

func loadPairs(path string) ([]pairMeta, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []pairMeta
	for i, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var p pairMeta
		if err := json.Unmarshal([]byte(line), &p); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, i+1, err)
		}
		out = append(out, p)
	}
	return out, nil
}
