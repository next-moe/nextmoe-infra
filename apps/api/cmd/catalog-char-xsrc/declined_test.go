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

func readGroups(t *testing.T, path string) map[int64][]int64 {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	groups := map[int64][]int64{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var r struct {
			Survivor int64   `json:"survivor"`
			Sources  []int64 `json:"sources"`
		}
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("worklist line %q: %v", line, err)
		}
		sort.Slice(r.Sources, func(i, j int) bool { return r.Sources[i] < r.Sources[j] })
		groups[r.Survivor] = r.Sources
	}
	return groups
}

func sameGroups(got, want map[int64][]int64) bool {
	if len(got) != len(want) {
		return false
	}
	for s, srcs := range want {
		g, ok := got[s]
		if !ok || len(g) != len(srcs) {
			return false
		}
		for i := range srcs {
			if g[i] != srcs[i] {
				return false
			}
		}
	}
	return true
}

func TestRunEmitAllNeverGroupsADeclinedPair(t *testing.T) {
	dir := t.TempDir()
	v, b, e, g := []string{"vndb"}, []string{"bangumi"}, []string{"erogamescape"}, []string{"getchu"}
	pm := func(a, bb int64, as, bs []string) pairMeta {
		return pairMeta{A: a, B: bb, Tier: 1, ASources: as, BSources: bs,
			ARich: richness{NAliases: 100 - int(a)}, BRich: richness{NAliases: 100 - int(bb)}}
	}
	qualified := pm(28, 30, v, e)
	qualified.Qualified = true
	pairs := []any{
		pm(1, 2, v, b), pm(2, 3, b, e), pm(1, 3, v, e),
		pm(4, 5, v, b), pm(5, 6, b, e), pm(4, 6, v, e),
		pm(7, 8, v, b), pm(8, 9, b, e), pm(7, 9, v, e),
		pm(28, 29, v, b), pm(29, 30, b, e), qualified,
		pm(10, 11, v, b), pm(11, 12, b, e),
		pm(13, 14, v, b), pm(15, 16, e, g), pm(14, 15, b, e), pm(14, 16, b, g),
	}
	r1 := func(a, bb int64, verdict string, conf float64) any {
		return personadj.Verdict{Key: keyFor(a, bb), Verdict: verdict, Confidence: conf}
	}
	verdicts := []any{
		r1(1, 2, "merge", 0.99), r1(2, 3, "merge", 0.98), r1(1, 3, "distinct", 0.85),
		r1(4, 5, "merge", 0.99), r1(5, 6, "merge", 0.90), r1(4, 6, "merge", 0.80),
		r1(7, 8, "merge", 0.99), r1(8, 9, "merge", 0.90), r1(7, 9, "merge", 0.80),
		r1(28, 29, "merge", 0.99), r1(29, 30, "merge", 0.97), r1(28, 30, "merge", 0.98),
		r1(10, 11, "merge", 0.99), r1(11, 12, "merge", 0.90),
		r1(13, 14, "merge", 0.99), r1(15, 16, "merge", 0.98), r1(14, 15, "merge", 0.97), r1(14, 16, "distinct", 0.90),
	}
	pp := func(p any, conf float64) any {
		return panelPair{pairMeta: p.(pairMeta), Cat: catLowConf, R1Conf: conf}
	}
	ppairs := []any{pp(pairs[4], 0.90), pp(pairs[5], 0.80), pp(pairs[7], 0.90), pp(pairs[8], 0.80),
		pp(pairs[13], 0.90), pp(pm(10, 12, v, e), 0.80)}
	vote := func(a, bb int64, n int, verdict string, conf float64) any {
		return personadj.Verdict{Key: panelKey(a, bb, n), Verdict: verdict, Confidence: conf}
	}
	verdicts2 := []any{
		vote(5, 6, 1, "merge", 0.95), vote(5, 6, 2, "merge", 0.95), vote(5, 6, 3, "merge", 0.95),
		vote(4, 6, 1, "distinct", 0.90), vote(4, 6, 2, "distinct", 0.90), vote(4, 6, 3, "merge", 0.90),
		vote(8, 9, 1, "merge", 0.95), vote(8, 9, 2, "merge", 0.95), vote(8, 9, 3, "merge", 0.95),
		vote(7, 9, 1, "merge", 0.90), vote(7, 9, 2, "merge", 0.90), vote(7, 9, 3, "merge", 0.90),
		vote(11, 12, 1, "merge", 0.95), vote(11, 12, 2, "merge", 0.95), vote(11, 12, 3, "merge", 0.95),
		vote(10, 12, 1, "distinct", 0.90), vote(10, 12, 2, "distinct", 0.90), vote(10, 12, 3, "distinct", 0.90),
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
	want := map[int64][]int64{
		1:  {2},
		4:  {5},
		7:  {8, 9},
		10: {11},
		13: {14},
		15: {16},
		28: {29},
	}
	if got := readGroups(t, paths["worklist"]); !sameGroups(got, want) {
		t.Fatalf("groups = %v, want %v; stats: %s", got, want, out.String())
	}
	res, err := os.ReadFile(paths["residual"])
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"[并组后含已否决对 1-3/auto] tier=1 2 ", "[并组后含已否决对 4-6/lowconf] tier=1 5 ",
		"[并组后含已否决对 28-30/auto] tier=1 29 ", "[并组后含已否决对 10-12/lowconf] tier=1 11 ",
		"[并组后含已否决对 14-16/auto] tier=1 14 "} {
		if !strings.Contains(string(res), w) {
			t.Errorf("residual missing %q:\n%s", w, res)
		}
	}
	for _, w := range []string{"edge_declined=5 ", "edges_applied=8 ", "groups_emitted=7"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("stats missing %q: %s", w, out.String())
		}
	}
}

