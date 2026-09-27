package sanitize

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	mathjax "github.com/litao91/goldmark-mathjax"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"golang.org/x/net/html"
)

const SpoilerMask = "███"

var (
	// The sites' rendering, step for step (the forum's markdown.go): GFM,
	// math and spoilers with raw HTML, then UGC sanitizing that keeps class.
	// Math matters to spoilers — || inside $…$ is not a delimiter there — and
	// the sanitizer drops what the site never shows, such as <noscript>.
	excerptMarkdown = goldmark.New(
		goldmark.WithExtensions(extension.GFM, mathjax.MathJax, &spoilerExtension{}),
		goldmark.WithRendererOptions(gmhtml.WithUnsafe()),
	)
	excerptPolicy = func() *bluemonday.Policy {
		p := bluemonday.UGCPolicy()
		p.AllowAttrs("class").Globally()
		return p
	}()

	referenceTokenPattern = regexp.MustCompile(`\[[@#][^\]]*\]\([a-z0-9-]+:\d+\)`)
	replyHeaderPattern    = regexp.MustCompile(`(?m)^\s*>\s*回复\s*`)

	hiddenTags = []string{"details", "script", "style", "template", "noscript", "img"}
	blockTags  = []string{"p", "br", "div", "li", "tr", "td", "th", "pre", "blockquote", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "table"}
)

// PlainText is a post's Markdown as the text a reader sees without opening
// anything: every spoiler becomes SpoilerMask, collapsed blocks and images are
// left out, mention and quote tokens are dropped, and whitespace collapses to
// single spaces. The HTML is read as a browser builds it (html.Parse), so a
// stray end tag cannot end a spoiler early.
func PlainText(markdown string) string {
	src := referenceTokenPattern.ReplaceAllString(markdown, "")
	src = replyHeaderPattern.ReplaceAllString(src, "")
	var buf bytes.Buffer
	if err := excerptMarkdown.Convert([]byte(src), &buf); err != nil {
		return ""
	}
	root, err := html.Parse(excerptPolicy.SanitizeReader(&buf))
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			b.WriteString(n.Data)
			return
		case html.ElementNode:
			if hasSpoilerClass(n) || n.Data == "details" {
				b.WriteString(" " + SpoilerMask + " ")
				return
			}
			if slices.Contains(hiddenTags, n.Data) {
				return
			}
			if slices.Contains(blockTags, n.Data) {
				defer b.WriteByte(' ')
				b.WriteByte(' ')
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(root)
	return strings.Join(strings.Fields(b.String()), " ")
}

func hasSpoilerClass(n *html.Node) bool {
	for _, a := range n.Attr {
		if a.Key == "class" && slices.Contains(strings.Fields(a.Val), spoilerClass) {
			return true
		}
	}
	return false
}
