package getchuchars

import "testing"

func rr(getchu string, work, char int64, name, alias string) rosterRow {
	return rosterRow{GetchuID: getchu, WorkID: work, CharacterID: char,
		KeyName: normKey(name), KeyAlias: normKey(alias)}
}

func TestMatchLevels(t *testing.T) {
	idx := buildIndex([]rosterRow{
		rr("g1", 1, 100, "九條都", ""),
		rr("g1", 1, 101, "新海天", "そら"),
		rr("g1", 1, 102, "香坂春風", "こうさかはるかぜ"),
	})
	got, st := match([]getchuChar{
		{GetchuID: "g1", Ordinal: 0, Name: "九條　都", Profile: "p"},
		{GetchuID: "g1", Ordinal: 1, Name: "そら", Profile: "p"},
		{GetchuID: "g1", Ordinal: 2, Name: "香坂 春風（はるかぜ）", Reading: "こうさかはるかぜ", Profile: "p"},
	}, idx)

	if st.ByName != 1 || st.ByAlias != 1 || st.ByReading != 1 {
		t.Errorf("levels = name %d alias %d reading %d; want 1/1/1", st.ByName, st.ByAlias, st.ByReading)
	}
	if st.Matched != 3 || len(got) != 3 {
		t.Errorf("matched = %d, candidates = %d; want 3/3 — each level resolves a different character",
			st.Matched, len(got))
	}
	seen := map[string]bool{}
	for _, c := range got {
		seen[c.MatchedBy] = true
	}
	for _, want := range []string{MatchDisplayName, MatchAlias, MatchReading} {
		if !seen[want] {
			t.Errorf("no candidate carries matched_by=%q — the audit trail is incomplete", want)
		}
	}
}

func TestCollisionDropsBothSides(t *testing.T) {
	idx := buildIndex([]rosterRow{rr("g1", 1, 101, "新海天", "そら")})
	got, st := match([]getchuChar{
		{GetchuID: "g1", Ordinal: 0, Name: "新海天"},
		{GetchuID: "g1", Ordinal: 1, Name: "そら"},
	}, idx)
	if len(got) != 0 || st.Collided != 2 || st.Matched != 0 {
		t.Errorf("both sides of a collision must drop: got %d candidates, stats %+v", len(got), st)
	}
}

func TestAmbiguityIsDropped(t *testing.T) {
	idx := buildIndex([]rosterRow{
		rr("g1", 1, 200, "神楽坂", ""),
		rr("g1", 1, 201, "神楽坂", ""),
	})
	got, st := match([]getchuChar{{GetchuID: "g1", Name: "神楽坂", Profile: "p"}}, idx)
	if len(got) != 0 || st.Ambiguous != 1 || st.Matched != 0 {
		t.Errorf("ambiguous name must be dropped: got %d candidates, stats %+v", len(got), st)
	}
}

func TestUnanchoredAndUnknownAreCounted(t *testing.T) {
	idx := buildIndex([]rosterRow{rr("g1", 1, 300, "有栖川", "")})
	_, st := match([]getchuChar{
		{GetchuID: "g1", Name: "有栖川"},
		{GetchuID: "g1", Name: "知らない子"},
		{GetchuID: "g9", Name: "誰か"},
	}, idx)
	if st.Matched+st.NoNameInWork+st.NoWork+st.Bundle+st.Ambiguous+st.Collided != st.Input {
		t.Errorf("buckets do not add up to the input: %+v", st)
	}
	if st.NoWork != 1 || st.NoNameInWork != 1 || st.Matched != 1 {
		t.Errorf("stats = %+v", st)
	}
}

func TestBundleProductMatchesNobody(t *testing.T) {
	bundle := rr("g1", 1, 400, "美咲", "")
	bundle.Bundle = true
	idx := buildIndex([]rosterRow{bundle, rr("g2", 2, 401, "美咲", "")})
	got, st := match([]getchuChar{
		{GetchuID: "g1", Name: "美咲", Profile: "another game's 美咲"},
		{GetchuID: "g2", Name: "美咲", Profile: "this game's 美咲"},
	}, idx)
	if st.Bundle != 1 || st.Matched != 1 || len(got) != 1 {
		t.Fatalf("a bundle product must match nobody while a single-VN one still does: got %d, stats %+v", len(got), st)
	}
	if got[0].CharacterID != 401 {
		t.Errorf("matched character = %d, want 401", got[0].CharacterID)
	}
}

