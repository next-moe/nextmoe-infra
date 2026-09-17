package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFoldName(t *testing.T) {
	cases := []struct {
		a, b  string
		equal bool
	}{
		{"蓮佛雪之進", "蓮仏 雪之進", true},
		{"冬月十夜", "冬月 十夜", true},
		{"硯川・e・涙香", "硯川・E・涙香", true},
		{"髙橋しょう子", "高橋しょう子", true},
		{"硯川・e・涙香", "硯川・ユーフラジー・涙香", false},
		{"蓮見榛佳", "蓮見槇夏", false},
	}
	for _, c := range cases {
		if got := foldName(c.a) == foldName(c.b); got != c.equal {
			t.Errorf("foldName(%q) vs foldName(%q): equal=%v, want %v", c.a, c.b, got, c.equal)
		}
	}
}

func TestNamesSimilar(t *testing.T) {
	cases := []struct {
		a, b    string
		similar bool
	}{
		{"硯川・e・涙香", "硯川・ユーフラジー・涙香", true},
		{"蓮見榛佳", "蓮見槇夏", true},
		{"アリス", "有栖", false},
		{"雪子", "雪美", false},
		{"エリカ", "エリナ", true},
	}
	for _, c := range cases {
		if got := namesSimilar([]string{c.a}, []string{c.b}); got != c.similar {
			t.Errorf("namesSimilar(%q, %q) = %v, want %v", c.a, c.b, got, c.similar)
		}
	}
}

func TestRunEmit(t *testing.T) {
	dir := t.TempDir()
	pairs := []pairMeta{
		{A: 10, B: 20, Tier: 1, Works: []int64{1}, AName: "a", BName: "b",
			ASources: []string{"vndb"}, BSources: []string{"bangumi"}, BRich: richness{Img: true}},
		{A: 30, B: 40, Tier: 3, Works: []int64{1}, ASources: []string{"vndb"}, BSources: []string{"bangumi"}},
		{A: 50, B: 60, Tier: 2, Works: []int64{2}, ASources: []string{"vndb"}, BSources: []string{"erogamescape"}},
		{A: 70, B: 80, Tier: 1, Works: []int64{3}, ASources: []string{"vndb"}, BSources: []string{"bangumi"}, Instance: true},
		{A: 90, B: 91, Tier: 3, Works: []int64{4}, ASources: []string{"vndb"}, BSources: []string{"bangumi"}},
		{A: 100, B: 110, Tier: 1, Works: []int64{5}, ASources: []string{"vndb"}, BSources: []string{"bangumi"}},
		{A: 110, B: 120, Tier: 1, Works: []int64{5}, ASources: []string{"bangumi"}, BSources: []string{"vndb"}},
	}
	verdicts := []map[string]any{
		{"key": "xsrc:10:20", "bucket": "character_pair", "verdict": "merge", "confidence": 0.98},
		{"key": "xsrc:30:40", "bucket": "character_pair", "verdict": "merge", "confidence": 0.7},
		{"key": "xsrc:50:60", "bucket": "character_cv", "verdict": "unsure", "confidence": 0.5},
		{"key": "xsrc:70:80", "bucket": "character_pair", "verdict": "merge", "confidence": 0.99},
		{"key": "xsrc:90:91", "bucket": "character_pair", "verdict": "distinct", "confidence": 0.9},
		{"key": "xsrc:100:110", "bucket": "character_pair", "verdict": "merge", "confidence": 0.99},
		{"key": "xsrc:110:120", "bucket": "character_pair", "verdict": "merge", "confidence": 0.99},
	}
	writeJSONL(t, filepath.Join(dir, "pairs.jsonl"), pairs)
	writeJSONL(t, filepath.Join(dir, "verdicts.jsonl"), verdicts)

	var out strings.Builder
	wl := filepath.Join(dir, "worklist.jsonl")
	review := filepath.Join(dir, "review.txt")
	if err := runEmit(filepath.Join(dir, "pairs.jsonl"), filepath.Join(dir, "verdicts.jsonl"), wl, review, &out); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(wl)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(got)), "\n")
	if len(lines) != 1 {
		t.Fatalf("worklist should hold exactly the one clean auto group, got %d: %s", len(lines), got)
	}
	var g struct {
		Class    string  `json:"class"`
		Survivor int64   `json:"survivor"`
		Sources  []int64 `json:"sources"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &g); err != nil {
		t.Fatal(err)
	}
	if g.Class != "character" || g.Survivor != 20 || len(g.Sources) != 1 || g.Sources[0] != 10 {
		t.Errorf("portrait-bearing side must survive: got %+v", g)
	}

	rv, err := os.ReadFile(review)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"低置信", "unsure", "instance", "组内同源"} {
		if !strings.Contains(string(rv), want) {
			t.Errorf("review file should carry a %q line:\n%s", want, rv)
		}
	}
	if strings.Contains(string(rv), "90") && strings.Contains(string(rv), "distinct 弃") {
		t.Errorf("distinct pairs are dropped, not reviewed")
	}
	if !strings.Contains(out.String(), "same_source_deferred=1") {
		t.Errorf("stats line should count the deferred chain group: %s", out.String())
	}
}

func writeJSONL[T any](t *testing.T, path string, rows []T) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNamesSimilarASCIIFloor(t *testing.T) {
	if namesSimilar([]string{"蓝Saber"}, []string{"Berserker"}) {
		t.Error("a shared Latin trigram must not qualify")
	}
	if !namesSimilar([]string{"Shion"}, []string{"Shion Juujou"}) {
		t.Error("a shared Latin word still qualifies via the segment rule")
	}
}

func TestQualifiedPair(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"リップ（少女編）", "リップ・ゲルソーク・ゼングルクル", true},
		{"グリム", "グリム(ヴィルヘルム)", true},
		{"猫（キャット）", "猫", true},
		{"【覚醒】ミカ", "ミカ", true},
		{"SILENCE(シランス)", "シランス", false},
		{"高嶺 鏡華", "高嶺鏡華", false},
		{"アルビレオ", "白鳥のナイト", false},
		{"木下 百合（右）/蘭（左）", "木下百合", true},
		{"猫（キャット）", "キャットの猫", false},
	}
	for _, c := range cases {
		if got := qualifiedPair(c.a, c.b); got != c.want {
			t.Errorf("qualifiedPair(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