func TestRunPanelEmitNeverGroupsADeclinedPair(t *testing.T) {
	dir := t.TempDir()
	v, b, e := []string{"vndb"}, []string{"bangumi"}, []string{"erogamescape"}
	pp := func(a, bb int64, cat string, as, bs []string) any {
		return panelPair{pairMeta: pairMeta{A: a, B: bb, Tier: 1, ASources: as, BSources: bs,
			ARich: richness{NAliases: 100 - int(a)}}, Cat: cat, R1Conf: 0.9}
	}
	ppairs := []any{
		pp(1, 2, catLowConf, v, b), pp(2, 3, catLowConf, b, e), pp(1, 3, catUnsure, v, e),
		pp(4, 5, catLowConf, v, b), pp(5, 6, catLowConf, b, e), pp(4, 6, catUnsure, v, e),
	}
	vote := func(a, bb int64, verdict string, conf float64) []any {
		var out []any
		for n := 1; n <= panelVotes; n++ {
			out = append(out, personadj.Verdict{Key: panelKey(a, bb, n), Verdict: verdict, Confidence: conf})
		}
		return out
	}
	var verdicts []any
	verdicts = append(verdicts, vote(1, 2, "merge", 0.99)...)
	verdicts = append(verdicts, vote(2, 3, "merge", 0.90)...)
	verdicts = append(verdicts, vote(1, 3, "unsure", 0.50)...)
	verdicts = append(verdicts, vote(4, 5, "merge", 0.99)...)
	verdicts = append(verdicts, vote(5, 6, "merge", 0.90)...)
	verdicts = append(verdicts, vote(4, 6, "merge", 0.88)...)
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
	want := map[int64][]int64{1: {2}, 4: {5, 6}}
	if got := readGroups(t, worklistPath); !sameGroups(got, want) {
		t.Fatalf("groups = %v, want %v; stats: %s", got, want, out.String())
	}
	if !strings.Contains(out.String(), "edge_declined=1 ") {
		t.Errorf("stats should count the refused edge 2-3: %s", out.String())
	}
}

func TestRunEmitDefersAGroupHoldingADeclinedPair(t *testing.T) {
	dir := t.TempDir()
	v, b, e := []string{"vndb"}, []string{"bangumi"}, []string{"erogamescape"}
	pm := func(a, bb int64, as, bs []string) pairMeta {
		return pairMeta{A: a, B: bb, Tier: 1, ASources: as, BSources: bs}
	}
	qualified := pm(7, 9, v, e)
	qualified.Qualified = true
	pairs := []pairMeta{
		pm(1, 2, v, b), pm(2, 3, b, e), pm(1, 3, v, e),
		pm(4, 5, v, b), pm(5, 6, b, e), pm(4, 6, v, e),
		pm(7, 8, v, b), pm(8, 9, b, e), qualified,
		pm(10, 11, v, b),
	}
	r1 := func(a, bb int64, verdict string, conf float64) personadj.Verdict {
		return personadj.Verdict{Key: keyFor(a, bb), Verdict: verdict, Confidence: conf}
	}
	verdicts := []personadj.Verdict{
		r1(1, 2, "merge", 0.99), r1(2, 3, "merge", 0.99), r1(1, 3, "distinct", 0.90),
		r1(4, 5, "merge", 0.99), r1(5, 6, "merge", 0.99), r1(4, 6, "merge", 0.99),
		r1(7, 8, "merge", 0.99), r1(8, 9, "merge", 0.99), r1(7, 9, "merge", 0.99),
		r1(10, 11, "merge", 0.99),
	}
	writeJSONL(t, filepath.Join(dir, "pairs.jsonl"), pairs)
	writeJSONL(t, filepath.Join(dir, "verdicts.jsonl"), verdicts)
	var out strings.Builder
	wl, review := filepath.Join(dir, "worklist.jsonl"), filepath.Join(dir, "review.txt")
	if err := runEmit(filepath.Join(dir, "pairs.jsonl"), filepath.Join(dir, "verdicts.jsonl"), wl, review, &out); err != nil {
		t.Fatal(err)
	}
	want := map[int64][]int64{4: {5, 6}, 10: {11}}
	if got := readGroups(t, wl); !sameGroups(got, want) {
		t.Fatalf("groups = %v, want %v; stats: %s", got, want, out.String())
	}
	rv, err := os.ReadFile(review)
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"组内含已否决对 defer: ", "[限定名条目] tier=1 7 "} {
		if !strings.Contains(string(rv), w) {
			t.Errorf("review missing %q:\n%s", w, rv)
		}
	}
	for _, w := range []string{"qualified_held=1 ", "declined_deferred=2 ", "groups_emitted=2"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("stats missing %q: %s", w, out.String())
		}
	}
}
