package egworks

import (
	"sort"

	"api/internal/platform/catalog/model"
	"api/internal/platform/catalog/titlekey"
)

const (
	actionAttach = "attach"
	actionMint   = "mint"
	actionFold   = "fold"

	rulePack       = "rule:eg-pack"
	ruleTransplant = "rule:eg-transplant"
	ruleTitleDate  = "rule:eg-title+date"
	ruleTitleBrand = "rule:eg-title+brand"
	ruleTitleBgm   = "rule:eg-title+bgmdate"
	ruleWorkImport = "rule:eg-work-import"
	ruleEdition    = "rule:eg-title-edition"

	maxCandidates = 3
)

type plannedAction struct {
	Action     string
	EgID       int64
	WorkID     int64
	LinkKind   int16
	MatchedBy  string
	Hits       []int64
	Primary    game
	Folded     []game
	Quarantine bool
}

type mintCand struct {
	game       game
	hits       []int64
	quarantine bool
}

func decide(snap snapshot, pop []game, limit int) ([]plannedAction, Stats) {
	st := Stats{Population: len(pop)}
	hasPrimary := copyPrimary(snap.workHasPrimary)
	var planned []plannedAction
	var toMint []mintCand

	for _, g := range pop {
		if subs, ok := snap.packSubjects[g.ID]; ok && len(subs) >= 2 {
			// 322 objects bundle two or more different games; 204 objects bundle
			// exactly one subject and are standalone games that ship a bonus, not
			// packs (measured 2026-09-18).
			st.PackGames++
			planned = append(planned, packRefs(snap, g, subs, &st)...)
			continue
		}
		if w, ok := portWork(snap, g); ok {
			if p, skipped := relatedRef(snap, g.ID, w, ruleTransplant, &st); !skipped {
				st.PortGames++
				planned = append(planned, p)
				continue
			}
		}
		hits := withoutRejected(titleHits(g, snap), g.ID, snap, &st)
		corr := corroboratedHits(g, hits, snap)
		if len(corr) == 1 {
			// A unique corroborated title hit was the anchored work in 14,798 of
			// 14,814 hold-out games (99.89%, measured 2026-09-18). A unique hit
			// with neither date nor brand was right only 52% of the time.
			w := corr[0].workID
			kind := model.LinkKindExact
			// A work that already holds an EG ref at link kind 0 or 1 takes related
			// for every new candidate: production consumers were written when every
			// work had exactly one exact EG id (measured 2026-09-18), and a second
			// exact must not appear.
			if hasPrimary[w] {
				kind = model.LinkKindRelated
			}
			if p, skipped := attachRef(snap, g.ID, w, kind, corr[0].rule, &st); !skipped {
				planned = append(planned, p)
				st.Attached++
				if kind == model.LinkKindExact {
					hasPrimary[w] = true
				}
			}
			continue
		}
		if len(hits) > 0 {
			toMint = append(toMint, mintCand{game: g, hits: hits, quarantine: true})
			continue
		}
		toMint = append(toMint, mintCand{game: g, quarantine: false})
	}

	groups := groupMintCands(toMint)
	var mintActions []plannedAction
	for _, grp := range groups {
		primary := choosePrimary(grp)
		quarantine := false
		var allHits []int64
		var folded []game
		for _, c := range grp {
			if c.quarantine {
				quarantine = true
			}
			allHits = append(allHits, c.hits...)
			if c.game.ID != primary.game.ID {
				folded = append(folded, c.game)
			}
		}
		sort.Slice(folded, func(i, j int) bool { return folded[i].ID < folded[j].ID })
		hits := capHits(allHits, maxCandidates)
		mintActions = append(mintActions, plannedAction{
			Action: actionMint, EgID: primary.game.ID,
			LinkKind: model.LinkKindExact, MatchedBy: ruleWorkImport,
			Hits: hits, Primary: primary.game, Folded: folded, Quarantine: quarantine,
		})
	}
	sort.Slice(mintActions, func(i, j int) bool { return mintActions[i].EgID < mintActions[j].EgID })

	kept := mintActions
	if limit > 0 && len(mintActions) > limit {
		kept = mintActions[:limit]
		for _, p := range mintActions[limit:] {
			st.Limited++
			st.Limited += len(p.Folded)
		}
	}
	for _, p := range kept {
		if p.Quarantine {
			st.Quarantined++
			st.CandidatesPlanned += len(p.Hits)
		} else {
			st.MintedLive++
		}
		st.EditionFolded += len(p.Folded)
		st.RefsPlanned++
		st.RefsPlanned += len(p.Folded)
		planned = append(planned, p)
		for _, f := range p.Folded {
			planned = append(planned, plannedAction{
				Action: actionFold, EgID: f.ID,
				LinkKind: model.LinkKindRelated, MatchedBy: ruleEdition,
			})
		}
	}

	sort.Slice(planned, func(i, j int) bool {
		if planned[i].EgID != planned[j].EgID {
			return planned[i].EgID < planned[j].EgID
		}
		if planned[i].Action != planned[j].Action {
			return planned[i].Action < planned[j].Action
		}
		return planned[i].WorkID < planned[j].WorkID
	})
	return planned, st
}

