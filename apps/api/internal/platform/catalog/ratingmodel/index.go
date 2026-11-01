package ratingmodel

const ConsensusKey = "consensus"

const (
	FieldNative     = "native"
	FieldVotes      = "votes"
	FieldMean       = "mean"
	FieldLower      = "lower"
	FieldUpper      = "upper"
	FieldMedian     = "median"
	FieldSpread     = "spread"
	FieldPercentile = "pct"
	FieldNum        = "num"
	FieldDen        = "den"

	FieldTheta      = "theta"
	FieldSources    = "sources"
	FieldDivergence = "divergence"
)

var SourceIndexFields = []string{
	FieldNative, FieldVotes, FieldMean, FieldLower, FieldUpper, FieldMedian, FieldSpread, FieldPercentile, FieldNum, FieldDen,
}

var ConsensusIndexFields = []string{FieldTheta, FieldLower, FieldUpper, FieldVotes, FieldSources, FieldDivergence}

func IndexDoc(ws WorkScore) map[string]map[string]float64 {
	if len(ws.Sources) == 0 {
		return nil
	}
	out := make(map[string]map[string]float64, len(ws.Sources)+1)
	for _, st := range ws.Sources {
		f := map[string]float64{
			FieldNative: st.Native, FieldVotes: float64(st.Votes),
			FieldMean: st.Mean, FieldLower: st.Lower, FieldUpper: st.Upper, FieldPercentile: st.Percentile,
		}
		if st.Median != nil {
			f[FieldMedian] = *st.Median
		}
		if st.Spread != nil {
			f[FieldSpread] = *st.Spread
		}
		if st.Cal {
			f[FieldNum], f[FieldDen] = st.Num, st.Den
		}
		out[st.Source] = f
	}
	if c := ws.Consensus; c != nil {
		f := map[string]float64{
			FieldTheta: c.Theta, FieldLower: c.Theta - Z*c.SD, FieldUpper: c.Theta + Z*c.SD,
			FieldVotes: float64(c.Votes), FieldSources: float64(c.Sources),
		}
		if c.Divergence != nil {
			f[FieldDivergence] = *c.Divergence
		}
		out[ConsensusKey] = f
	}
	return out
}
