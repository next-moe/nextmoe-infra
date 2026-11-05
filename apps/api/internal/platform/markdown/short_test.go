package markdown

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCookShortRendersTheDialect(t *testing.T) {
	cases := []struct {
		name, src string
		want      []string
		bad       []string
	}{
		{"single newline is a hard break", "第一行\n第二行", []string{"<p>第一行<br>\n第二行</p>"}, nil},
		{"paragraphs", "a\n\nb", []string{"<p>a</p>", "<p>b</p>"}, nil},
		{"emphasis", "**粗** *斜* ~~删~~ `码`", []string{"<strong>粗</strong>", "<em>斜</em>", "<del>删</del>", "<code>码</code>"}, nil},
		{"fenced code", "```go\nx := 1\n```", []string{`<pre><code class="language-go">x := 1`}, nil},
		{"link", "[站](https://example.com/a?b=1&c=2)", []string{`<a href="https://example.com/a?b=1&amp;c=2" rel="nofollow ugc">站</a>`}, nil},
		{"bare url", "见 https://example.com 吧", []string{`<a href="https://example.com" rel="nofollow ugc">https://example.com</a>`}, nil},
		{"www url", "www.example.com", []string{`href="http://www.example.com"`}, nil},
		{"lists", "- a\n- b\n\n1. c", []string{"<ul>", "<li>a</li>", "<ol>", "<li>c</li>"}, nil},
		{"ordered start", "3. c", []string{`<ol start="3">`}, nil},
		{"blockquote", "> 引用", []string{"<blockquote>", "引用"}, nil},
		{"inline spoiler", "结局是||全员幸存||。", []string{`<span class="kun-spoiler kun-spoiler-hidden">全员幸存</span>`}, nil},
		{"block spoiler", ":::spoiler\n真相\n:::", []string{`<div class="kun-spoiler kun-spoiler-hidden">`, "<p>真相</p>"}, nil},
		{"raw html is text", "<b>x</b><script>alert(1)</script>", []string{"&lt;b&gt;x&lt;/b&gt;&lt;script&gt;"}, []string{"<b>", "<script"}},
		{"html block is text", "<div onclick=x>\nhi\n</div>", []string{"&lt;div onclick=x&gt;"}, []string{"<div"}},
		{"image is alt text", "![封面 *图*](https://img.example/x.png)", []string{"<p>封面 图</p>"}, []string{"<img", "x.png"}},
		{"heading is a paragraph", "# 标题\n正文", []string{"<p># 标题<br>\n正文</p>"}, []string{"<h1"}},
		{"setext is a paragraph", "标题\n===", []string{"<p>标题<br>\n===</p>"}, []string{"<h1"}},
		{"table is text", "| a | b |\n|---|---|\n| 1 | 2 |", []string{"| a | b |"}, []string{"<table"}},
		{"thematic break is text", "a\n\n---", []string{"<p>---</p>"}, []string{"<hr"}},
		{"js link is text", "[点我](javascript:alert(1))", []string{"<p>点我</p>"}, []string{"javascript", "<a"}},
		{"relative link is text", "[站内](/galgame/1)", []string{"<p>站内</p>"}, []string{"<a"}},
		{"mention link is text", "[@kun](kungal-user:1)", []string{"<p>@kun</p>"}, []string{"<a"}},
		{"email is text", "mail a@b.com", []string{"a@b.com"}, []string{"mailto", "<a"}},
		{"indented paragraph stays prose", "    第一段\n    第二段", []string{"<p>第一段<br>\n第二段</p>"}, []string{"<pre"}},
		{"math is text", "$a||b$", []string{"$a"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CookShort(c.src).HTML
			for _, w := range c.want {
				if !strings.Contains(got, w) {
					t.Errorf("want %q in %q", w, got)
				}
			}
			for _, b := range c.bad {
				if strings.Contains(got, b) {
					t.Errorf("must not contain %q: %q", b, got)
				}
			}
		})
	}
}

