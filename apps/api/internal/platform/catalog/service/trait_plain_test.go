package service

import (
	"slices"
	"testing"
)

func TestPlainTraitDescription(t *testing.T) {
	got := PlainTraitDescription("Uses [url=https://x.test]magic[/url].\n\n\n\nMore")
	want := "Uses magic.\n\nMore"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	got = PlainTraitDescription("  [b]Bold[/b] and [i]it[/i].  ")
	if got != "Bold and it." {
		t.Fatalf("tags: %q", got)
	}
	got = PlainTraitDescription("[url=https://a.test]keep[/url] [spoiler]x[/spoiler]")
	if got != "keep x" {
		t.Fatalf("mixed: %q", got)
	}
}

func TestTraitIntros(t *testing.T) {
	got := traitIntros("This character is [b]small[/b].\n\nMore", "该角色身材娇小。\n\n还有。")
	if len(got) != 2 || got[0].Lang != "en" || got[1].Lang != "zh-Hans" {
		t.Fatalf("%+v", got)
	}
	if got[0].Intro != "This character is small.\n\nMore" || got[0].Source != "vndb" || got[0].Machine {
		t.Fatalf("en %+v", got[0])
	}
	if got[1].Intro != "该角色身材娇小。\n\n还有。" || got[1].Source != "vndb" || got[1].Machine {
		t.Fatalf("zh %+v", got[1])
	}
	onlyEn := traitIntros("Hello", "")
	if len(onlyEn) != 1 || onlyEn[0].Lang != "en" {
		t.Fatalf("only en %+v", onlyEn)
	}
	none := traitIntros("", "")
	if none == nil || len(none) != 0 {
		t.Fatalf("none %#v", none)
	}
}

func TestSplitTraitAliases(t *testing.T) {
	got := SplitTraitAliases("a\n\n b \na")
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("got %q", got)
	}
	got = SplitTraitAliases("")
	if got == nil || len(got) != 0 {
		t.Fatalf("empty: %#v", got)
	}
	got = SplitTraitAliases("  \n\n  ")
	if len(got) != 0 {
		t.Fatalf("whitespace: %#v", got)
	}
}
