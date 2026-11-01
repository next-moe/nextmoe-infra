package ratingmodel

import (
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

type synthWork struct {
	id    int64
	theta float64
	obs   []Observation
}

func synth(t *testing.T, n int, seed uint64) []synthWork {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, 7))
	type src struct {
		key            string
		lo, hi, center float64
		slope, noise   float64
		share          float64
	}
	srcs := []src{
		{"vndb", 1, 10, 6.5, 1.0, 1.6, 0.8},
		{"bangumi", 1, 10, 6.0, 1.1, 1.4, 0.7},
		{"erogamescape", 0, 100, 65, 9, 14, 0.6},
	}
	var out []synthWork
	for i := range n {
		w := synthWork{id: int64(i + 1), theta: rng.NormFloat64()}
		for _, s := range srcs {
			if rng.Float64() > s.share {
				continue
			}
			votes := int(math.Exp(rng.NormFloat64()*1.5 + 2.5 + 0.4*w.theta))
			votes = max(1, min(votes, 3000))
			mu := s.center + s.slope*w.theta
			hist := map[string]int{}
			for range votes {
				v := math.Round(mu + rng.NormFloat64()*s.noise)
				if s.key == "erogamescape" {
					v = math.Floor(max(s.lo, min(s.hi, v))/10) * 10
				} else {
					v = max(s.lo, min(s.hi, v))
				}
				hist[strconv.Itoa(int(v))]++
			}
			w.obs = append(w.obs, Observation{WorkID: w.id, Source: s.key, Votes: votes, Hist: hist, Score: mu})
		}
		if len(w.obs) > 0 {
			out = append(out, w)
		}
	}
	return out
}

func fitSynth(t *testing.T, works []synthWork) *Params {
	t.Helper()
	var obs []Observation
	for _, w := range works {
		obs = append(obs, w.obs...)
	}
	p, rep, err := Fit(obs)
	require.NoError(t, err)
	require.Equal(t, len(works), rep.Works)
	return p
}

func TestFitRecoversQuality(t *testing.T) {
	works := synth(t, 4000, 1)
	p := fitSynth(t, works)
	for _, key := range []string{"vndb", "bangumi", "erogamescape"} {
		sp := p.Sources[key]
		require.NotNil(t, sp, key)
		require.True(t, sp.Calibrated, key)
		require.Greater(t, sp.PriorVotes, 0.1, key)
		require.Less(t, sp.PriorVotes, 20.0, key)
		require.Greater(t, sp.B, 0.0, key)
	}
	var truth, est []float64
	for _, w := range works {
		c := p.Consensus(w.obs)
		require.NotNil(t, c)
		truth = append(truth, w.theta)
		est = append(est, c.Theta)
	}
	require.Greater(t, spearman(truth, est), 0.85)
}

func TestLowerBoundKeepsAFewPerfectVotesOffTheTop(t *testing.T) {
	works := synth(t, 3000, 2)
	p := fitSynth(t, works)
	lucky := []Observation{{WorkID: -1, Source: "bangumi", Votes: 2, Hist: map[string]int{"10": 2}, Score: 10}}
	luckyLower := p.Consensus(lucky).Lower
	var better int
	for _, w := range works {
		if p.Consensus(w.obs).Lower > luckyLower {
			better++
		}
	}
	require.Greater(t, better, len(works)/20, "two perfect votes ranked in the top 5%%")
	require.Greater(t, p.Consensus(lucky).Score, luckyLower, "the point estimate stays high; only the bound is cautious")
}

func TestMoreVotesNarrowTheInterval(t *testing.T) {
	works := synth(t, 2000, 3)
	p := fitSynth(t, works)
	sp, spec := p.Sources["vndb"], Sources[0]
	few := sp.posterior(spec, Observation{Source: "vndb", Votes: 3, Hist: map[string]int{"8": 3}})
	many := sp.posterior(spec, Observation{Source: "vndb", Votes: 300, Hist: map[string]int{"8": 300}})
	require.Greater(t, few.Upper-few.Lower, many.Upper-many.Lower)
	require.Less(t, few.Mean, many.Mean, "three 8s are pulled further toward the prior than three hundred")
	require.InDelta(t, 8, many.Mean, 0.1)
}

func TestCombineSourcesUsesOnlyTheNamedSources(t *testing.T) {
	works := synth(t, 2000, 4)
	p := fitSynth(t, works)
	for _, w := range works {
		if len(w.obs) < 2 {
			continue
		}
		ws := p.Score(w.obs)
		keep := w.obs[0].Source
		sub := p.CombineSources(ws.Sources, []string{keep})
		alone := p.Consensus(w.obs[:1])
		require.InDelta(t, alone.Theta, sub.Theta, 1e-12)
		require.Equal(t, 1, sub.Sources)
		return
	}
	t.Fatal("no multi-source work")
}

func TestIndexDocCarriesEveryRankField(t *testing.T) {
	works := synth(t, 1500, 5)
	p := fitSynth(t, works)
	for _, w := range works {
		if len(w.obs) < 3 {
			continue
		}
		doc := IndexDoc(p.Score(w.obs))
		for _, o := range w.obs {
			for _, f := range []string{FieldNative, FieldVotes, FieldMean, FieldLower, FieldUpper, FieldPercentile, FieldNum, FieldDen, FieldMedian} {
				require.Contains(t, doc[o.Source], f, o.Source)
			}
		}
		for _, f := range []string{FieldTheta, FieldLower, FieldUpper, FieldVotes, FieldSources} {
			require.Contains(t, doc[ConsensusKey], f)
		}
		c := doc[ConsensusKey]
		require.Less(t, c[FieldLower], c[FieldTheta])
		require.Greater(t, c[FieldUpper], c[FieldTheta])
		return
	}
	t.Fatal("no three-source work")
}

func TestHistMedianInterpolatesInsideTheBucket(t *testing.T) {
	sp := &SourceParams{Keys: []string{"6", "7", "8"}, Values: []float64{6, 7, 8}}
	spec, _ := Spec("bangumi")
	require.InDelta(t, 7.0, sp.histMedian(spec, []float64{2, 2, 2}, 6), 1e-9)
	require.InDelta(t, 7.75, sp.histMedian(spec, []float64{1, 1, 4}, 6), 1e-9)

	eg := &SourceParams{Keys: []string{"70", "80"}, Values: []float64{74.5, 84.5}}
	egSpec, _ := Spec("erogamescape")
	require.InDelta(t, 80.0, eg.histMedian(egSpec, []float64{1, 1}, 2), 1e-9)
}

func TestDeviationScoreAndPercentile(t *testing.T) {
	require.Equal(t, 50.0, DeviationScore(0))
	require.Equal(t, 70.0, DeviationScore(2))
	require.Equal(t, 100.0, DeviationScore(9))
	require.Equal(t, 0.0, DeviationScore(-9))

	p := &Params{ThetaQ: quantiles([]float64{-2, -1, 0, 1, 2}, 5)}
	require.InDelta(t, 50, p.ThetaPercentile(0), 1e-9)
	require.InDelta(t, 0, p.ThetaAtPercentile(50), 1e-9)
	require.Equal(t, 100.0, p.ThetaPercentile(5))
}

func TestFitIsDeterministic(t *testing.T) {
	works := synth(t, 800, 6)
	a, b := fitSynth(t, works), fitSynth(t, works)
	for key, sp := range a.Sources {
		require.Equal(t, sp.PriorVotes, b.Sources[key].PriorVotes, key)
		require.Equal(t, sp.A, b.Sources[key].A, key)
	}
	require.True(t, slices.Equal(a.ThetaQ, b.ThetaQ))
}
