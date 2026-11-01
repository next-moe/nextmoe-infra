package ratingmodel

import (
	"math"
	"slices"
)

type Consensus struct {
	Theta      float64
	SD         float64
	Score      float64
	Lower      float64
	Upper      float64
	Percentile float64
	Sources    int
	Votes      int
	Divergence *float64
}

type WorkScore struct {
	Sources   []SourceStat
	Consensus *Consensus
}

func (p *Params) Score(obs []Observation) WorkScore {
	var ws WorkScore
	for _, spec := range Sources {
		for _, o := range obs {
			if o.Source != spec.Key || o.Votes <= 0 {
				continue
			}
			sp := p.Sources[spec.Key]
			if sp == nil {
				continue
			}
			ws.Sources = append(ws.Sources, sp.posterior(spec, o))
		}
	}
	ws.Consensus = p.combine(ws.Sources)
	return ws
}

func (p *Params) Consensus(obs []Observation) *Consensus {
	return p.Score(obs).Consensus
}

func (p *Params) CombineSources(stats []SourceStat, keep []string) *Consensus {
	var sub []SourceStat
	for _, st := range stats {
		if slices.Contains(keep, st.Source) {
			sub = append(sub, st)
		}
	}
	return p.combine(sub)
}

func (p *Params) ThetaAtPercentile(pct float64) float64 {
	if len(p.ThetaQ) < 2 {
		return 0
	}
	return quantileAt(p.ThetaQ, max(0, min(1, pct/100)))
}

func (p *Params) combine(stats []SourceStat) *Consensus {
	num, den := 0.0, 0.0
	c := &Consensus{}
	for _, st := range stats {
		if !st.Cal {
			continue
		}
		num += st.Num
		den += st.Den
		c.Sources++
		c.Votes += st.Votes
	}
	if c.Sources == 0 {
		return nil
	}
	c.Theta, c.SD = Theta(num, den)
	c.Score = DeviationScore(c.Theta)
	c.Lower = DeviationScore(c.Theta - Z*c.SD)
	c.Upper = DeviationScore(c.Theta + Z*c.SD)
	c.Percentile = p.ThetaPercentile(c.Theta)
	c.Divergence = p.divergence(stats)
	return c
}

func (sp *SourceParams) DivergenceVotes() float64 { return reliableVotes(sp.PriorVotes) }

func Theta(num, den float64) (float64, float64) {
	prec := 1 + den
	return num / prec, 1 / math.Sqrt(prec)
}

func DeviationScore(theta float64) float64 { return max(0, min(100, 50+10*theta)) }

func (p *Params) ThetaPercentile(theta float64) float64 {
	if len(p.ThetaQ) < 2 {
		return 100 * normalCDF(theta)
	}
	return 100 * ecdf(p.ThetaQ, theta)
}

// divergence counts only sources whose estimate is mostly the work's own:
// with the prior worth half, nextmoe's three-vote ratings made the most
// "divisive" works of the catalog.
func (p *Params) divergence(stats []SourceStat) *float64 {
	lo, hi, n := 101.0, -1.0, 0
	for _, st := range stats {
		sp := p.Sources[st.Source]
		if sp == nil || float64(st.Votes) < sp.DivergenceVotes() {
			continue
		}
		lo, hi, n = math.Min(lo, st.Percentile), math.Max(hi, st.Percentile), n+1
	}
	if n < 2 {
		return nil
	}
	d := hi - lo
	return &d
}
