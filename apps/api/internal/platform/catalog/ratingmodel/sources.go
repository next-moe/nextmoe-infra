package ratingmodel

import "slices"

type SourceSpec struct {
	Key      string
	ScaleMin float64
	ScaleMax float64
	// BucketWidth is the span one histogram key covers: ErogameScape's bars
	// are tokuten floored to tens, so key "70" stands for 70-79.
	BucketWidth  float64
	NativeMedian bool
}

var Sources = []SourceSpec{
	{Key: "vndb", ScaleMin: 1, ScaleMax: 10, BucketWidth: 1},
	{Key: "bangumi", ScaleMin: 1, ScaleMax: 10, BucketWidth: 1},
	{Key: "dlsite", ScaleMin: 1, ScaleMax: 5, BucketWidth: 1},
	{Key: "erogamescape", ScaleMin: 0, ScaleMax: 100, BucketWidth: 10, NativeMedian: true},
	{Key: "nextmoe", ScaleMin: 1, ScaleMax: 10, BucketWidth: 1},
	{Key: "howlongtobeat", ScaleMin: 0, ScaleMax: 100, BucketWidth: 1},
}

func SourceKeys() []string {
	out := make([]string, len(Sources))
	for i, s := range Sources {
		out[i] = s.Key
	}
	return out
}

func Spec(key string) (SourceSpec, bool) {
	i := slices.IndexFunc(Sources, func(s SourceSpec) bool { return s.Key == key })
	if i < 0 {
		return SourceSpec{}, false
	}
	return Sources[i], true
}

func (s SourceSpec) bucketValue(key float64) float64 {
	if s.BucketWidth <= 1 {
		return key
	}
	return min(key+(s.BucketWidth-1)/2, s.ScaleMax)
}

func (s SourceSpec) clamp(v float64) float64 {
	return max(s.ScaleMin, min(s.ScaleMax, v))
}
