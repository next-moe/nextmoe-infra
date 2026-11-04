package ratingmodel

import (
	"math"
	"slices"
	"sort"
	"strconv"
)

const Version = 1

// Z is the one-sided 95% normal quantile: lower and upper bound a 90% interval.
const Z = 1.6448536269514722

type Observation struct {
	WorkID  int64
	Source  string
	Score   float64
	Votes   int
	Hist    map[string]int
	Average *float64
	Stdev   *float64
}

type SourceParams struct {
	Keys       []string  `json:"keys"`
	Values     []float64 `json:"values"`
	PriorVotes float64   `json:"prior_votes"`
	Within     float64   `json:"within"`
	Tau2       float64   `json:"tau2"`
	Knots      []float64 `json:"knots"`
	Beta       []float64 `json:"beta"`
	Works      int       `json:"works"`
	MeanQ      []float64 `json:"mean_quantiles"`
	GaussX     []float64 `json:"gauss_x"`
	GaussG     []float64 `json:"gauss_g"`

	Calibrated bool    `json:"calibrated"`
	A          float64 `json:"a"`
	B          float64 `json:"b"`
	Sigma2     float64 `json:"sigma2"`
	FitWorks   int     `json:"fit_works"`
}

type Params struct {
	Version   int                      `json:"version"`
	Sources   map[string]*SourceParams `json:"sources"`
	ThetaQ    []float64                `json:"theta_quantiles"`
	ThetaMean float64                  `json:"theta_mean"`
	ThetaSD   float64                  `json:"theta_sd"`
	FitWorks  int                      `json:"fit_works"`
}

type SourceStat struct {
	Source     string
	Native     float64
	Votes      int
	Mean       float64
	Lower      float64
	Upper      float64
	Median     *float64
	Spread     *float64
	Percentile float64

	Num float64
	Den float64
	Cal bool
}

type moments struct {
	n, mean, variance float64
	knownVar          bool
	median            *float64
	hist              []float64
	histTotal         float64
}

func canonicalKey(k string) (string, float64, bool) {
	v, err := strconv.ParseFloat(k, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return "", 0, false
	}
	return strconv.FormatFloat(v, 'f', -1, 64), v, true
}

func (p *SourceParams) keyIndex(k string) int {
	ck, _, ok := canonicalKey(k)
	if !ok {
		return -1
	}
	return slices.Index(p.Keys, ck)
}

func (p *SourceParams) momentsOf(spec SourceSpec, o Observation) moments {
	m := moments{n: float64(o.Votes)}
	if len(o.Hist) > 0 {
		m.hist = make([]float64, len(p.Keys))
		for k, c := range o.Hist {
			if i := p.keyIndex(k); i >= 0 && c > 0 {
				m.hist[i] += float64(c)
				m.histTotal += float64(c)
			}
		}
	}
	var hMean, hVar float64
	if m.histTotal > 0 {
		for i, c := range m.hist {
			hMean += p.Values[i] * c
		}
		hMean /= m.histTotal
		for i, c := range m.hist {
			d := p.Values[i] - hMean
			hVar += d * d * c
		}
		hVar = hVar/m.histTotal + (spec.BucketWidth*spec.BucketWidth-1)/12
	}
	switch {
	case o.Average != nil:
		m.mean = *o.Average
	case m.histTotal > 0:
		m.mean = hMean
	default:
		m.mean = o.Score
	}
	switch {
	case o.Stdev != nil:
		m.variance, m.knownVar = *o.Stdev**o.Stdev, true
	case m.histTotal >= 2:
		m.variance, m.knownVar = hVar, true
	default:
		m.variance = p.Within
	}
	switch {
	case spec.NativeMedian:
		med := o.Score
		m.median = &med
	case m.histTotal > 0:
		med := p.histMedian(spec, m.hist, m.histTotal)
		m.median = &med
	}
	return m
}

func (p *SourceParams) priorMean(spec SourceSpec, n float64) float64 {
	if len(p.Beta) == 0 {
		return 0
	}
	return spec.clamp(dot(p.Beta, splineBasis(p.Knots, n)))
}

func splineBasis(knots []float64, n float64) []float64 {
	l := math.Log1p(n)
	x := make([]float64, 2+len(knots))
	x[0], x[1] = 1, l
	for i, k := range knots {
		x[2+i] = max(l-k, 0)
	}
	return x
}

func dot(a, b []float64) float64 {
	s := 0.0
	for i := range a {
		s += a[i] * b[i]
	}
	return s
}

