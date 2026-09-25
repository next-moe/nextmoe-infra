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
