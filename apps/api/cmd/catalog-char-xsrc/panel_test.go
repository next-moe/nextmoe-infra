package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"api/internal/jobs/personadj"
)

func TestClassifyReview(t *testing.T) {
	src := func(s ...string) []string { return s }
	pairs := []pairMeta{
		{A: 1, B: 2, Tier: 1, ASources: src("vndb"), BSources: src("bangumi")},
		{A: 3, B: 4, Tier: 3, ASources: src("vndb"), BSources: src("bangumi")},
		{A: 5, B: 6, Tier: 2, ASources: src("vndb"), BSources: src("erogamescape")},
		{A: 7, B: 8, Tier: 1, Instance: true, ASources: src("vndb"), BSources: src("bangumi")},
		{A: 9, B: 10, Tier: 1, ASources: src("vndb"), BSources: src("bangumi")},
		{A: 10, B: 11, Tier: 1, ASources: src("bangumi"), BSources: src("vndb")},
		{A: 12, B: 13, Tier: 3, ASources: src("vndb"), BSources: src("bangumi")},
		{A: 14, B: 15, Tier: 1, ASources: src("erogamescape"), BSources: src("vndb"), Qualified: true},
	}
	v := func(a, b int64, verdict string, conf float64) personadj.Verdict {
		return personadj.Verdict{Key: keyFor(a, b), Verdict: verdict, Confidence: conf}
	}
	verdicts := []personadj.Verdict{
		v(1, 2, "merge", 0.99),
		v(3, 4, "merge", 0.85),
		v(5, 6, "unsure", 0.50),
		v(7, 8, "merge", 0.99),
		v(9, 10, "merge", 0.97),
		v(10, 11, "merge", 0.96),
		v(12, 13, "distinct", 0.95),
		v(14, 15, "merge", 0.80),
	}
	tail := classifyReview(pairs, verdicts)
	got := map[string]string{}
	for _, p := range tail {
		got[keyFor(p.A, p.B)] = p.Cat
	}
	want := map[string]string{
		keyFor(3, 4):   catLowConf,
		keyFor(5, 6):   catUnsure,
		keyFor(7, 8):   catInstance,
		keyFor(9, 10):  catSameSource,
		keyFor(10, 11): catSameSource,
	}
	if len(got) != len(want) {
		t.Fatalf("tail = %v, want %v", got, want)
	}
	for k, cat := range want {
		if got[k] != cat {
			t.Errorf("%s: cat = %q, want %q", k, got[k], cat)
		}
	}
}

func keyFor(a, b int64) string { return "xsrc:" + itoa(a) + ":" + itoa(b) }

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestRunPanelEmit(t *testing.T) {
	dir := t.TempDir()
	src := func(s ...string) []string { return s }
	pp := func(a, b int64, cat string, r1 float64, as, bs []string) panelPair {
		return panelPair{
			pairMeta: pairMeta{A: a, B: b, Tier: 1, ASources: as, BSources: bs,
				ARich: richness{NAliases: int(a)}},
			Cat: cat, R1Conf: r1,
		}
	}
	ppairs := []any{
		pp(1, 2, catLowConf, 0.85, src("vndb"), src("bangumi")),
		pp(3, 4, catLowConf, 0.85, src("vndb"), src("bangumi")),
		pp(5, 6, catUnsure, 0.50, src("vndb"), src("bangumi")),
		pp(7, 8, catInstance, 0.99, src("vndb"), src("bangumi")),
		pp(9, 10, catInstance, 0.99, src("vndb"), src("bangumi")),
		pp(11, 12, catLowConf, 0.85, src("vndb"), src("bangumi")),
		pp(12, 13, catSameSource, 0.97, src("bangumi"), src("vndb")),
	}
	vv := func(a, b int64, vote int, verdict string, conf float64) any {
		return personadj.Verdict{Key: panelKey(a, b, vote), Verdict: verdict, Confidence: conf}
	}
	verdicts := []any{
		vv(1, 2, 1, "merge", 0.95), vv(1, 2, 2, "merge", 0.92), vv(1, 2, 3, "merge", 0.98),
		vv(3, 4, 1, "merge", 0.95), vv(3, 4, 2, "distinct", 0.90), vv(3, 4, 3, "unsure", 0.50),
		vv(5, 6, 1, "distinct", 0.90), vv(5, 6, 2, "distinct", 0.85), vv(5, 6, 3, "unsure", 0.50),
		vv(7, 8, 1, "merge", 0.92), vv(7, 8, 2, "merge", 0.96), vv(7, 8, 3, "merge", 0.97),
		vv(9, 10, 1, "merge", 0.96), vv(9, 10, 2, "merge", 0.95), vv(9, 10, 3, "merge", 0.99),
		vv(11, 12, 1, "merge", 0.95), vv(11, 12, 2, "merge", 0.96), vv(11, 12, 3, "merge", 0.97),
	}
	pairsPath := filepath.Join(dir, "pairs2.jsonl")
	verdictsPath := filepath.Join(dir, "verdicts2.jsonl")
	worklistPath := filepath.Join(dir, "worklist.jsonl")
	residualPath := filepath.Join(dir, "residual.txt")
	writeJSONL(t, pairsPath, ppairs)
	writeJSONL(t, verdictsPath, verdicts)

	var out strings.Builder
	if err := runPanelEmit(pairsPath, verdictsPath, worklistPath, residualPath, &out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(worklistPath)
	if err != nil {
		t.Fatal(err)
	}
	type row struct {
		Survivor int64   `json:"survivor"`
		Sources  []int64 `json:"sources"`
	}
	groups := map[int64][]int64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r row
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("worklist line %q: %v", line, err)
		}
		groups[r.Survivor] = r.Sources
	}
	if len(groups) != 3 {
		t.Fatalf("groups = %v, want 3; stats: %s", groups, out.String())
	}
	if got := groups[1]; len(got) != 1 || got[0] != 2 {
		t.Errorf("group for 1-2 = %v, want survivor 1 absorbing [2]", got)
	}
	if got := groups[9]; len(got) != 1 || got[0] != 10 {
		t.Errorf("instance pair 9-10 should merge into 9: %v", groups)
	}
	if got := groups[12]; len(got) != 1 || got[0] != 13 {
		t.Errorf("structural pair 12-13 should merge into 12: %v", groups)
	}
	for s, srcs := range groups {
		for _, id := range srcs {
			if id == 11 || s == 11 {
				t.Errorf("conflicted edge 11-12 must not merge: %v", groups)
			}
		}
	}

	res, err := os.ReadFile(residualPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res), "分歧") {
		t.Errorf("residual should hold the split-vote pair 3-4: %q", string(res))
	}
	if !strings.Contains(string(res), "并组后同源冲突") {
		t.Errorf("residual should hold the conflicted structural edge: %q", string(res))
	}
	if strings.Contains(string(res), "xsrc") && strings.Contains(string(res), " 5 ") {
		t.Errorf("closed-distinct pair 5-6 must not be residual: %q", string(res))
	}
	stats := out.String()
	for _, want := range []string{"accept_lowconf=2", "accept_instance=1", "closed_distinct=1",
		"closed_instance=1", "residual=1", "edge_conflict=1", "groups_emitted=3"} {
		if !strings.Contains(stats, want) {
			t.Errorf("stats missing %q: %s", want, stats)
		}
	}
}
