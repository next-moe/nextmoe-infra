package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"api/internal/jobs/personadj"
)

func TestRunEmitAllClaimsEachCharacterOnce(t *testing.T) {
	dir := t.TempDir()
	src := func(s ...string) []string { return s }
	pm := func(a, b int64, as, bs []string, instance bool) pairMeta {
		return pairMeta{A: a, B: b, Tier: 1, ASources: as, BSources: bs, Instance: instance,
			ARich: richness{NAliases: 100 - int(a)}, BRich: richness{NAliases: 100 - int(b)}}
	}
	pairs := []any{
		pm(1, 2, src("vndb"), src("bangumi"), false),
		pm(2, 3, src("bangumi"), src("erogamescape"), false),
		pm(4, 5, src("vndb"), src("bangumi"), true),
		pm(6, 7, src("vndb"), src("bangumi"), false),
		pm(7, 8, src("bangumi"), src("vndb"), false),
		pm(9, 10, src("vndb"), src("bangumi"), false),
		pm(11, 12, src("vndb"), src("bangumi"), false),
		pm(13, 14, src("vndb"), src("erogamescape"), false),
		pm(15, 16, src("vndb"), src("bangumi"), false),
		pairMeta{A: 17, B: 18, Tier: 1, ASources: src("erogamescape"), BSources: src("vndb"), Qualified: true,
			AName: "グリム(ヴィルヘルム)", BName: "グリム"},
	}
	r1 := func(a, b int64, verdict string, conf float64) any {
		return personadj.Verdict{Key: keyFor(a, b), Verdict: verdict, Confidence: conf}
	}
	verdicts := []any{
		r1(1, 2, "merge", 0.99),
		r1(2, 3, "merge", 0.90),
		r1(4, 5, "merge", 0.99),
		r1(6, 7, "merge", 0.99),
		r1(7, 8, "merge", 0.97),
		r1(9, 10, "distinct", 0.95),
		r1(13, 14, "merge", 0.80),
		r1(15, 16, "unsure", 0.97),
		r1(17, 18, "merge", 0.98),
	}
	pp := func(p any, cat string, conf float64) any {
		return panelPair{pairMeta: p.(pairMeta), Cat: cat, R1Conf: conf}
	}
	ppairs := []any{
		pp(pairs[1], catLowConf, 0.90),
		pp(pairs[2], catInstance, 0.99),
		pp(pairs[3], catSameSource, 0.99),
		pp(pairs[4], catSameSource, 0.97),
		pp(pairs[7], catLowConf, 0.80),
	}
	vote := func(a, b int64, n int, verdict string, conf float64) any {
		return personadj.Verdict{Key: panelKey(a, b, n), Verdict: verdict, Confidence: conf}
	}
	verdicts2 := []any{
		vote(2, 3, 1, "merge", 0.90), vote(2, 3, 2, "merge", 0.88), vote(2, 3, 3, "merge", 0.95),
		vote(4, 5, 1, "merge", 0.96), vote(4, 5, 2, "merge", 0.97), vote(4, 5, 3, "merge", 0.99),
		vote(13, 14, 1, "merge", 0.90), vote(13, 14, 2, "unsure", 0.50), vote(13, 14, 3, "distinct", 0.70),
	}
	paths := map[string]string{}
	for _, name := range []string{"pairs", "verdicts", "pairs2", "verdicts2", "worklist", "residual"} {
		paths[name] = filepath.Join(dir, name)
	}
	writeJSONL(t, paths["pairs"], pairs)
	writeJSONL(t, paths["verdicts"], verdicts)
	writeJSONL(t, paths["pairs2"], ppairs)
	writeJSONL(t, paths["verdicts2"], verdicts2)

	var out strings.Builder
	if err := runEmitAll(paths["pairs"], paths["verdicts"], paths["pairs2"], paths["verdicts2"],
		paths["worklist"], paths["residual"], &out); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(paths["worklist"])
	if err != nil {
		t.Fatal(err)
	}
	groups := map[int64][]int64{}
	claimed := map[int64]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r struct {
			Class    string  `json:"class"`
			Survivor int64   `json:"survivor"`
			Sources  []int64 `json:"sources"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("worklist line %q: %v", line, err)
		}
		if r.Class != "character" {
			t.Errorf("class = %q", r.Class)
		}
		sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i] < r.Sources[j] })
		groups[r.Survivor] = r.Sources
		for _, id := range append([]int64{r.Survivor}, r.Sources...) {
			claimed[id]++
		}
	}
	for id, n := range claimed {
		if n > 1 {
			t.Errorf("character %d is claimed %d times: %v", id, n, groups)
		}
	}
	want := map[int64][]int64{
		1: {2, 3},
		4: {5},
		6: {7},
	}
	if len(groups) != len(want) {
		t.Fatalf("groups = %v, want %v; stats: %s", groups, want, out.String())
	}
	for s, srcs := range want {
		got := groups[s]
		if len(got) != len(srcs) {
			t.Errorf("group %d = %v, want %v", s, got, srcs)
			continue
		}
		for i := range srcs {
			if got[i] != srcs[i] {
				t.Errorf("group %d = %v, want %v", s, got, srcs)
			}
		}
	}

	res, err := os.ReadFile(paths["residual"])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(res), "[并组后同源冲突/auto] tier=1 7 ") {
		t.Errorf("the weaker vndb edge 7-8 must land in the residual: %q", res)
	}
	if !strings.Contains(string(res), "[限定名条目] tier=1 17 グリム(ヴィルヘルム)") {
		t.Errorf("a qualified pair judged merge must wait in the residual: %q", res)
	}
	if !strings.Contains(string(res), "分歧") {
		t.Errorf("the split panel pair 13-14 must land in the residual: %q", res)
	}
	stats := out.String()
	for _, w := range []string{"pairs=10 ", "judged=9 ", "auto_edges=3 ", "held_qualified=1 ", "panel_pairs=3 ",
		"panel_accepted=2 ", "residual=1 ", "edge_conflict=1 ", "edges_applied=4 ", "groups_emitted=3"} {
		if !strings.Contains(stats, w) {
			t.Errorf("stats missing %q: %s", w, stats)
		}
	}
}
