package main

import (
	"testing"
	"time"
)

func day(t *testing.T, s string) *time.Time {
	t.Helper()
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return &parsed
}

func TestClassifyPairBuckets(t *testing.T) {
	base := pairRow{A: 1, B: 2, LaneA: "kungal", LaneB: "bgm", SharedNames: 1, SharedNorms: 1, SharedOfficial: 1}
	keyOnly := func(r pairRow) pairRow {
		r.SharedNames, r.SharedNorms, r.SharedOfficial = 0, 0, 0
		return r
	}
	cases := []struct {
		name string
		row  func(pairRow) pairRow
		want bucket
	}{
		{"bare", func(r pairRow) pairRow { return r }, bucketBare},
		{"shared registry id alone", func(r pairRow) pairRow { r.SharedKey, r.SharedRegistry = "bangumi:1", true; return r }, bucketAuto},
		{"shared store id and the official title", func(r pairRow) pairRow { r.SharedKey = "steam:3919400"; return r }, bucketAuto},
		{"label overlap alone", func(r pairRow) pairRow { r.LabelOverlap = true; return r }, bucketAuto},
		{"two shared norms alone", func(r pairRow) pairRow { r.SharedNames, r.SharedNorms = 2, 2; return r }, bucketAuto},
		{"near dates alone", func(r pairRow) pairRow {
			r.DateA, r.DateB = day(t, "2015-08-10"), day(t, "2015-09-25")
			return r
		}, bucketAuto},
		{"far dates in one year are not corroboration", func(r pairRow) pairRow {
			r.DateA, r.DateB = day(t, "2015-01-10"), day(t, "2015-12-25")
			return r
		}, bucketBare},
		{"year mismatch vetoes the label corroborator", func(r pairRow) pairRow {
			r.LabelOverlap = true
			r.DateA, r.DateB = day(t, "2015-08-10"), day(t, "2019-08-10")
			return r
		}, bucketDateClash},
		{"a vndb conflict beats every corroborator", func(r pairRow) pairRow {
			r.VetoConflict, r.AnchorConflict, r.SharedKey, r.SharedRegistry, r.LabelOverlap = true, true, "bangumi:1", true, true
			return r
		}, bucketVetoConflict},
		{"another source's conflict stops a title-only merge", func(r pairRow) pairRow {
			r.AnchorConflict, r.LabelOverlap, r.SharedNames, r.SharedNorms = true, true, 2, 2
			return r
		}, bucketAnchorConflict},
		{"a shared registry id outranks another source's conflict", func(r pairRow) pairRow {
			r.AnchorConflict, r.SharedKey, r.SharedRegistry = true, "bangumi:1", true
			return r
		}, bucketAuto},
		{"a shared store id and the official title outrank another source's conflict", func(r pairRow) pairRow {
			r.AnchorConflict, r.SharedKey = true, "steam:1552140"
			return r
		}, bucketAuto},
		{"release conflict beats title corroborators", func(r pairRow) pairRow {
			r.RelConflict, r.LabelOverlap = true, true
			return r
		}, bucketRelConflict},
		{"a shared id outranks a release conflict", func(r pairRow) pairRow {
			r.RelConflict, r.SharedKey = true, "dmm:abc_0001"
			return r
		}, bucketAuto},
		{"two claimed works merge like any other pair", func(r pairRow) pairRow {
			r.LaneB, r.ClaimA, r.ClaimB, r.SharedKey = "kungal", 3, 1, "steam:3919400"
			return r
		}, bucketAuto},
		{"a hidden claim waits for a person", func(r pairRow) pairRow {
			r.HiddenA, r.SharedKey, r.SharedRegistry = true, "bangumi:1", true
			return r
		}, bucketHiddenClaim},
		{"a vndb conflict still outranks a hidden claim", func(r pairRow) pairRow {
			r.HiddenA, r.VetoConflict = true, true
			return r
		}, bucketVetoConflict},
		{"dlsite edition splits are out of scope", func(r pairRow) pairRow {
			r.LaneA, r.LaneB, r.AnchorConflict = "dlsite", "dlsite", true
			return r
		}, bucketOutOfScope},
		{"a registry id outranks a year clash", func(r pairRow) pairRow {
			r.SharedKey, r.SharedRegistry = "bangumi:1", true
			r.DateA, r.DateB = day(t, "2024-01-01"), day(t, "2026-01-01")
			return r
		}, bucketAuto},
		{"a registry id with no shared name is auto", func(r pairRow) pairRow {
			r = keyOnly(r)
			r.SharedKey, r.SharedRegistry = "bangumi:1", true
			return r
		}, bucketAuto},
		{"a store id with no shared name is judged: fan discs and packs share them", func(r pairRow) pairRow {
			r = keyOnly(r)
			r.SharedKey = "dmm:abc_0001pack"
			return r
		}, bucketSharedKey},
		{"a related-only id pair is judged", func(r pairRow) pairRow {
			r = keyOnly(r)
			r.RelatedOnly = true
			return r
		}, bucketSharedKey},
		{"alias-only title with a corroborator", func(r pairRow) pairRow {
			r.SharedOfficial = 0
			r.LabelOverlap = true
			return r
		}, bucketAliasOnly},
		{"a registry id outranks alias-only", func(r pairRow) pairRow {
			r.SharedOfficial = 0
			r.SharedKey, r.SharedRegistry = "erogamescape:1", true
			return r
		}, bucketAuto},
		{"a short official title alone is judged, never merged on the name", func(r pairRow) pairRow {
			r.SharedNorms = 0
			r.LabelOverlap = true
			return r
		}, bucketShortTitle},
		{"a short official title and a shared store id merge", func(r pairRow) pairRow {
			r.SharedNorms = 0
			r.SharedKey = "steam:3919400"
			return r
		}, bucketAuto},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyPair(tc.row(base)); got != tc.want {
				t.Fatalf("classifyPair = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyMergesAComponentOfClaimsIntoTheStrongest(t *testing.T) {
	rows := []pairRow{
		{A: 1, B: 2, ClaimA: 1, ClaimB: 0, SharedKey: "bangumi:1", SharedRegistry: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
		{A: 2, B: 3, ClaimA: 0, ClaimB: 3, SharedKey: "bangumi:1", SharedRegistry: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
	}
	verdicts, groups := classify(rows)
	if verdicts[0] != bucketAuto || verdicts[1] != bucketAuto {
		t.Fatalf("verdicts: %v", verdicts)
	}
	if len(groups) != 1 || groups[0].survivor != 3 {
		t.Fatalf("the published claim must survive: %+v", groups)
	}
}

func TestClassifySurvivorSelection(t *testing.T) {
	cases := []struct {
		name string
		row  pairRow
		want int64
	}{
		{
			name: "claimed work beats anchor count",
			row: pairRow{A: 10, B: 11, LaneA: "bgm", LaneB: "kungal", ClaimB: 3,
				AnchorsA: 5, AnchorsB: 0, LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
			want: 11,
		},
		{
			name: "a published claim beats a draft",
			row: pairRow{A: 12, B: 13, LaneA: "kungal", LaneB: "kungal", ClaimA: 1, ClaimB: 3,
				AnchorsA: 5, AnchorsB: 0, LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
			want: 13,
		},
		{
			name: "anchors decide when neither is claimed",
			row: pairRow{A: 20, B: 21, LaneA: "bgm", LaneB: "vndb",
				AnchorsA: 1, AnchorsB: 3, LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
			want: 21,
		},
		{
			name: "an anchor tie keeps the lower id",
			row: pairRow{A: 30, B: 31, LaneA: "bgm", LaneB: "vndb",
				AnchorsA: 1, AnchorsB: 1, LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
			want: 30,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, groups := classify([]pairRow{tc.row})
			if len(groups) != 1 {
				t.Fatalf("groups: %+v", groups)
			}
			if groups[0].survivor != tc.want {
				t.Fatalf("survivor = %d, want %d", groups[0].survivor, tc.want)
			}
			if len(groups[0].sources) != 1 || groups[0].sources[0] == tc.want {
				t.Fatalf("sources: %+v", groups[0].sources)
			}
		})
	}
}

func TestPairEvidenceNamesTheCorroborators(t *testing.T) {
	got := pairEvidence(pairRow{
		SharedKey: "steam:3919400", SharedNorms: 1,
		DateA: day(t, "2015-08-10"), DateB: day(t, "2015-09-25"),
	})
	if want := "key=steam:3919400+date(2015-08-10~2015-09-25)"; got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
	if got := pairEvidence(pairRow{LabelOverlap: true, SharedNorms: 2}); got != "label+norms=2" {
		t.Fatalf("evidence = %q, want %q", got, "label+norms=2")
	}
	if got := pairEvidence(pairRow{SharedNorms: 1}); got != "none" {
		t.Fatalf("evidence = %q, want %q", got, "none")
	}
}

func TestClassifyNeverElectsAHiddenSurvivor(t *testing.T) {
	rows := []pairRow{
		{A: 1, B: 2, LaneA: "kungal", LaneB: "bgm", ClaimA: 3, HiddenA: true, AnchorsB: 3, LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
		{A: 2, B: 3, LaneA: "bgm", LaneB: "vndb", LabelOverlap: true, SharedNames: 1, SharedNorms: 1, SharedOfficial: 1},
	}
	verdicts, groups := classify(rows)
	if verdicts[0] != bucketHiddenClaim {
		t.Fatalf("hidden pair: %v", verdicts)
	}
	if len(groups) != 1 || groups[0].survivor == 1 || len(groups[0].sources) != 1 || groups[0].sources[0] == 1 {
		t.Fatalf("the hidden work must stay out of every merge group: %+v", groups)
	}

	rows[0].HiddenA = false
	_, groups = classify(rows)
	if len(groups) != 1 || groups[0].survivor != 1 {
		t.Fatalf("without the hide the claimed work survives the same component: %+v", groups)
	}
}