func (p *SourceParams) shrink(spec SourceSpec, m moments) (mean, sd, weight float64) {
	c := p.PriorVotes
	weight = m.n / (c + m.n)
	mean = (1-weight)*p.priorMean(spec, m.n) + weight*m.mean
	// The interval uses the pooled spread of a vote, not the work's own: the
	// Rance 35th Anniversary Box drew 12 near-unanimous 9.9s on Bangumi, its
	// own spread made the interval a hair wide, and its lower bound ranked
	// above most of the canon.
	return mean, math.Sqrt(p.Within / (c + m.n)), weight
}

func (p *SourceParams) posterior(spec SourceSpec, o Observation) SourceStat {
	m := p.momentsOf(spec, o)
	e, sd, w := p.shrink(spec, m)
	st := SourceStat{
		Source: spec.Key, Native: o.Score, Votes: o.Votes,
		Mean:  spec.clamp(e),
		Lower: spec.clamp(e - Z*sd),
		Upper: spec.clamp(e + Z*sd),
	}
	if m.median != nil {
		med := spec.clamp(e + w*(*m.median-m.mean))
		st.Median = &med
	}
	if o.Votes >= 2 && m.knownVar {
		s := math.Sqrt(m.variance)
		st.Spread = &s
	}
	if len(p.MeanQ) > 1 {
		st.Percentile = 100 * ecdf(p.MeanQ, e)
	}
	if p.Calibrated && len(p.GaussX) > 1 {
		g, dvar := p.normalScore(e, sd)
		v := p.Sigma2 + dvar
		st.Num = p.B * (g - p.A) / v
		st.Den = p.B * p.B / v
		st.Cal = true
	}
	return st
}

func (p *SourceParams) normalScore(e, sd float64) (float64, float64) {
	// The clamp is for the point only: clamping both ends of the interval
	// zeroed the measurement variance of every work past ±4σ, and a
	// single-source box set with 12 votes reached the consensus top 25.
	dg := (p.rawGauss(e+Z*sd) - p.rawGauss(e-Z*sd)) / (2 * Z)
	return p.gauss(e), dg * dg
}

func (p *SourceParams) histMedian(spec SourceSpec, hist []float64, total float64) float64 {
	cum := 0.0
	for i, c := range hist {
		if c > 0 && cum+c >= total/2 {
			lo, hi := p.bucketEdges(spec, i)
			return spec.clamp(lo + (total/2-cum)/c*(hi-lo))
		}
		cum += c
	}
	return p.Values[len(p.Values)-1]
}

func (p *SourceParams) bucketEdges(spec SourceSpec, i int) (float64, float64) {
	if spec.BucketWidth > 1 {
		key, _ := strconv.ParseFloat(p.Keys[i], 64)
		return key, min(key+spec.BucketWidth, spec.ScaleMax)
	}
	v := p.Values
	half := 0.5
	if len(v) > 1 {
		if i+1 < len(v) {
			half = (v[i+1] - v[i]) / 2
		} else {
			half = (v[i] - v[i-1]) / 2
		}
	}
	return spec.clamp(v[i] - half), spec.clamp(v[i] + half)
}

func (p *SourceParams) rawGauss(x float64) float64 { return interpExtrap(p.GaussX, p.GaussG, x) }

func (p *SourceParams) gauss(x float64) float64 {
	return max(-gaussClamp, min(gaussClamp, p.rawGauss(x)))
}

const gaussClamp = 4.0

func ecdf(q []float64, x float64) float64 {
	n := len(q)
	if x <= q[0] {
		return 0
	}
	if x >= q[n-1] {
		return 1
	}
	i := sort.SearchFloat64s(q, x)
	lo, hi := q[i-1], q[i]
	frac := 0.5
	if hi > lo {
		frac = (x - lo) / (hi - lo)
	}
	return (float64(i-1) + frac) / float64(n-1)
}

func interpExtrap(xs, ys []float64, x float64) float64 {
	n := len(xs)
	i := sort.SearchFloat64s(xs, x)
	switch {
	case i <= 0:
		i = 1
	case i >= n:
		i = n - 1
	}
	x0, x1, y0, y1 := xs[i-1], xs[i], ys[i-1], ys[i]
	if x1 <= x0 {
		return (y0 + y1) / 2
	}
	return y0 + (x-x0)/(x1-x0)*(y1-y0)
}

func quantiles(sorted []float64, n int) []float64 {
	out := make([]float64, n)
	for i := range n {
		out[i] = quantileAt(sorted, float64(i)/float64(n-1))
	}
	return out
}

func quantileAt(sorted []float64, p float64) float64 {
	pos := p * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	if lo >= len(sorted)-1 {
		return sorted[len(sorted)-1]
	}
	frac := pos - float64(lo)
	return sorted[lo] + frac*(sorted[lo+1]-sorted[lo])
}

func normalCDF(x float64) float64 { return 0.5 * math.Erfc(-x/math.Sqrt2) }