func TestNormKey(t *testing.T) {
	cases := map[string]string{
		"九條　都":  "九條都",
		"九條 都":  "九條都",
		"ＡＢＣ":   "abc",
		" そら\t": "そら",
	}
	for in, want := range cases {
		if got := normKey(in); got != want {
			t.Errorf("normKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEditionsAreDedupedNotDropped(t *testing.T) {
	idx := buildIndex([]rosterRow{
		rr("g1", 1, 400, "九條都", ""),
		rr("g2", 1, 400, "九條都", ""),
	})
	got, st := match([]getchuChar{
		{GetchuID: "g1", Ordinal: 0, Name: "九條都", Profile: "短い"},
		{GetchuID: "g2", Ordinal: 0, Name: "九條都", Profile: "こちらのほうが長い紹介文です"},
	}, idx)

	if st.Matched != 1 || len(got) != 1 {
		t.Fatalf("editions must fold to one candidate: matched=%d candidates=%d", st.Matched, len(got))
	}
	if st.Deduped != 1 {
		t.Errorf("deduped = %d, want 1", st.Deduped)
	}
	if st.Collided != 0 {
		t.Errorf("collided = %d — editions are not ambiguity", st.Collided)
	}
	if got[0].Profile != "こちらのほうが長い紹介文です" {
		t.Errorf("kept the shorter profile: %q", got[0].Profile)
	}
}

func TestRichestIsDeterministic(t *testing.T) {
	g := []Candidate{
		{GetchuID: "g9", Ordinal: 1, Profile: "同じ長さ"},
		{GetchuID: "g2", Ordinal: 0, Profile: "同じ長さ"},
		{GetchuID: "g5", Ordinal: 3, Profile: "同じ長さ"},
	}
	for i := 0; i < 20; i++ {
		if got := richest(g); got.GetchuID != "g2" {
			t.Fatalf("tie-break drifted: got %s", got.GetchuID)
		}
	}
}

func TestEditionsListsEveryResolvedRow(t *testing.T) {
	idx := buildIndex([]rosterRow{
		rr("limited", 1, 100, "九條都", ""),
		rr("regular", 1, 100, "九條都", ""),
	})
	got, st := match([]getchuChar{
		{GetchuID: "regular", Ordinal: 3, Name: "九條都", Profile: "short"},
		{GetchuID: "limited", Ordinal: 7, Name: "九條都", Profile: "a much longer profile"},
	}, idx)

	if st.Matched != 1 || len(got) != 1 {
		t.Fatalf("matched = %d, candidates = %d; want 1/1 — two editions are one character", st.Matched, len(got))
	}
	c := got[0]
	if c.GetchuID != "limited" || c.Ordinal != 7 {
		t.Errorf("chosen edition = %s/%d; want limited/7 (the richest profile)", c.GetchuID, c.Ordinal)
	}
	want := []Edition{{GetchuID: "limited", Ordinal: 7}, {GetchuID: "regular", Ordinal: 3}}
	if len(c.Editions) != len(want) {
		t.Fatalf("editions = %+v; want %+v", c.Editions, want)
	}
	for i := range want {
		if c.Editions[i] != want[i] {
			t.Errorf("editions[%d] = %+v; want %+v", i, c.Editions[i], want[i])
		}
	}
}

func TestEditionsPopulatedForASingleMatch(t *testing.T) {
	idx := buildIndex([]rosterRow{rr("g1", 1, 100, "九條都", "")})
	got, _ := match([]getchuChar{{GetchuID: "g1", Ordinal: 2, Name: "九條都", Profile: "p"}}, idx)
	if len(got) != 1 {
		t.Fatalf("candidates = %d; want 1", len(got))
	}
	if len(got[0].Editions) != 1 || got[0].Editions[0] != (Edition{GetchuID: "g1", Ordinal: 2}) {
		t.Errorf("editions = %+v; want exactly [{g1 2}]", got[0].Editions)
	}
}
