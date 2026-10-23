package protocol

import "testing"

func TestIfNoneMatchWeakComparison(t *testing.T) {
	tag := `"abc"`
	for _, h := range []string{tag, `W/"abc"`, `*`, `"nope", ` + tag} {
		if !IfNoneMatch(h, tag) {
			t.Errorf("If-None-Match %q should match %s", h, tag)
		}
	}
	if IfNoneMatch(`"nope"`, tag) {
		t.Fatal("stale tag matched")
	}
	if IfNoneMatch("", tag) {
		t.Fatal("empty header matched")
	}
}

func TestIfMatchIgnoresWeakPrefix(t *testing.T) {
	tag := `"abc"`
	if !IfMatch(tag, tag) {
		t.Fatal("exact If-Match must match")
	}
	if !IfMatch(`W/"abc"`, tag) {
		t.Fatal("the W/ form a CDN hands out must match its strong tag")
	}
	if IfMatch(`W/"nope"`, tag) {
		t.Fatal("a stale weak tag matched")
	}
	if !IfMatch("*", tag) {
		t.Fatal("* must match an existing representation")
	}
	if IfMatch("*", "") {
		t.Fatal("* must not match a missing representation")
	}
	if IfMatch("", tag) {
		t.Fatal("absent If-Match is not a match")
	}
}
