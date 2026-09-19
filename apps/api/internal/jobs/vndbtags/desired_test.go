package vndbtags

import (
	"strings"
	"testing"
)

func pi(v int16) *int16 { return &v }
func pb(v bool) *bool   { return &v }

func vote(tag string, score int16, spoiler *int16, ignore bool, lie *bool) Vote {
	return Vote{Tag: tag, Score: score, Spoiler: spoiler, Ignore: ignore, Lie: lie}
}

func meta(name, alias, cat string, spoil int16) TagMeta {
	return TagMeta{Name: name, Alias: alias, Cat: cat, DefaultSpoil: spoil}
}

func TestDesiredTagsIgnoreAndFloor(t *testing.T) {
	tags := map[string]TagMeta{"g1": meta("One", "", "cont", 0)}
	tm := map[string]string{}

	got := desiredTags([]Vote{
		vote("g1", 3, nil, true, nil),
		vote("g1", 0, nil, false, nil),
	}, tags, tm)
	if _, ok := got["One"]; ok {
		t.Fatal("ignored votes must be dropped before the mean; remaining mean 0 must not keep the tag")
	}

	got = desiredTags([]Vote{
		vote("g1", 3, nil, true, nil),
		vote("g1", 3, nil, true, nil),
	}, tags, tm)
	if len(got) != 0 {
		t.Fatalf("a tag with only ignored votes must be dropped, got %v", got)
	}

	got = desiredTags([]Vote{vote("g1", 1, nil, false, nil)}, tags, tm)
	if d, ok := got["One"]; !ok || d.Count != 1 || d.Spoiler != 0 {
		t.Fatalf("mean exactly 1.0 must be kept: %+v ok=%v", d, ok)
	}

	var low []Vote
	for i := 0; i < 99; i++ {
		low = append(low, vote("g1", 1, nil, false, nil))
	}
	low = append(low, vote("g1", 0, nil, false, nil))
	got = desiredTags(low, tags, tm)
	if _, ok := got["One"]; ok {
		t.Fatal("mean 0.99 must be dropped")
	}
}

func TestDesiredTagsSpoilerBoundaries(t *testing.T) {
	tags := map[string]TagMeta{"g1": meta("One", "", "cont", 2)}
	tm := map[string]string{}
	keep := func(spoilers ...int16) int16 {
		t.Helper()
		var vs []Vote
		for _, s := range spoilers {
			vs = append(vs, vote("g1", 2, pi(s), false, nil))
		}
		got := desiredTags(vs, tags, tm)
		d, ok := got["One"]
		if !ok {
			t.Fatalf("expected kept tag for spoilers %v", spoilers)
		}
		return d.Spoiler
	}

	// 2/5 = 0.4 → 0
	if s := keep(0, 0, 0, 0, 2); s != 0 {
		t.Errorf("mean 0.4: spoiler=%d, want 0", s)
	}
	// 41/100 = 0.41 → 1
	var s041 []int16
	for i := 0; i < 59; i++ {
		s041 = append(s041, 0)
	}
	for i := 0; i < 41; i++ {
		s041 = append(s041, 1)
	}
	if s := keep(s041...); s != 1 {
		t.Errorf("mean 0.41: spoiler=%d, want 1", s)
	}
	// (7*1 + 3*2)/10 = 1.3 → 1
	if s := keep(1, 1, 1, 1, 1, 1, 1, 2, 2, 2); s != 1 {
		t.Errorf("mean 1.3: spoiler=%d, want 1", s)
	}
	// (69*1 + 31*2)/100 = 1.31 → 2
	var s131 []int16
	for i := 0; i < 69; i++ {
		s131 = append(s131, 1)
	}
	for i := 0; i < 31; i++ {
		s131 = append(s131, 2)
	}
	if s := keep(s131...); s != 2 {
		t.Errorf("mean 1.31: spoiler=%d, want 2", s)
	}

	got := desiredTags([]Vote{vote("g1", 2, nil, false, nil), vote("g1", 2, nil, false, nil)}, tags, tm)
	if d := got["One"]; d.Spoiler != 2 {
		t.Errorf("all-NULL spoilers: spoiler=%d, want defaultspoil 2", d.Spoiler)
	}
}

