package ratingmodel

import (
	"fmt"
	"math"
	"slices"
)

const (
	meanQuantiles = 1001
	gaussStep     = 0.05
	gaussLimit    = 3.5
	minFitOverlap = 100
	emIterations  = 500
	emTolerance   = 1e-7
	// EM drove ErogameScape's residual variance to zero (a Heywood case) and
	// the consensus became ErogameScape alone; the held-out agreement backtest
	// was best across all six sources with the floor at 0.1.
	minSigma2    = 0.1
	priorRounds  = 50
	minKnotWorks = 50
)

var knotVotes = []float64{3, 10, 30, 100, 300, 1000, 3000}

type SourceReport struct {
	Source        string
	Works         int
	PriorVotes    float64
	Within        float64
	Tau2          float64
	PriorMeanAt   map[string]float64
	ReliableVotes float64
	ReliableWorks int
	Calibrated    bool
	A, B, Sigma2  float64
	FitWorks      int
}

type Report struct {
	Sources      []SourceReport
	Works        int
	FitWorks     int
	EMIterations int
}

func Fit(obs []Observation) (*Params, Report, error) {
	bySource := map[string][]Observation{}
	for _, o := range obs {
		if o.Votes > 0 {
			bySource[o.Source] = append(bySource[o.Source], o)
		}
	}
	params := &Params{Version: Version, Sources: map[string]*SourceParams{}}
	var rep Report
	for _, spec := range Sources {
		rows := bySource[spec.Key]
		if len(rows) == 0 {
			continue
		}
		sp, sr, err := fitSource(spec, rows)
		if err != nil {
			return nil, Report{}, fmt.Errorf("%s: %w", spec.Key, err)
		}
		params.Sources[spec.Key] = sp
		rep.Sources = append(rep.Sources, sr)
	}
	works := groupWorks(bySource)
	rep.Works = len(works)
	rep.EMIterations, rep.FitWorks = fitFactor(params, works)
	params.FitWorks = rep.FitWorks
	for i := range rep.Sources {
		sp := params.Sources[rep.Sources[i].Source]
		rep.Sources[i].Calibrated, rep.Sources[i].A, rep.Sources[i].B = sp.Calibrated, sp.A, sp.B
		rep.Sources[i].Sigma2, rep.Sources[i].FitWorks = sp.Sigma2, sp.FitWorks
	}

	thetas := make([]float64, 0, len(works))
	for _, w := range works {
		if c := params.Consensus(w); c != nil {
			thetas = append(thetas, c.Theta)
		}
	}
	if len(thetas) > 1 {
		slices.Sort(thetas)
		params.ThetaQ = quantiles(thetas, meanQuantiles)
	}
	return params, rep, nil
}

func groupWorks(bySource map[string][]Observation) [][]Observation {
	idx := map[int64]int{}
	var out [][]Observation
	for _, spec := range Sources {
		for _, o := range bySource[spec.Key] {
			i, ok := idx[o.WorkID]
			if !ok {
				i = len(out)
				idx[o.WorkID] = i
				out = append(out, nil)
			}
			out[i] = append(out[i], o)
		}
	}
	return out
}

