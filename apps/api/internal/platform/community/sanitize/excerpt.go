package sanitize

import (
	"bytes"
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

	hiddenTags = []string{"details", "script", "style", "template", "noscript", "img"}
	// Classes the sites' CSS hides; any class is allowed through, so raw HTML
	// can use them.
	hiddenClasses = []string{"hidden", "invisible", "sr-only", "text-transparent"}
	blockTags     = []string{"p", "br", "div", "li", "tr", "td", "th", "pre", "blockquote", "hr",
		"h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "table"}
)

// PlainText is a post's Markdown as the text a reader sees without opening
// anything: every spoiler becomes SpoilerMask, collapsed blocks, images and
// CSS-hidden text are left out, and whitespace collapses to single spaces. The
// source is rendered untouched — editing it first (stripping a "> 回复"
// header) moved lines out of the quote holding a spoiler, and leaked them — and
// the HTML is read as a browser builds it (html.Parse), so a stray end tag
// cannot end a spoiler early.
func PlainText(markdown string) string {
	var buf bytes.Buffer
	if err := excerptMarkdown.Convert([]byte(markdown), &buf); err != nil {
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
			if isSpoiler(n) || n.Data == "details" {
				b.WriteString(" " + SpoilerMask + " ")
				return
			}
			if hasHiddenClass(n) {
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

func classes(n *html.Node) []string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return strings.Fields(a.Val)
		}
	}
	return nil
}

func isSpoiler(n *html.Node) bool {
	return slices.ContainsFunc(classes(n), func(c string) bool { return strings.HasPrefix(c, spoilerClass) })
}

func hasHiddenClass(n *html.Node) bool {
	return slices.ContainsFunc(classes(n), func(c string) bool { return slices.Contains(hiddenClasses, c) })
}
