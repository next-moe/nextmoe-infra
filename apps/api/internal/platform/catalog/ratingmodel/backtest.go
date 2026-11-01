package ratingmodel

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
)

const (
	truthMinVotes = 100
	topK          = 100
	sampleMaxN    = 60
)

func Backtest(obs []Observation, p *Params, seed int64) []string {
	rng := rand.New(rand.NewPCG(uint64(seed), 0x9e3779b97f4a7c15))
	out := []string{"", "stability (spearman / precision@100 against the full-data ranking)"}
	head := fmt.Sprintf("  %-13s %5s", "source", "works")
	for _, c := range stabilityGrid {
		head += fmt.Sprintf(" %9s", fmt.Sprintf("C=%g", c))
	}
	out = append(out, head)
	for _, spec := range Sources {
		sp := p.Sources[spec.Key]
		if sp == nil {
			continue
		}
		if line, ok := stability(spec, sp, obs, rng); ok {
			out = append(out, line)
		}
	}
	out = append(out, "", "agreement (spearman of the held-out source's percentile)")
	out = append(out, fmt.Sprintf("  %-14s %6s %15s %15s", "held_out", "works", "mean_percentile", "consensus"))
	out = append(out, agreement(obs, p)...)
	return out
}

func stability(spec SourceSpec, sp *SourceParams, obs []Observation, rng *rand.Rand) (string, bool) {
	var truth []float64
	type thin struct{ n, mean, variance float64 }
	var thinned []thin
	for _, o := range obs {
		if o.Source != spec.Key {
			continue
		}
		m := sp.momentsOf(spec, o)
		if m.histTotal < truthMinVotes {
			continue
		}
		truth = append(truth, histMean(sp, m.hist))
		n := int(math.Round(math.Exp(rng.Float64() * math.Log(sampleMaxN))))
		tm := sp.momentsOf(spec, Observation{Votes: n, Hist: sampleHist(sp, m.hist, m.histTotal, n, rng)})
		thinned = append(thinned, thin{float64(n), tm.mean, tm.variance})
	}
	if len(truth) < 2*topK {
		return "", false
	}
	target := 0.0
	for _, t := range thinned {
		target += t.mean
	}
	target /= float64(len(thinned))
	within := sp.Within
	rank := func(c float64, lower bool) []float64 {
		out := make([]float64, len(thinned))
		for i, t := range thinned {
			e := (c*target + t.n*t.mean) / (c + t.n)
			out[i] = e
			if lower {
				out[i] -= Z * math.Sqrt(within/(c+t.n))
			}
		}
		return out
	}
	cell := func(x []float64) string {
		return fmt.Sprintf("%.3f/%.2f", spearman(truth, x), precisionAtK(truth, x, topK))
	}
	line := fmt.Sprintf("  %-13s %5d", spec.Key, len(truth))
	for _, c := range stabilityGrid {
		line += " " + cell(rank(c, false))
	}
	eb := sp.PriorVotes
	line += fmt.Sprintf(" | eb C=%.2f %s lb %s", eb, cell(rank(eb, false)), cell(rank(eb, true)))
	return line, true
}

var stabilityGrid = []float64{0.5, 1, 2, 4, 8, 16, 32}

func histMean(sp *SourceParams, h []float64) float64 {
	s, t := 0.0, 0.0
	for i, c := range h {
		s += sp.Values[i] * c
		t += c
	}
	return s / t
}

func sampleHist(sp *SourceParams, hist []float64, total float64, n int, rng *rand.Rand) map[string]int {
	out := map[string]int{}
	for range n {
		u := rng.Float64() * total
		for i, c := range hist {
			if u < c {
				out[sp.Keys[i]]++
				break
			}
			u -= c
		}
	}
	return out
}

func agreement(obs []Observation, p *Params) []string {
	byWork := map[int64][]Observation{}
	for _, o := range obs {
		if o.Votes > 0 {
			byWork[o.WorkID] = append(byWork[o.WorkID], o)
		}
	}
	var lines []string
	for _, spec := range Sources {
		sp := p.Sources[spec.Key]
		if sp == nil || !sp.Calibrated {
			continue
		}
		var held, meanPct, cons []float64
		for _, w := range byWork {
			ws := p.Score(w)
			var target *SourceStat
			var rest []SourceStat
			for i, st := range ws.Sources {
				if !st.Cal || float64(st.Votes) < p.Sources[st.Source].PriorVotes {
					continue
				}
				if st.Source == spec.Key {
					target = &ws.Sources[i]
				} else {
					rest = append(rest, st)
				}
			}
			if target == nil || len(rest) < 2 {
				continue
			}
			sum, num, den := 0.0, 0.0, 0.0
			for _, st := range rest {
				sum += st.Percentile
				num += st.Num
				den += st.Den
			}
			th, _ := Theta(num, den)
			held = append(held, target.Percentile)
			meanPct = append(meanPct, sum/float64(len(rest)))
			cons = append(cons, th)
		}
		if len(held) < 50 {
			continue
		}
		lines = append(lines, fmt.Sprintf("  %-14s %6d %15.4f %15.4f", spec.Key, len(held), spearman(held, meanPct), spearman(held, cons)))
	}
	return lines
}

func ranks(x []float64) []float64 {
	idx := make([]int, len(x))
	for i := range idx {
		idx[i] = i
	}
	slices.SortFunc(idx, func(a, b int) int {
		switch {
		case x[a] < x[b]:
			return -1
		case x[a] > x[b]:
			return 1
		}
		return 0
	})
	r := make([]float64, len(x))
	for i := 0; i < len(idx); {
		j := i
		for j+1 < len(idx) && x[idx[j+1]] == x[idx[i]] {
			j++
		}
		avg := float64(i+j) / 2
		for k := i; k <= j; k++ {
			r[idx[k]] = avg
		}
		i = j + 1
	}
	return r
}

func spearman(a, b []float64) float64 {
	ra, rb := ranks(a), ranks(b)
	n := float64(len(ra))
	ma, mb := (n-1)/2, (n-1)/2
	var sab, saa, sbb float64
	for i := range ra {
		da, db := ra[i]-ma, rb[i]-mb
		sab += da * db
		saa += da * da
		sbb += db * db
	}
	return sab / math.Sqrt(saa*sbb)
}

func precisionAtK(truth, pred []float64, k int) float64 {
	top := func(x []float64) map[int]bool {
		idx := make([]int, len(x))
		for i := range idx {
			idx[i] = i
		}
		slices.SortStableFunc(idx, func(a, b int) int {
			switch {
			case x[a] > x[b]:
				return -1
			case x[a] < x[b]:
				return 1
			}
			return 0
		})
		out := map[int]bool{}
		for _, i := range idx[:k] {
			out[i] = true
		}
		return out
	}
	t, p := top(truth), top(pred)
	hit := 0
	for i := range p {
		if t[i] {
			hit++
		}
	}
	return float64(hit) / float64(k)
}

var _ = strings.Join
