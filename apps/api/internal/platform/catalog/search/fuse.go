package search

import "slices"

const DefaultRRFK = 60

type RankedList struct {
	IDs    []int64
	Weight float64
}

func FuseRRF(k int, lists ...RankedList) []int64 {
	score := map[int64]float64{}
	first := map[int64]int{}
	order := 0
	for _, list := range lists {
		for rank, id := range list.IDs {
			score[id] += list.Weight / float64(k+rank+1)
			if _, ok := first[id]; !ok {
				first[id] = order
				order++
			}
		}
	}
	out := make([]int64, 0, len(score))
	for id := range score {
		out = append(out, id)
	}
	slices.SortFunc(out, func(a, b int64) int {
		if score[a] != score[b] {
			if score[a] > score[b] {
				return -1
			}
			return 1
		}
		return first[a] - first[b]
	})
	return out
}