func TestDesiredTagsLie(t *testing.T) {
	tags := map[string]TagMeta{"g1": meta("One", "", "cont", 0)}
	tm := map[string]string{}

	got := desiredTags([]Vote{
		vote("g1", 2, nil, false, pb(true)),
		vote("g1", 2, nil, false, pb(false)),
	}, tags, tm)
	if _, ok := got["One"]; ok {
		t.Fatal("nt=1, nl=2 (exactly half) must be a lie and dropped")
	}

	got = desiredTags([]Vote{
		vote("g1", 2, nil, false, pb(true)),
		vote("g1", 2, nil, false, pb(false)),
		vote("g1", 2, nil, false, pb(false)),
	}, tags, tm)
	if _, ok := got["One"]; !ok {
		t.Fatal("nt=1, nl=3 must not be a lie")
	}

	got = desiredTags([]Vote{
		vote("g1", 2, nil, false, pb(false)),
		vote("g1", 2, nil, false, pb(false)),
	}, tags, tm)
	if _, ok := got["One"]; !ok {
		t.Fatal("nt=0 must not be a lie")
	}

	got = desiredTags([]Vote{
		vote("g1", 2, nil, false, nil),
		vote("g1", 2, nil, false, nil),
	}, tags, tm)
	if _, ok := got["One"]; !ok {
		t.Fatal("all-NULL lie must not be a lie")
	}
}

func TestDesiredTagsMissingFromTagTable(t *testing.T) {
	got := desiredTags([]Vote{vote("g999", 3, nil, false, nil)}, map[string]TagMeta{
		"g1": meta("One", "", "cont", 0),
	}, map[string]string{})
	if len(got) != 0 {
		t.Fatalf("tag absent from the tag table must be dropped, got %v", got)
	}
}

func TestDesiredTagsNaming(t *testing.T) {
	tm := map[string]string{
		"English Name": "中文名",
		"Mapped Alias": "别名中文",
		"Second Alias": "不该命中",
		"Mapped Two":   "空行后命中",
	}
	tags := map[string]TagMeta{
		"gDirect": meta("English Name", "Ignored\nAlias", "cont", 0),
		"gAlias":  meta("Unmapped English", "not-in-map\nMapped Alias\nSecond Alias", "cont", 0),
		"gNone":   meta("Plain English", "also-unmapped\n", "cont", 0),
		"gBlank":  meta("Unmapped Two", "\n  \nMapped Two", "cont", 0),
	}
	vs := []Vote{
		vote("gDirect", 2, nil, false, nil),
		vote("gAlias", 2, nil, false, nil),
		vote("gNone", 2, nil, false, nil),
		vote("gBlank", 2, nil, false, nil),
	}
	got := desiredTags(vs, tags, tm)
	if _, ok := got["空行后命中"]; !ok {
		t.Errorf("blank aliases must be skipped, not end the search: %v", got)
	}
	if _, ok := got["中文名"]; !ok {
		t.Errorf("direct map hit missing: %v", got)
	}
	if _, ok := got["别名中文"]; !ok {
		t.Errorf("first mapped alias missing: %v", got)
	}
	if _, ok := got["不该命中"]; ok {
		t.Errorf("later alias must not win: %v", got)
	}
	if _, ok := got["Plain English"]; !ok {
		t.Errorf("unmapped English missing: %v", got)
	}
}

func TestDesiredTagsMerge(t *testing.T) {
	tm := map[string]string{"A": "shared", "B": "shared"}
	tags := map[string]TagMeta{
		"gA": meta("A", "", "cont", 0),
		"gB": meta("B", "", "ero", 0),
	}
	got := desiredTags([]Vote{
		vote("gA", 2, pi(2), false, nil),
		vote("gA", 2, pi(2), false, nil),
		vote("gB", 2, pi(0), false, nil),
		vote("gB", 2, pi(0), false, nil),
		vote("gB", 2, pi(0), false, nil),
	}, tags, tm)
	d, ok := got["shared"]
	if !ok {
		t.Fatalf("merged name missing: %v", got)
	}
	if d.Spoiler != 0 {
		t.Errorf("merge spoiler=%d, want min 0", d.Spoiler)
	}
	if d.Count != 3 {
		t.Errorf("merge count=%d, want max 3", d.Count)
	}
	if !d.Ero {
		t.Error("merge ero must be true if either tag is ero")
	}
	for range 64 {
		if !desiredTags([]Vote{vote("gB", 2, nil, false, nil), vote("gA", 2, nil, false, nil)}, tags, tm)["shared"].Ero {
			t.Fatal("merge ero depends on which tag is visited first")
		}
	}
}

func TestDSNRequired(t *testing.T) {
	_, err := Run(t.Context(), Opts{})
	if err == nil || !strings.Contains(err.Error(), "catalog DSN is required (--dsn); refusing to guess") {
		t.Fatalf("empty DSN: err=%v", err)
	}
}
