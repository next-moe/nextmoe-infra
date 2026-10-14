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
	base := pairRow{A: 1, B: 2, LaneA: "kungal", LaneB: "bgm", SharedNorms: 1, SharedOfficial: 1}
	cases := []struct {
		name string
		row  func(pairRow) pairRow
		want bucket
	}{
		{"bare", func(r pairRow) pairRow { return r }, bucketBare},
		{"shared ref alone", func(r pairRow) pairRow { r.RefOverlap = true; return r }, bucketAuto},
		{"label overlap alone", func(r pairRow) pairRow { r.LabelOverlap = true; return r }, bucketAuto},
		{"two shared norms alone", func(r pairRow) pairRow { r.SharedNorms = 2; return r }, bucketAuto},
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
		{"anchor conflict beats every corroborator", func(r pairRow) pairRow {
			r.AnchorConflict, r.RefOverlap, r.LabelOverlap = true, true, true
			return r
		}, bucketAnchorConflict},
		{"release conflict beats every corroborator", func(r pairRow) pairRow {
			r.RelConflict, r.RefOverlap, r.LabelOverlap = true, true, true
			return r
		}, bucketRelConflict},
		{"two claimed works are never machine-merged", func(r pairRow) pairRow {
			r.LaneB, r.RefOverlap = "kungal", true
			return r
		}, bucketBothKungal},
		{"a hidden claim facing an unclaimed work waits for a person", func(r pairRow) pairRow {
			r.HiddenA, r.RefOverlap = true, true
			return r
		}, bucketHiddenClaim},
		{"a hidden claim facing another claim stays both-kungal", func(r pairRow) pairRow {
			r.LaneB, r.HiddenA, r.RefOverlap = "kungal", true, true
			return r
		}, bucketBothKungal},
		{"a conflict still outranks a hidden claim", func(r pairRow) pairRow {
			r.HiddenA, r.AnchorConflict = true, true
			return r
		}, bucketAnchorConflict},
		{"dlsite edition splits are out of scope", func(r pairRow) pairRow {
			r.LaneA, r.LaneB, r.AnchorConflict = "dlsite", "dlsite", true
			return r
		}, bucketOutOfScope},
		{"exact ref outranks a year clash", func(r pairRow) pairRow {
			r.RefOverlap = true
			r.DateA, r.DateB = day(t, "2024-01-01"), day(t, "2026-01-01")
			return r
		}, bucketAuto},
		{"exact ref with no shared norms is auto", func(r pairRow) pairRow {
			r.RefOverlap = true
			r.SharedNorms, r.SharedOfficial = 0, 0
			return r
		}, bucketAuto},
		{"case-insensitive ref only is never auto", func(r pairRow) pairRow {
			r.RefOverlapCI = true
			r.SharedNorms, r.SharedOfficial = 0, 0
			return r
		}, bucketRefCI},
		{"alias-only title with a corroborator", func(r pairRow) pairRow {
			r.SharedOfficial = 0
			r.LabelOverlap = true
			return r
		}, bucketAliasOnly},
		{"exact ref outranks alias-only", func(r pairRow) pairRow {
			r.SharedOfficial = 0
			r.RefOverlap = true
			return r
		}, bucketAuto},
		{"ci ref corroborates an official title pair", func(r pairRow) pairRow {
			r.RefOverlapCI = true
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

func TestClassifyDemotesBridgedComponents(t *testing.T) {
	rows := []pairRow{
		{A: 1, B: 2, LaneA: "kungal", LaneB: "bgm", RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
		{A: 2, B: 3, LaneA: "bgm", LaneB: "kungal", RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
		{A: 10, B: 11, LaneA: "bgm", LaneB: "vndb", RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
	}
	verdicts, groups := classify(rows)
	if verdicts[0] != bucketBridged || verdicts[1] != bucketBridged {
		t.Fatalf("a component holding two claimed works must be demoted: %v", verdicts)
	}
	if verdicts[2] != bucketAuto {
		t.Fatalf("the untouched component must stay auto: %v", verdicts)
	}
	if len(groups) != 1 || groups[0].survivor != 10 {
		t.Fatalf("groups: %+v", groups)
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
			row: pairRow{A: 10, B: 11, LaneA: "bgm", LaneB: "kungal",
				AnchorsA: 5, AnchorsB: 0, RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
			want: 11,
		},
		{
			name: "anchors decide when neither is claimed",
			row: pairRow{A: 20, B: 21, LaneA: "bgm", LaneB: "vndb",
				AnchorsA: 1, AnchorsB: 3, RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
			want: 21,
		},
		{
			name: "an anchor tie keeps the lower id",
			row: pairRow{A: 30, B: 31, LaneA: "bgm", LaneB: "vndb",
				AnchorsA: 1, AnchorsB: 1, RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
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
		RefOverlap: true, SharedNorms: 1,
		DateA: day(t, "2015-08-10"), DateB: day(t, "2015-09-25"),
	})
	if want := "ref+date(2015-08-10~2015-09-25)"; got != want {
		t.Fatalf("evidence = %q, want %q", got, want)
	}
	if got := pairEvidence(pairRow{LabelOverlap: true, SharedNorms: 2}); got != "label+norms=2" {
		t.Fatalf("evidence = %q, want %q", got, "label+norms=2")
	}
	if got := pairEvidence(pairRow{SharedNorms: 1}); got != "none" {
		t.Fatalf("evidence = %q, want %q", got, "none")
	}
	if got := pairEvidence(pairRow{RefOverlapCI: true, SharedNorms: 0}); got != "ref_ci" {
		t.Fatalf("evidence = %q, want %q", got, "ref_ci")
	}
	if got := pairEvidence(pairRow{RefOverlap: true, RefOverlapCI: true, SharedNorms: 1}); got != "ref" {
		t.Fatalf("evidence = %q, want %q", got, "ref")
	}
}

func TestClassifyNeverElectsAHiddenSurvivor(t *testing.T) {
	rows := []pairRow{
		{A: 1, B: 2, LaneA: "kungal", LaneB: "bgm", HiddenA: true, AnchorsB: 3, RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
		{A: 2, B: 3, LaneA: "bgm", LaneB: "vndb", RefOverlap: true, SharedNorms: 1, SharedOfficial: 1},
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
