package main

import (
	"testing"

	"api/internal/platform/trust/model"
)

func TestClassifySeparatesMeasuredFromUnevidenced(t *testing.T) {
	stats := []termStat{
		{ID: 1, Term: "loud-and-wrong", Hits: 4000, Flagged: 50, Precision: 0.0125},
		{ID: 2, Term: "loud-and-right", Hits: 4000, Flagged: 3000, Precision: 0.75},
		{ID: 3, Term: "never-fired", Hits: 0, Flagged: 0},
		{ID: 4, Term: "barely-fired-badly", Hits: 3, Flagged: 0},
	}

	got, _ := classify(stats, 20, 0.10, false)
	if len(got) != 1 || got[0].ID != 1 {
		t.Fatalf("measured-only pass retired %+v, want just loud-and-wrong", ids(got))
	}
	if got[0].Reason == "" {
		t.Fatal("a retired term must carry the reason into the backup file")
	}

	got, _ = classify(stats, 20, 0.10, true)
	if len(got) != 3 {
		t.Fatalf("with -drop-unevidenced retired %v, want 3", ids(got))
	}
	for _, s := range got {
		if s.ID == 2 {
			t.Fatal("a term with 75% precision was retired; the policy is inverted")
		}
	}
}

func TestClassifyRespectsMinHits(t *testing.T) {
	stats := []termStat{{ID: 1, Term: "rare", Hits: 2, Flagged: 0, Precision: 0}}
	if got, _ := classify(stats, 20, 0.10, false); len(got) != 0 {
		t.Fatalf("retired a term on %d observations: %v", stats[0].Hits, ids(got))
	}
	stats[0].Hits = 20
	if got, _ := classify(stats, 20, 0.10, false); len(got) != 1 {
		t.Fatalf("a term at exactly min-hits was not judged: %v", ids(got))
	}
}

func ids(ss []termStat) []int64 {
	out := make([]int64, len(ss))
	for i, s := range ss {
		out[i] = s.ID
	}
	return out
}

func TestClassifyNeverRetiresComplianceTerms(t *testing.T) {
	stats := []termStat{
		{ID: 1, Term: "abuse-noise", Purpose: model.TermPurposeAbuse,
			Hits: 4000, Flagged: 10, Precision: 0.0025},
		{ID: 2, Term: "compliance-term", Purpose: model.TermPurposeCompliance,
			Hits: 4000, Flagged: 0, Precision: 0},
		{ID: 3, Term: "compliance-quiet", Purpose: model.TermPurposeCompliance,
			Hits: 0, Flagged: 0},
	}

	doomed, review := classify(stats, 20, 0.10, true)
	for _, s := range doomed {
		if s.Purpose == model.TermPurposeCompliance {
			t.Fatalf("compliance term %q was retired; the lexicon is not safe", s.Term)
		}
	}
	if len(doomed) != 1 || doomed[0].ID != 1 {
		t.Fatalf("retired %v, want just the abuse-purpose noise term", ids(doomed))
	}
	if len(review) != 2 {
		t.Fatalf("review bucket = %v, want both compliance terms nominated", ids(review))
	}
	for _, s := range review {
		if s.Reason == "" {
			t.Fatalf("compliance term %q nominated with no stated reason", s.Term)
		}
	}
}

func TestDomainShaped(t *testing.T) {
	for term, want := range map[string]bool{
		"h.ltf8.cn":             true,
		"haibo.16f.cn":          true,
		"qqhaoma2.cn:81":        true,
		"www.example.com/a-b_c": true,
		".cn":                   true,
		"admin":                 false,
		"10086":                 false,
		"u-r":                   false,
		"美腿.com":                false,
		"key text.cn":           false,
		"第一次":                   false,
	} {
		if got := domainShaped(term); got != want {
			t.Errorf("domainShaped(%q) = %t, want %t", term, got, want)
		}
	}
}
