package eganchors

import (
	"sort"
	"strconv"
	"strings"

	"api/internal/platform/catalog/model"
)

const (
	classPrimary = "primary"
	classEdition = "edition"
	classMulti   = "multi"
	classTwin    = "twin"

	ruleMulti = "rule:eg-xlink-multi"
	ruleTwin  = "rule:eg-xlink-twin"
	rulePref  = "rule:eg-xlink:"
)

var familyOrder = []string{"vndb", "egvndb", "dlsite"}

type egGame struct {
	ID       int64
	Gamename string
	VNDB     string
	DLsiteID string
	Model    string
	Sellday  string
}

type holding struct {
	WorkID   int64
	LinkKind int16
}

type snapshot struct {
	games          []egGame
	transplant     map[int64]struct{}
	vndbLinks      map[int64][]string
	vndbWork       map[string]int64
	dlsiteWork     map[string]int64
	egHoldings     map[int64][]holding
	workHasPrimary map[int64]bool
	rejected       map[string]struct{}
	workTitles     map[int64][]string
	workDates      map[int64][]string
}

type plannedRef struct {
	EgID          int64
	WorkID        int64
	LinkKind      int16
	MatchedBy     string
	Class         string
	Families      []string
	Corroboration string
}

type candidate struct {
	game       egGame
	families   []string
	transplant bool
}

type candBucket struct {
	workID int64
	cands  []candidate
}

type classDraft struct {
	planned   []plannedRef
	st        Stats
	byWork    map[int64]*candBucket
	workOrder []int64
}

func rejKey(workID int64, egID int64) string {
	return strconv.FormatInt(workID, 10) + "\x00" + strconv.FormatInt(egID, 10)
}

func decide(snap snapshot) ([]plannedRef, Stats) {
	return finish(snap, classify(snap))
}

func classify(snap snapshot) classDraft {
	d := classDraft{
		st:     Stats{Games: len(snap.games)},
		byWork: map[int64]*candBucket{},
	}

	for _, g := range snap.games {
		held := snap.egHoldings[g.ID]
		inA := map[int64]struct{}{}
		for _, h := range held {
			inA[h.WorkID] = struct{}{}
		}
		if len(inA) > 0 {
			d.st.AnchoredGames++
		}

		vndbWorks, multi := vndbFamily(g.ID, snap)
		if multi {
			d.st.MultiGames++
			for _, w := range vndbWorks {
				if _, ok := inA[w]; ok {
					continue
				}
				appendRelated(&d, snap, plannedRef{
					EgID: g.ID, WorkID: w, LinkKind: model.LinkKindRelated,
					MatchedBy: ruleMulti, Class: classMulti, Families: []string{"vndb"},
				})
			}
			continue
		}

		named := map[int64][]string{}
		add := func(w int64, fam string) {
			if w == 0 {
				return
			}
			for _, have := range named[w] {
				if have == fam {
					return
				}
			}
			named[w] = append(named[w], fam)
		}
		if len(vndbWorks) == 1 {
			add(vndbWorks[0], "vndb")
		}
		if w, ok := snap.vndbWork[g.VNDB]; ok && g.VNDB != "" {
			add(w, "egvndb")
		}
		if w, ok := snap.dlsiteWork[g.DLsiteID]; ok && g.DLsiteID != "" {
			add(w, "dlsite")
		}
		for w := range named {
			named[w] = orderedFamilies(named[w])
		}

		if len(inA) > 0 {
			wrote := false
			for w, fams := range named {
				if _, ok := inA[w]; ok {
					continue
				}
				if appendRelated(&d, snap, plannedRef{
					EgID: g.ID, WorkID: w, LinkKind: model.LinkKindRelated,
					MatchedBy: ruleTwin, Class: classTwin, Families: fams,
				}) {
					wrote = true
				}
			}
			if wrote {
				d.st.TwinGames++
			}
			continue
		}

		switch len(named) {
		case 0:
			d.st.NoEvidenceGames++
		case 1:
			d.st.CandidateGames++
			var w int64
			var fams []string
			for id, f := range named {
				w, fams = id, f
			}
			b := d.byWork[w]
			if b == nil {
				b = &candBucket{workID: w}
				d.byWork[w] = b
				d.workOrder = append(d.workOrder, w)
			}
			_, port := snap.transplant[g.ID]
			b.cands = append(b.cands, candidate{game: g, families: fams, transplant: port})
		default:
			d.st.TwinGames++
			for w, fams := range named {
				appendRelated(&d, snap, plannedRef{
					EgID: g.ID, WorkID: w, LinkKind: model.LinkKindRelated,
					MatchedBy: ruleTwin, Class: classTwin, Families: fams,
				})
			}
		}
	}
	return d
}