func copyPrimary(in map[int64]bool) map[int64]bool {
	out := make(map[int64]bool, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func packRefs(snap snapshot, g game, subjects []int64, st *Stats) []plannedAction {
	seen := map[int64]struct{}{}
	var out []plannedAction
	for _, sub := range subjects {
		for _, w := range snap.holders[sub] {
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			if p, skipped := relatedRef(snap, g.ID, w, rulePack, st); !skipped {
				out = append(out, p)
			}
		}
	}
	return out
}

func portWork(snap snapshot, g game) (int64, bool) {
	objects := snap.transplantObj[g.ID]
	if len(objects) == 0 {
		return 0, false
	}
	works := map[int64]struct{}{}
	for _, obj := range objects {
		hs := uniqueIDs(snap.holders[obj])
		if len(hs) != 1 {
			continue
		}
		works[hs[0]] = struct{}{}
	}
	if len(works) != 1 {
		return 0, false
	}
	var w int64
	for id := range works {
		w = id
	}
	return w, true
}

func relatedRef(snap snapshot, egID, workID int64, rule string, st *Stats) (plannedAction, bool) {
	return attachRef(snap, egID, workID, model.LinkKindRelated, rule, st)
}

func attachRef(snap snapshot, egID, workID int64, kind int16, rule string, st *Stats) (plannedAction, bool) {
	if _, hit := snap.rejected[rejKey(workID, egID)]; hit {
		st.RejectedSkips++
		return plannedAction{}, true
	}
	st.RefsPlanned++
	return plannedAction{
		Action: actionAttach, EgID: egID, WorkID: workID,
		LinkKind: kind, MatchedBy: rule,
	}, false
}

func titleHits(g game, snap snapshot) []int64 {
	seen := map[int64]struct{}{}
	var hits []int64
	for _, k := range titlekey.Keys(g.Gamename) {
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

func withoutRejected(hits []int64, egID int64, snap snapshot, st *Stats) []int64 {
	out := hits[:0:0]
	for _, w := range hits {
		if _, hit := snap.rejected[rejKey(w, egID)]; hit {
			st.RejectedSkips++
			continue
		}
		out = append(out, w)
	}
	return out
}

type corrHit struct {
	workID int64
	rule   string
}

func corroboratedHits(g game, hits []int64, snap snapshot) []corrHit {
	var out []corrHit
	for _, w := range hits {
		if rule := corroborate(g, w, snap); rule != "" {
			out = append(out, corrHit{workID: w, rule: rule})
		}
	}
	return out
}

func corroborate(g game, workID int64, snap snapshot) string {
	if g.Sellday != "" {
		for _, d := range snap.workDates[workID] {
			if d == g.Sellday {
				return ruleTitleDate
			}
		}
	}
	if g.BrandID != 0 {
		if lid, ok := snap.brandLabel[g.BrandID]; ok {
			if _, has := snap.workLabels[workID][lid]; has {
				return ruleTitleBrand
			}
		}
	}
	// Many works that EG games collide with were minted by a 2026-07-20 Bangumi
	// lane that wrote no release and no label, so their only date is Bangumi's.
	if g.Sellday != "" {
		for _, d := range snap.workBgmDates[workID] {
			if d == g.Sellday {
				return ruleTitleBgm
			}
		}
	}
	return ""
}

func uniqueIDs(in []int64) []int64 {
	seen := map[int64]struct{}{}
	var out []int64
	for _, id := range in {
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func capHits(ids []int64, n int) []int64 {
	u := uniqueIDs(ids)
	sort.Slice(u, func(i, j int) bool { return u[i] < u[j] })
	if len(u) > n {
		u = u[:n]
	}
	return u
}

func groupMintCands(cands []mintCand) [][]mintCand {
	if len(cands) == 0 {
		return nil
	}
	keys := make([][]string, len(cands))
	for i, c := range cands {
		keys[i] = titlekey.Keys(c.game.Gamename)
	}
	idxGroups := groupByKeyLists(keys, func(i, j int) bool { return foldable(cands[i].game, cands[j].game) })
	out := make([][]mintCand, len(idxGroups))
	for i, idxs := range idxGroups {
		grp := make([]mintCand, len(idxs))
		for j, ix := range idxs {
			grp[j] = cands[ix]
		}
		out[i] = grp
	}
	return out
}