func fitSource(spec SourceSpec, rows []Observation) (*SourceParams, SourceReport, error) {
	sp := &SourceParams{Works: len(rows)}
	keySet := map[string]float64{}
	for _, o := range rows {
		for k, c := range o.Hist {
			if ck, v, ok := canonicalKey(k); ok && c > 0 {
				keySet[ck] = v
			}
		}
	}
	for k := range keySet {
		sp.Keys = append(sp.Keys, k)
	}
	slices.SortFunc(sp.Keys, func(a, b string) int {
		if keySet[a] < keySet[b] {
			return -1
		}
		return 1
	})
	for _, k := range sp.Keys {
		sp.Values = append(sp.Values, spec.bucketValue(keySet[k]))
	}
	ms := make([]moments, len(rows))
	for i, o := range rows {
		ms[i] = sp.momentsOf(spec, o)
	}
	if err := sp.fitPrior(spec, ms); err != nil {
		return nil, SourceReport{}, err
	}

	means := make([]float64, 0, len(rows))
	var reliable []float64
	rv := reliableVotes(sp.PriorVotes)
	for _, m := range ms {
		e, _, _ := sp.shrink(spec, m)
		means = append(means, e)
		if m.n >= rv {
			reliable = append(reliable, e)
		}
	}
	slices.Sort(means)
	sp.MeanQ = quantiles(means, meanQuantiles)
	if len(reliable) < minFitOverlap {
		reliable = means
	}
	slices.Sort(reliable)
	for g := -gaussLimit; g <= gaussLimit+1e-9; g += gaussStep {
		sp.GaussX = append(sp.GaussX, quantileAt(reliable, normalCDF(g)))
		sp.GaussG = append(sp.GaussG, g)
	}
	at := map[string]float64{}
	for _, n := range []float64{1, 10, 100, 1000} {
		at[fmt.Sprint(n)] = sp.priorMean(spec, n)
	}
	return sp, SourceReport{
		Source: spec.Key, Works: len(rows), PriorVotes: sp.PriorVotes, Within: sp.Within, Tau2: sp.Tau2,
		PriorMeanAt: at, ReliableVotes: rv, ReliableWorks: len(reliable),
	}, nil
}

func reliableVotes(c float64) float64 { return math.Max(4*c, 2) }

func (sp *SourceParams) fitPrior(spec SourceSpec, ms []moments) error {
	var within []float64
	for _, m := range ms {
		if m.knownVar && m.n >= 2 {
			within = append(within, m.variance)
		}
	}
	if len(within) == 0 {
		return fmt.Errorf("no work publishes a vote spread")
	}
	sp.Within, _ = meanVar(within)
	for _, k := range knotVotes {
		above, below := 0, 0
		for _, m := range ms {
			if m.n > k {
				above++
			} else {
				below++
			}
		}
		if above >= minKnotWorks && below >= minKnotWorks {
			sp.Knots = append(sp.Knots, math.Log1p(k))
		}
	}
	noise := func(m moments) float64 {
		if m.knownVar {
			return m.variance / m.n
		}
		return sp.Within / m.n
	}
	_, total := meanVar(func() []float64 {
		out := make([]float64, len(ms))
		for i, m := range ms {
			out[i] = m.mean
		}
		return out
	}())
	sp.Tau2 = total / 2
	floor := 1e-6 * (spec.ScaleMax - spec.ScaleMin) * (spec.ScaleMax - spec.ScaleMin)
	for range priorRounds {
		k := 2 + len(sp.Knots)
		xtx := make([][]float64, k)
		for i := range xtx {
			xtx[i] = make([]float64, k)
		}
		xty := make([]float64, k)
		for _, m := range ms {
			u := 1 / (sp.Tau2 + noise(m))
			x := splineBasis(sp.Knots, m.n)
			for i := range k {
				xty[i] += u * x[i] * m.mean
				for j := range k {
					xtx[i][j] += u * x[i] * x[j]
				}
			}
		}
		beta, ok := solve(xtx, xty)
		if !ok {
			return fmt.Errorf("prior-mean spline is singular")
		}
		sp.Beta = beta
		num, den := 0.0, 0.0
		for _, m := range ms {
			u := 1 / (sp.Tau2 + noise(m))
			r := m.mean - dot(beta, splineBasis(sp.Knots, m.n))
			num += u * (r*r - noise(m))
			den += u
		}
		next := max(num/den, floor)
		done := math.Abs(next-sp.Tau2) < 1e-9*max(next, floor)
		sp.Tau2 = next
		if done {
			break
		}
	}
	sp.PriorVotes = sp.Within / sp.Tau2
	return nil
}

func solve(a [][]float64, b []float64) ([]float64, bool) {
	n := len(b)
	m := make([][]float64, n)
	for i := range m {
		m[i] = append(append([]float64{}, a[i]...), b[i])
	}
	for c := range n {
		p := c
		for r := c + 1; r < n; r++ {
			if math.Abs(m[r][c]) > math.Abs(m[p][c]) {
				p = r
			}
		}
		if math.Abs(m[p][c]) < 1e-12 {
			return nil, false
		}
		m[c], m[p] = m[p], m[c]
		for r := c + 1; r < n; r++ {
			f := m[r][c] / m[c][c]
			for j := c; j <= n; j++ {
				m[r][j] -= f * m[c][j]
			}
		}
	}
	x := make([]float64, n)
	for r := n - 1; r >= 0; r-- {
		s := m[r][n]
		for j := r + 1; j < n; j++ {
			s -= m[r][j] * x[j]
		}
		x[r] = s / m[r][r]
	}
	return x, true
}

