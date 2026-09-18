package importer

import "sort"

func (s *dlGamesSnap) buildGroups() []dlGameGroup {
	var nodes []string
	var links [][2]string
	seenNode := map[string]struct{}{}
	addNode := func(n string) {
		if n == "" {
			return
		}
		if _, ok := seenNode[n]; ok {
			return
		}
		seenNode[n] = struct{}{}
		nodes = append(nodes, n)
	}
	for workno, p := range s.byWorkno {
		if p.pack {
			continue
		}
		if _, ok := dlGameTypes[p.workType]; !ok {
			if _, pop := s.population[workno]; !pop {
				continue
			}
		}
		addNode(workno)
		for _, other := range p.links {
			if other == "" || other == workno {
				continue
			}
			if q, ok := s.byWorkno[other]; ok && q.pack {
				addNode(other)
				links = append(links, [2]string{workno, other})
				continue
			}
			addNode(other)
			links = append(links, [2]string{workno, other})
		}
	}
	for workno := range s.population {
		addNode(workno)
	}
	raw := DLsiteUnionFind(nodes, links)
	out := make([]dlGameGroup, 0, len(raw))
	for _, worknos := range raw {
		g := dlGameGroup{worknos: worknos}
		for _, wn := range worknos {
			if _, ok := s.population[wn]; !ok {
				continue
			}
			g.pop = append(g.pop, s.byWorkno[wn])
		}
		if len(g.pop) == 0 {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		return DLsitePrimaryLess(out[i].primaryKey(), out[j].primaryKey())
	})
	return out
}

func (s *dlGamesSnap) decideGroup(g dlGameGroup, st *DLsiteGamesStats) dlGamePlan {
	if works := s.editionWorks(g); len(works) == 1 {
		st.EditionGroups++
		return s.attachPlan(g, works[0], ruleDLsiteEdition, st)
	} else if len(works) > 1 {
		st.SplitGroups++
		return dlGamePlan{kind: "split", members: g.pop}
	}
	if works := s.declaredWorks(g); len(works) == 1 {
		st.DeclaredGroups++
		return s.attachPlan(g, works[0], ruleDLsiteBgmXlink, st)
	} else if len(works) > 1 {
		st.SplitGroups++
		return dlGamePlan{kind: "split", members: g.pop}
	}
	hits := s.filterHits(g, s.titleHits(g), st)
	switch {
	case len(hits) == 1:
		rule, ok := s.corroborated(g, hits[0])
		if ok {
			st.TitleAttachedGroups++
			return s.attachPlan(g, hits[0], rule, st)
		}
		// Uncorroborated unique title hits were the anchored work 3,296 of 3,540
		// times (93.1%) on the 2026-09-18 hold-out; corroborated hits were 10,109
		// of 10,137 (99.72%). An attach is silent, so uncorroborated hits mint
		// quarantined for the nightly judge.
		return s.mintPlan(g, hits, true, st)
	case len(hits) > 1:
		return s.mintPlan(g, hits, true, st)
	default:
		return s.mintPlan(g, nil, false, st)
	}
}

