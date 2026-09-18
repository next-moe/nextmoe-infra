package hltbattach

import "fmt"

func runHoldout(snap snapshot, opts Opts) (*Stats, error) {
	pop, truth := holdoutPopulation(snap)
	planned, st := decide(snap, pop)
	var wrong []plannedAction
	for _, p := range planned {
		ok := false
		for _, w := range truth[p.HltbID] {
			if w == p.WorkID {
				ok = true
				break
			}
		}
		if ok {
			st.HoldoutCorrect++
		} else {
			st.HoldoutWrong++
			wrong = append(wrong, p)
		}
	}
	st.HoldoutSkipped = st.Population - st.HoldoutCorrect - st.HoldoutWrong
	fmt.Printf("holdout n=%d attach_correct=%d attach_wrong=%d skipped=%d\n",
		st.Population, st.HoldoutCorrect, st.HoldoutWrong, st.HoldoutSkipped)
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
		for _, w := range snap.exactHoldings[g.ID] {
			if _, ok := snap.vndbWorks[w]; ok {
				works = append(works, w)
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