func meanVar(x []float64) (float64, float64) {
	if len(x) == 0 {
		return 0, 0
	}
	m := 0.0
	for _, v := range x {
		m += v
	}
	m /= float64(len(x))
	v := 0.0
	for _, y := range x {
		v += (y - m) * (y - m)
	}
	return m, v / float64(len(x))
}

type fitPoint struct {
	source string
	g      float64
	dvar   float64
}

func fitFactor(params *Params, works [][]Observation) (int, int) {
	var points [][]fitPoint
	overlap := map[string]int{}
	for _, w := range works {
		var pts []fitPoint
		for _, o := range w {
			sp := params.Sources[o.Source]
			if sp == nil {
				continue
			}
			spec, _ := Spec(o.Source)
			e, sd, _ := sp.shrink(spec, sp.momentsOf(spec, o))
			g, dvar := sp.normalScore(e, sd)
			pts = append(pts, fitPoint{source: o.Source, g: g, dvar: dvar})
		}
		if len(pts) >= 2 {
			points = append(points, pts)
			for _, p := range pts {
				overlap[p.source]++
			}
		}
	}
	for key, sp := range params.Sources {
		if overlap[key] >= minFitOverlap {
			sp.Calibrated, sp.A, sp.B, sp.Sigma2, sp.FitWorks = true, 0, 1, 0.3, overlap[key]
		}
	}
	type acc struct{ su, sut, sutt, sug, sugt, res float64 }
	iter := 0
	for iter = 1; iter <= emIterations; iter++ {
		accs := map[string]*acc{}
		for _, pts := range points {
			prec, num := 1.0, 0.0
			for _, p := range pts {
				sp := params.Sources[p.source]
				if !sp.Calibrated {
					continue
				}
				v := sp.Sigma2 + p.dvar
				prec += sp.B * sp.B / v
				num += sp.B * (p.g - sp.A) / v
			}
			th, tv := num/prec, 1/prec
			for _, p := range pts {
				sp := params.Sources[p.source]
				if !sp.Calibrated {
					continue
				}
				a := accs[p.source]
				if a == nil {
					a = &acc{}
					accs[p.source] = a
				}
				u := 1 / (sp.Sigma2 + p.dvar)
				a.su += u
				a.sut += u * th
				a.sutt += u * (th*th + tv)
				a.sug += u * p.g
				a.sugt += u * p.g * th
			}
		}
		change := 0.0
		for key, a := range accs {
			sp := params.Sources[key]
			det := a.su*a.sutt - a.sut*a.sut
			if det <= 0 {
				continue
			}
			na := (a.sug*a.sutt - a.sut*a.sugt) / det
			nb := (a.su*a.sugt - a.sut*a.sug) / det
			change = max(change, math.Abs(na-sp.A), math.Abs(nb-sp.B))
			sp.A, sp.B = na, nb
		}
		for _, pts := range points {
			prec, num := 1.0, 0.0
			for _, p := range pts {
				sp := params.Sources[p.source]
				if !sp.Calibrated {
					continue
				}
				v := sp.Sigma2 + p.dvar
				prec += sp.B * sp.B / v
				num += sp.B * (p.g - sp.A) / v
			}
			th, tv := num/prec, 1/prec
			for _, p := range pts {
				sp := params.Sources[p.source]
				if !sp.Calibrated {
					continue
				}
				r := p.g - sp.A - sp.B*th
				accs[p.source].res += r*r + sp.B*sp.B*tv - p.dvar
			}
		}
		for key, a := range accs {
			sp := params.Sources[key]
			ns := max(a.res/float64(sp.FitWorks), minSigma2)
			change = max(change, math.Abs(ns-sp.Sigma2))
			sp.Sigma2 = ns
		}
		if change < emTolerance {
			break
		}
	}
	return iter, len(points)
}
