package getchuattach

import (
	"context"
	"fmt"
	"sort"
)

type holdoutRule struct {
	Rule    string
	Correct int
	Wrong   int
}

func runHoldout(ctx context.Context, snap snapshot, opts Opts) (*Stats, error) {
	_ = ctx
	pop, truth := holdoutPopulation(snap)
	planned, st := decide(snap, pop)
	rules := map[string]*holdoutRule{}
	var wrong []plannedAction
	for _, p := range planned {
		ok := p.WorkID != 0 && p.WorkID == truth[p.GetchuID]
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
	st.HoldoutSkipped = st.Population - st.HoldoutCorrect - st.HoldoutWrong
	fmt.Printf("holdout n=%d attach_correct=%d attach_wrong=%d skipped=%d\n",
		st.Population, st.HoldoutCorrect, st.HoldoutWrong, st.HoldoutSkipped)
	for _, r := range st.HoldoutRules {
		fmt.Printf("holdout_rule rule=%s correct=%d wrong=%d\n", r.Rule, r.Correct, r.Wrong)
	}
	if err := writeReceipts(opts.Receipts, wrong); err != nil {
		return nil, err
	}
	logSummary(st)
	return &st, nil
}

func holdoutPopulation(snap snapshot) ([]item, map[string]int64) {
	var pop []item
	truth := map[string]int64{}
	for _, it := range snap.items {
		w, ok := snap.exactWork[it.GetchuID]
		if !ok || w == 0 {
			continue
		}
		if _, has := snap.vndbWorks[w]; !has {
			continue
		}
		pop = append(pop, it)
		truth[it.GetchuID] = w
	}
	return pop, truth
}
