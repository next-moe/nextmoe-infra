package sanitize

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

const SpoilerMask = "███"

var (
	// Unsafe on purpose: a raw <span class="kun-spoiler"> is a spoiler on the
	// sites, so it has to reach the tokenizer below to be dropped. This output
	// is only ever read for its text.
	excerptMarkdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM, &spoilerExtension{}),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)

	referenceTokenPattern = regexp.MustCompile(`\[[@#][^\]]*\]\([a-z0-9-]+:\d+\)`)
	replyHeaderPattern    = regexp.MustCompile(`(?m)^\s*>\s*回复\s*`)

	voidTags   = []string{"area", "base", "br", "col", "embed", "hr", "img", "input", "link", "meta", "source", "track", "wbr"}
	hiddenTags = []string{"details", "script", "style", "template"}
)

// PlainText is a post's Markdown as the text a reader sees without opening
// anything: every spoiler, collapsed block and image is left out (a spoiler
// as SpoilerMask), mention and quote tokens are dropped, and whitespace is
// collapsed to single spaces.
func PlainText(markdown string) string {
	src := referenceTokenPattern.ReplaceAllString(markdown, "")
	src = replyHeaderPattern.ReplaceAllString(src, "")
	var buf bytes.Buffer
	if err := excerptMarkdown.Convert([]byte(src), &buf); err != nil {
		return ""
	}

	z := html.NewTokenizer(&buf)
	var b strings.Builder
	var open []string
	hideAt := -1
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return strings.Join(strings.Fields(b.String()), " ")
		case html.TextToken:
			if hideAt < 0 {
				b.Write(z.Text())
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			tag := string(name)
			if tt == html.SelfClosingTagToken || slices.Contains(voidTags, tag) {
				b.WriteByte(' ')
				continue
			}
			open = append(open, tag)
			if hideAt < 0 && (slices.Contains(hiddenTags, tag) || (hasAttr && hasSpoilerClass(z))) {
				hideAt = len(open) - 1
				if tag != "script" && tag != "style" {
					b.WriteString(" " + SpoilerMask + " ")
				}
			}
			b.WriteByte(' ')
		case html.EndTagToken:
			name, _ := z.TagName()
			for i := len(open) - 1; i >= 0; i-- {
				if open[i] == string(name) {
					open = open[:i]
					if hideAt >= len(open) {
						hideAt = -1
					}
					break
				}
			}
			b.WriteByte(' ')
		}
	}
}

func hasSpoilerClass(z *html.Tokenizer) bool {
	for {
		key, val, more := z.TagAttr()
		if string(key) == "class" && strings.Contains(string(val), spoilerClass) {
			return true
		}
		if !more {
			return false
		}
	}
}
