package sanitize

import "testing"

func TestCookStripsXSS(t *testing.T) {
	cases := []struct{ name, raw, mustNotContain string }{
		{"script tag", "hello <script>alert(1)</script> world", "<script"},
		{"img onerror", `<img src=x onerror="alert(1)">`, "onerror"},
		{"js scheme link", "[click](javascript:alert(1))", "javascript:"},
		{"iframe", "<iframe src=//evil></iframe>", "<iframe"},
		{"event handler", `<a href="http://x" onclick="steal()">x</a>`, "onclick"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Cook(c.raw).HTML
			if contains(got, c.mustNotContain) {
				t.Errorf("cooked HTML must not contain %q, got: %s", c.mustNotContain, got)
			}
		})
	}
}

func TestCookKeepsLegitimateMarkdown(t *testing.T) {
	got := Cook("# Title\n\n**bold** and *em* and a [link](https://example.com).").HTML
	for _, want := range []string{"<strong>bold</strong>", "<em>em</em>", `href="https://example.com"`} {
		if !contains(got, want) {
			t.Errorf("cooked HTML should contain %q, got: %s", want, got)
		}
	}
	if !contains(got, "nofollow") {
		t.Errorf("links should be rel=nofollow, got: %s", got)
	}
}

func TestCookCounts(t *testing.T) {
	c := Cook("see [a](https://a.com) and [b](https://b.com)\n\n![pic](https://img.com/x.png)\n\nhi @alice and @bob")
	if c.Links != 2 {
		t.Errorf("links: want 2, got %d (%s)", c.Links, c.HTML)
	}
	if c.Images != 1 {
		t.Errorf("images: want 1, got %d", c.Images)
	}
	if c.Mentions != 2 {
		t.Errorf("mentions: want 2, got %d", c.Mentions)
	}
}

func TestMentionCountIgnoresEmail(t *testing.T) {
	if n := countMentions("mail me at user@example.com please"); n != 0 {
		t.Errorf("email local part must not count as a mention, got %d", n)
	}
	if n := countMentions("@lead cc @dev-2"); n != 2 {
		t.Errorf("leading + hyphenated mentions: want 2, got %d", n)
	}
}

func TestSanitizerVersion(t *testing.T) {
	if Cook("x").Version != SanitizerVersion {
		t.Fatal("cooked version must equal SanitizerVersion")
	}
	if SanitizerVersion < 1 {
		t.Fatal("SanitizerVersion must start at 1")
	}
}

func contains(hay, needle string) bool {
	return len(needle) > 0 && indexOf(hay, needle) >= 0
}

func indexOf(hay, needle string) int {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}

func TestPlainTextMasksSpoilers(t *testing.T) {
	got := PlainText("前||中||后\n\n:::spoiler\n藏\n:::")
	if got != "前 ███ 后 ███" {
		t.Fatalf("got %q", got)
	}
}