func (s *dlGamesSnap) editionWorks(g dlGameGroup) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, wn := range g.worknos {
		for _, id := range s.heldLiveStub[wn] {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *dlGamesSnap) declaredWorks(g dlGameGroup) []int64 {
	seen := map[int64]struct{}{}
	var ids []int64
	for _, wn := range g.worknos {
		for _, id := range s.declared[wn] {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *dlGamesSnap) titleHits(g dlGameGroup) []int64 {
	seen := map[int64]struct{}{}
	for _, p := range g.pop {
		for _, k := range p.keys() {
			for _, id := range s.titleIndex[k] {
				seen[id] = struct{}{}
			}
		}
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func (s *dlGamesSnap) corroborated(g dlGameGroup, workID int64) (string, bool) {
	for _, p := range g.pop {
		if s.circleOK(p, workID) {
			return ruleDLsiteTitleCircle, true
		}
	}
	for _, p := range g.pop {
		if s.dateOK(p, workID) {
			return ruleDLsiteTitleDate, true
		}
	}
	for _, p := range g.pop {
		if s.bgmDateOK(p, workID) {
			return ruleDLsiteTitleBgm, true
		}
	}
	return "", false
}

func (s *dlGamesSnap) circleOK(p dlGameProd, workID int64) bool {
	if p.makerExt == "" {
		return false
	}
	labels := s.workLabels[workID]
	if len(labels) == 0 {
		return false
	}
	for _, lid := range s.makerLabel[p.makerExt] {
		if _, ok := labels[lid]; ok {
			return true
		}
	}
	return false
}

func (s *dlGamesSnap) dateOK(p dlGameProd, workID int64) bool {
	if p.ymd == "" {
		return false
	}
	for _, d := range s.workDates[workID] {
		if d == p.ymd {
			return true
		}
	}
	return false
}

func (s *dlGamesSnap) bgmDateOK(p dlGameProd, workID int64) bool {
	if p.ymd == "" {
		return false
	}
	for _, d := range s.bgmDates[workID] {
		if d == p.ymd {
			return true
		}
	}
	return false
}

func (s *dlGamesSnap) attachPlan(g dlGameGroup, workID int64, rule string, st *DLsiteGamesStats) dlGamePlan {
	kept := make([]dlGameProd, 0, len(g.pop))
	for _, p := range g.pop {
		if s.rejectedPair(workID, p.workno) {
			st.RejectedSkips++
			continue
		}
		kept = append(kept, p)
	}
	return dlGamePlan{kind: "attach", rule: rule, workID: workID, members: kept}
}

func (s *dlGamesSnap) mintPlan(g dlGameGroup, hits []int64, quarantine bool, st *DLsiteGamesStats) dlGamePlan {
	return dlGamePlan{
		kind: "mint", rule: ruleDLsiteGameImport, members: g.pop,
		quarantine: quarantine, hits: capHits(hits),
	}
}

func (s *dlGamesSnap) rejectedPair(workID int64, workno string) bool {
	_, ok := s.rejected[dlRejKey(workID, workno)]
	return ok
}

func (s *dlGamesSnap) filterHits(g dlGameGroup, hits []int64, st *DLsiteGamesStats) []int64 {
	out := make([]int64, 0, len(hits))
	for _, id := range hits {
		skip := false
		for _, p := range g.pop {
			if s.rejectedPair(id, p.workno) {
				skip = true
				st.RejectedSkips++
			}
		}
		if skip {
			continue
		}
		out = append(out, id)
	}
	return out
}

func capHits(hits []int64) []int64 {
	sort.Slice(hits, func(i, j int) bool { return hits[i] < hits[j] })
	if len(hits) > 3 {
		return hits[:3]
	}
	return hits
}

func (s *dlGamesSnap) foldIntraBatch(plans []dlGamePlan, st *DLsiteGamesStats) []dlGamePlan {
	var attach []dlGamePlan
	var mint []dlGamePlan
	var other []dlGamePlan
	for _, p := range plans {
		switch p.kind {
		case "attach":
			attach = append(attach, p)
		case "mint":
			mint = append(mint, p)
		default:
			other = append(other, p)
		}
	}
	if len(mint) == 0 {
		out := make([]dlGamePlan, 0, len(attach)+len(other))
		out = append(out, attach...)
		out = append(out, other...)
		return out
	}
	keys := make([][]string, len(mint))
	for i, p := range mint {
		seen := map[string]struct{}{}
		var ks []string
		for _, m := range p.members {
			for _, k := range m.keys() {
				if _, ok := seen[k]; ok {
					continue
				}
				seen[k] = struct{}{}
				ks = append(ks, k)
			}
		}
		keys[i] = ks
	}
	sets := DLsiteTransitiveSets(keys, func(i, j int) bool { return shareMaker(mint[i], mint[j]) })
	folded := make([]dlGamePlan, 0, len(sets))
	for _, set := range sets {
		primary := set[0]
		for _, idx := range set[1:] {
			if DLsitePrimaryLess(planPrimaryKey(mint[idx]), planPrimaryKey(mint[primary])) {
				primary = idx
			}
		}
		p := mint[primary]
		seenWN := map[string]struct{}{}
		for _, m := range p.members {
			seenWN[m.workno] = struct{}{}
		}
		hitSet := map[int64]struct{}{}
		for _, h := range p.hits {
			hitSet[h] = struct{}{}
		}
		foldedN := 0
		for _, idx := range set {
			if idx == primary {
				continue
			}
			foldedN++
			o := mint[idx]
			if o.quarantine {
				p.quarantine = true
			}
			for _, m := range o.members {
				if _, ok := seenWN[m.workno]; ok {
					continue
				}
				seenWN[m.workno] = struct{}{}
				p.members = append(p.members, m)
			}
			for _, h := range o.hits {
				hitSet[h] = struct{}{}
			}
		}
		hits := make([]int64, 0, len(hitSet))
		for h := range hitSet {
			hits = append(hits, h)
		}
		p.hits = capHits(hits)
		p.folded = foldedN
		if p.quarantine {
			st.QuarantinedGroups++
		}
		st.MintedGroups++
		st.FoldedGroups += foldedN
		folded = append(folded, p)
	}
	out := make([]dlGamePlan, 0, len(attach)+len(folded)+len(other))
	out = append(out, attach...)
	out = append(out, folded...)
	out = append(out, other...)
	return out
}

// A shared title alone does not make two DLsite groups one game: doujin titles
// repeat across circles, and a fold hides the second game for good, while two
// works with one name are paired by the dedup census. A port by another maker
// (the 【スマホ版】 publisher) still reaches the judge that way.
func shareMaker(a, b dlGamePlan) bool {
	makers := map[string]struct{}{}
	for _, m := range a.members {
		if m.makerExt != "" {
			makers[m.makerExt] = struct{}{}
		}
	}
	for _, m := range b.members {
		if _, ok := makers[m.makerExt]; ok {
			return true
		}
	}
	return false
}

func planPrimaryKey(p dlGamePlan) DLsitePrimaryKey {
	g := dlGameGroup{pop: p.members}
	g.worknos = make([]string, len(p.members))
	for i, m := range p.members {
		g.worknos[i] = m.workno
	}
	return g.primaryKey()
}

func applyDLsiteGamesLimit(plans []dlGamePlan, limit int, st *DLsiteGamesStats) []dlGamePlan {
	if limit <= 0 {
		return plans
	}
	var attach, mint []dlGamePlan
	for _, p := range plans {
		if p.kind == "mint" {
			mint = append(mint, p)
		} else {
			attach = append(attach, p)
		}
	}
	sort.Slice(mint, func(i, j int) bool {
		return DLsitePrimaryLess(planPrimaryKey(mint[i]), planPrimaryKey(mint[j]))
	})
	if len(mint) <= limit {
		return plans
	}
	kept := mint[:limit]
	for _, p := range mint[limit:] {
		st.LimitedGroups += 1 + p.folded
		st.MintedGroups--
		st.FoldedGroups -= p.folded
		if p.quarantine {
			st.QuarantinedGroups--
		}
	}
	out := append(attach, kept...)
	return out
}

func countPlanWrites(plans []dlGamePlan, st *DLsiteGamesStats) {
	for _, p := range plans {
		if p.kind != "attach" && p.kind != "mint" {
			continue
		}
		st.ReleasesPlanned += len(p.members)
		st.RefsPlanned += len(p.members)
		if p.kind == "mint" {
			st.CandidatesPlanned += len(p.hits)
		}
	}
}

func receiptsFromPlans(plans []dlGamePlan) []DLsiteGamesReceipt {
	out := make([]DLsiteGamesReceipt, 0, len(plans))
	for _, p := range plans {
		wns := make([]string, len(p.members))
		for i, m := range p.members {
			wns[i] = m.workno
		}
		sort.Strings(wns)
		switch p.kind {
		case "attach":
			out = append(out, DLsiteGamesReceipt{Action: "attach", Rule: p.rule, Worknos: wns, WorkID: p.workID})
		case "mint":
			out = append(out, DLsiteGamesReceipt{
				Action: "mint", Rule: p.rule, Worknos: wns,
				Hits: p.hits, Quarantine: p.quarantine,
			})
			if p.folded > 0 {
				out = append(out, DLsiteGamesReceipt{Action: "fold", Worknos: wns, Hits: p.hits, Quarantine: p.quarantine})
			}
		case "split":
			out = append(out, DLsiteGamesReceipt{Action: "split", Worknos: wns})
		}
	}
	return out
}