func finish(snap snapshot, d classDraft) ([]plannedRef, Stats) {
	planned := d.planned
	st := d.st

	for _, w := range d.workOrder {
		b := d.byWork[w]
		var live []candidate
		for _, c := range b.cands {
			if _, hit := snap.rejected[rejKey(w, c.game.ID)]; hit {
				st.RejectedSkips++
				continue
			}
			live = append(live, c)
		}
		if len(live) == 0 {
			continue
		}
		if snap.workHasPrimary[w] {
			// A work that already holds an EG ref at link kind 0 or 1 takes related
			// for every new candidate: production consumers were written when every
			// work had exactly one exact EG id (measured 2026-09-18), and a second
			// exact must not appear.
			for _, c := range live {
				planned = append(planned, plannedRef{
					EgID: c.game.ID, WorkID: w, LinkKind: model.LinkKindRelated,
					MatchedBy: matchedByFamilies(c.families), Class: classEdition, Families: c.families,
				})
				st.RelatedPlanned++
			}
			continue
		}
		primary := choosePrimary(live)
		for _, c := range live {
			p := plannedRef{
				EgID: c.game.ID, WorkID: w,
				MatchedBy: matchedByFamilies(c.families), Families: c.families,
			}
			if c.game.ID == primary.game.ID {
				p.Class = classPrimary
				if len(c.families) >= 2 {
					p.LinkKind = model.LinkKindExact
					st.ExactPlanned++
				} else if corr := corroborate(c.game, w, snap); corr != "" {
					// Of 3,280 EG games named by exactly one family (measured
					// 2026-09-18, read-only), 3,252 are corroborated by title or by
					// date; 28 by neither.
					p.LinkKind = model.LinkKindExact
					p.Corroboration = corr
					p.MatchedBy += "+" + corr
					st.ExactPlanned++
					st.Corroborated++
				} else {
					p.LinkKind = model.LinkKindProbable
					st.ProbablePlanned++
				}
			} else {
				p.Class = classEdition
				p.LinkKind = model.LinkKindRelated
				st.RelatedPlanned++
			}
			planned = append(planned, p)
		}
	}

	sort.Slice(planned, func(i, j int) bool {
		if planned[i].EgID != planned[j].EgID {
			return planned[i].EgID < planned[j].EgID
		}
		return planned[i].WorkID < planned[j].WorkID
	})
	return planned, st
}

func appendRelated(d *classDraft, snap snapshot, p plannedRef) bool {
	if _, hit := snap.rejected[rejKey(p.WorkID, p.EgID)]; hit {
		d.st.RejectedSkips++
		return false
	}
	d.planned = append(d.planned, p)
	d.st.RelatedPlanned++
	return true
}

func singleFamilyWorks(d classDraft) []int64 {
	var ids []int64
	for _, w := range d.workOrder {
		for _, c := range d.byWork[w].cands {
			if len(c.families) == 1 {
				ids = append(ids, w)
				break
			}
		}
	}
	return ids
}

func vndbFamily(egID int64, snap snapshot) (works []int64, multi bool) {
	vids := snap.vndbLinks[egID]
	seen := map[int64]struct{}{}
	for _, vid := range vids {
		w, ok := snap.vndbWork[vid]
		if !ok {
			continue
		}
		if _, dup := seen[w]; dup {
			continue
		}
		seen[w] = struct{}{}
		works = append(works, w)
	}
	sort.Slice(works, func(i, j int) bool { return works[i] < works[j] })
	return works, len(works) >= 2
}

func orderedFamilies(in []string) []string {
	have := map[string]struct{}{}
	for _, f := range in {
		have[f] = struct{}{}
	}
	out := make([]string, 0, len(have))
	for _, f := range familyOrder {
		if _, ok := have[f]; ok {
			out = append(out, f)
		}
	}
	return out
}

func matchedByFamilies(fams []string) string {
	return rulePref + strings.Join(orderedFamilies(fams), "+")
}

func choosePrimary(cands []candidate) candidate {
	ordered := append([]candidate(nil), cands...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].game.ID > ordered[j].game.ID })
	best := ordered[0]
	for _, c := range ordered[1:] {
		if primaryLess(c, best) {
			best = c
		}
	}
	return best
}

func primaryLess(a, b candidate) bool {
	if len(a.families) != len(b.families) {
		return len(a.families) > len(b.families)
	}
	if a.transplant != b.transplant {
		return !a.transplant
	}
	aPC, bPC := a.game.Model == "PC", b.game.Model == "PC"
	if aPC != bPC {
		return aPC
	}
	aEmpty, bEmpty := a.game.Sellday == "", b.game.Sellday == ""
	if aEmpty != bEmpty {
		return !aEmpty
	}
	if a.game.Sellday != b.game.Sellday {
		return a.game.Sellday < b.game.Sellday
	}
	return a.game.ID < b.game.ID
}