func TestCookShortText(t *testing.T) {
	cases := []struct {
		name, src, text string
		spoiler         bool
	}{
		{"plain lines kept", "第一行\n第二行", "第一行\n第二行", false},
		{"paragraphs", "a\n\n\n\nb", "a\n\nb", false},
		{"inline spoiler masked", "结局是||全员幸存||。", "结局是███。", true},
		{"block spoiler masked", "前\n\n:::spoiler\n真相\n\n还有\n:::\n\n后", "前\n\n███\n\n后", true},
		{"spoiler inside quote", "> 他||死了||", "他███", true},
		{"list items", "- a\n- b", "a\nb", false},
		{"code kept verbatim", "```\n  x\n  y\n```", "  x\n  y", false},
		{"markup dropped", "**粗** [站](https://e.com) <i>", "粗 站 <i>", false},
		{"entities decoded", "a & b < c", "a & b < c", false},
		{"pipes alone are no spoiler", "a | b || c", "a | b || c", false},
		{"empty", "", "", false},
		{"blank", "  \n ", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CookShort(c.src)
			if got.Text != c.text {
				t.Errorf("text: want %q, got %q (html %q)", c.text, got.Text, got.HTML)
			}
			if got.HasSpoiler != c.spoiler {
				t.Errorf("has_spoiler: want %v, got %v", c.spoiler, got.HasSpoiler)
			}
			if strings.Contains(got.Text, "全员幸存") || strings.Contains(got.Text, "真相") || strings.Contains(got.Text, "死了") {
				t.Errorf("spoiler leaked into text: %q", got.Text)
			}
		})
	}
}

func TestCookShortEmptyIsEmpty(t *testing.T) {
	got := CookShort("")
	if got.HTML != "" || got.Text != "" || got.HasSpoiler || got.Version != ShortVersion {
		t.Fatalf("empty source: %+v", got)
	}
}

func TestCookShortSurvivesDeepNesting(t *testing.T) {
	for _, src := range []string{
		strings.Repeat(">", 1999) + "x",
		strings.Repeat("- ", 999) + "x",
		strings.Repeat("> - ", 499) + "x",
		strings.Repeat("*", 999) + "x" + strings.Repeat("*", 999),
		strings.Repeat("_", 999) + "x" + strings.Repeat("_", 999),
	} {
		c := CookShort(src)
		if !strings.Contains(c.Text, "x") {
			t.Errorf("%q…: text lost its content: %q", src[:8], c.Text)
		}
		if n := strings.Count(c.HTML, "<blockquote>") + strings.Count(c.HTML, "<li>"); n > 2*maxNesting {
			t.Errorf("%q…: %d nested containers", src[:8], n)
		}
	}
	q := strings.Repeat("> ", 40)
	c := CookShort(q + "||秘密||\n\n" + q + ":::spoiler\n" + q + "真相\n" + q + ":::")
	if strings.Contains(c.Text, "秘密") || strings.Contains(c.Text, "真相") || !c.HasSpoiler {
		t.Fatalf("flattening must keep spoilers closed: %+v", c)
	}
}

func TestCookShortStaysWithinTheDeclaredLengths(t *testing.T) {
	const htmlMax, textMax = 24000, 4000
	units := []string{"||a|| ", "||&|| ", "||\"\"|| ", "||*a*|| ", "www.a.co ", "http://a.b ", "&", "*a* ", "- a\n", "> - ||a||\n",
		":::spoiler\n", ":::spoiler\na\n:::\n", "`a` ", "~~a~~ ", "1. a\n", "> a\n- b\n"}
	for _, u := range units {
		src := strings.Repeat(u, 2000/utf8.RuneCountInString(u))
		c := CookShort(src)
		if n := utf8.RuneCountInString(c.HTML); n > htmlMax {
			t.Errorf("%q: html %d > %d", u, n, htmlMax)
		}
		if n := utf8.RuneCountInString(c.Text); n > textMax {
			t.Errorf("%q: text %d > %d", u, n, textMax)
		}
	}
}
