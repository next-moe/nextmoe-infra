package egworks

import (
	"context"
	"fmt"
	"sort"

	"api/internal/platform/catalog/model"
)

type holdoutRule struct {
	Rule    string
	Correct int
	Wrong   int
}

func runHoldout(ctx context.Context, snap snapshot, opts Opts) (*Stats, error) {
	_ = ctx
	pop, truth := holdoutPopulation(snap)
	stripped := stripHoldoutHoldings(snap, pop)
	planned, st := decide(stripped, pop, 0)
	st.HoldoutQuarantined = st.Quarantined
	st.HoldoutMinted = st.MintedLive
	rules := map[string]*holdoutRule{}
	var wrong []plannedAction
	for _, p := range planned {
		if p.Action != actionAttach || p.MatchedBy == rulePack || p.MatchedBy == ruleTransplant {
			continue
		}
		ok := false
		for _, w := range truth[p.EgID] {
			if w == p.WorkID {
				ok = true
				break
			}
		}
		r := rules[p.MatchedBy]
		if r == nil {
			r = &holdoutRule{Rule: p.MatchedBy}
			rules[p.MatchedBy] = r
		}
		if ok {
			st.HoldoutCorrect++
			r.Correct++
		} else {
			st.HoldoutWrong++
			r.Wrong++
			wrong = append(wrong, p)
		}
	}
	names := make([]string, 0, len(rules))
	for k := range rules {
		names = append(names, k)
	}
	sort.Strings(names)
	st.HoldoutRules = make([]holdoutRule, 0, len(names))
	for _, k := range names {
		st.HoldoutRules = append(st.HoldoutRules, *rules[k])
	}
	fmt.Printf("holdout n=%d attach_correct=%d attach_wrong=%d quarantined=%d minted=%d\n",
		st.Population, st.HoldoutCorrect, st.HoldoutWrong, st.HoldoutQuarantined, st.HoldoutMinted)
	for _, r := range st.HoldoutRules {
		fmt.Printf("holdout_rule rule=%s correct=%d wrong=%d\n", r.Rule, r.Correct, r.Wrong)
	}
	if err := writeReceipts(opts.Receipts, wrong); err != nil {
		return nil, err
	}
	logSummary(st)
	return &st, nil
}

func holdoutPopulation(snap snapshot) ([]game, map[int64][]int64) {
	var pop []game
	truth := map[int64][]int64{}
	for _, g := range snap.games {
		var works []int64
		for _, h := range snap.holdings[g.ID] {
			if h.LinkKind != model.LinkKindExact {
				continue
			}
			if _, ok := snap.vndbWorks[h.WorkID]; ok {
				works = append(works, h.WorkID)
			}
		}
		if len(works) == 0 {
			continue
		}
		pop = append(pop, g)
		truth[g.ID] = works
	}
	return pop, truth
}

func stripHoldoutHoldings(snap snapshot, pop []game) snapshot {
	drop := map[int64]struct{}{}
	for _, g := range pop {
		drop[g.ID] = struct{}{}
	}
	out := snap
	out.holdings = map[int64][]holding{}
	out.workHasPrimary = map[int64]bool{}
	out.holders = map[int64][]int64{}
	for egID, holds := range snap.holdings {
		if _, skip := drop[egID]; skip {
			continue
		}
		out.holdings[egID] = holds
		for _, h := range holds {
			if h.LinkKind == model.LinkKindExact || h.LinkKind == model.LinkKindProbable {
				out.workHasPrimary[h.WorkID] = true
				out.holders[egID] = append(out.holders[egID], h.WorkID)
			}
		}
	}
	return out
}
