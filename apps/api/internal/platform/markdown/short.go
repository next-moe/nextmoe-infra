package markdown

import (
	"bytes"
	"regexp"
	"slices"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// ShortVersion is stored with every cooked short text. Bump it whenever
// CookShort's output changes for any input: the catalog migration re-cooks
// every row whose version differs.
const ShortVersion = 1

type Short struct {
	HTML       string
	Text       string
	HasSpoiler bool
	Version    int
}

var (
	shortMarkdown = goldmark.New(
		goldmark.WithParser(parser.NewParser(
			parser.WithBlockParsers(
				util.Prioritized(parser.NewListParser(), 300),
				util.Prioritized(parser.NewListItemParser(), 400),
				util.Prioritized(parser.NewFencedCodeBlockParser(), 700),
				util.Prioritized(parser.NewBlockquoteParser(), 800),
				util.Prioritized(indentedParagraphParser{parser.NewParagraphParser()}, 1000),
			),
			parser.WithInlineParsers(
				util.Prioritized(parser.NewCodeSpanParser(), 100),
				util.Prioritized(parser.NewLinkParser(), 200),
				util.Prioritized(parser.NewAutoLinkParser(), 300),
				util.Prioritized(parser.NewEmphasisParser(), 500),
			),
			parser.WithParagraphTransformers(parser.DefaultParagraphTransformers()...),
			parser.WithASTTransformers(util.Prioritized(nestingLimit{}, 100)),
		)),
		goldmark.WithExtensions(extension.Strikethrough, extension.Linkify, Spoiler),
		goldmark.WithRendererOptions(
			gmhtml.WithHardWraps(),
			renderer.WithNodeRenderers(util.Prioritized(&shortRenderer{}, 100)),
		),
	)

	shortPolicy = func() *bluemonday.Policy {
		p := bluemonday.NewPolicy()
		p.AllowElements("p", "br", "em", "strong", "del", "code", "pre", "ul", "ol", "li", "blockquote")
		p.AllowAttrs("start").Matching(bluemonday.Integer).OnElements("ol")
		p.AllowAttrs("class").Matching(regexp.MustCompile(`^language-[\w+#.-]+$`)).OnElements("code")
		p.AllowAttrs("class").Matching(regexp.MustCompile(`^`+spoilerClassAttr+`$`)).OnElements("span", "div")
		p.AllowAttrs("href").OnElements("a")
		p.AllowAttrs("rel").Matching(regexp.MustCompile(`^nofollow ugc$`)).OnElements("a")
		p.AllowURLSchemes("http", "https")
		p.RequireParseableURLs(true)
		return p
	}()

	shortBlocks = []string{"p", "pre", "blockquote", "ul", "ol", "div"}
)

// CookShort renders a short text such as a rating's review in the NextMoe
// dialect without headings, tables, images, math or raw HTML, every newline a
// hard break. Indented code blocks are left out too: legacy plain-text reviews
// indent paragraphs with spaces, and 18 of them came out as <pre>.
func CookShort(source string) Short {
	out := Short{Version: ShortVersion}
	if strings.TrimSpace(source) == "" {
		return out
	}
	var buf bytes.Buffer
	if err := shortMarkdown.Convert([]byte(source), &buf); err != nil {
		buf.Reset()
		buf.WriteString("<p>" + html.EscapeString(source) + "</p>")
	}
	out.HTML = strings.TrimSpace(shortPolicy.Sanitize(buf.String()))
	out.Text, out.HasSpoiler = shortText(out.HTML)
	return out
}

func shortText(cooked string) (string, bool) {
	nodes, err := html.ParseFragment(strings.NewReader(cooked), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return "", false
	}
	var w textWriter
	spoiler := false
	var walk func(n *html.Node, pre bool)
	walk = func(n *html.Node, pre bool) {
		switch n.Type {
		case html.TextNode:
			w.text(n.Data, pre)
			return
		case html.ElementNode:
			switch {
			case slices.ContainsFunc(classes(n), func(c string) bool { return c == SpoilerClass }):
				spoiler = true
				if n.Data == "div" {
					w.block()
					w.text(SpoilerMask, true)
					w.block()
				} else {
					w.text(SpoilerMask, true)
				}
				return
			case n.Data == "br":
				w.newline()
				return
			case n.Data == "li":
				w.endLine()
				defer w.endLine()
			case slices.Contains(shortBlocks, n.Data):
				w.block()
				defer w.block()
			}
			pre = pre || n.Data == "pre"
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, pre)
		}
	}
	for _, n := range nodes {
		walk(n, false)
	}
	return strings.TrimRight(strings.TrimLeft(w.String(), "\n"), " \n"), spoiler
}

func classes(n *html.Node) []string {
	for _, a := range n.Attr {
		if a.Key == "class" {
			return strings.Fields(a.Val)
		}
	}
	return nil
}

type textWriter struct{ strings.Builder }

var collapse = regexp.MustCompile(`\s+`)

func (w *textWriter) atLineStart() bool {
	s := w.String()
	return s == "" || strings.HasSuffix(s, "\n")
}

func (w *textWriter) text(s string, pre bool) {
	if !pre {
		s = collapse.ReplaceAllString(s, " ")
		if w.atLineStart() {
			s = strings.TrimLeft(s, " ")
		}
	}
	w.WriteString(s)
}

func (w *textWriter) trimTrailingSpace() {
	s := w.String()
	if t := strings.TrimRight(s, " "); len(t) != len(s) {
		w.Reset()
		w.WriteString(t)
	}
}

func (w *textWriter) newline() {
	w.trimTrailingSpace()
	w.WriteByte('\n')
}

func (w *textWriter) endLine() {
	w.trimTrailingSpace()
	if !w.atLineStart() {
		w.WriteByte('\n')
	}
}

func (w *textWriter) block() {
	w.trimTrailingSpace()
	s := w.String()
	switch {
	case s == "", strings.HasSuffix(s, "\n\n"):
	case strings.HasSuffix(s, "\n"):
		w.WriteByte('\n')
	default:
		w.WriteString("\n\n")
	}
}

// x/net/html refuses a document nested deeper than 512 elements, and a
// review of 600 '>' cooked to an empty short_summary_text. Quotes and lists
// past maxNesting are unwrapped into their parent; a spoiler block never is,
// since that would show what it hides.
const maxNesting = 16

type nestingLimit struct{}

func (nestingLimit) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	limitNesting(doc, 0)
}

func limitNesting(n ast.Node, depth int) {
	for c := n.FirstChild(); c != nil; {
		next := c.NextSibling()
		switch c.Kind() {
		case ast.KindBlockquote, ast.KindList, ast.KindListItem:
			if depth < maxNesting {
				limitNesting(c, depth+1)
				break
			}
			limitNesting(c, depth)
			for g := c.FirstChild(); g != nil; {
				gn := g.NextSibling()
				n.InsertBefore(n, c, g)
				g = gn
			}
			n.RemoveChild(n, c)
		case kindSpoilerBlock:
			limitNesting(c, depth+1)
		}
		c = next
	}
}

// With no indented code block parser, goldmark offers a line indented four
// spaces only to parsers that accept indented lines, and the paragraph parser
// does not: such a line was dropped from the output.
type indentedParagraphParser struct{ parser.BlockParser }

func (indentedParagraphParser) CanAcceptIndentedLine() bool { return true }

type shortRenderer struct{}

func (r *shortRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindLink, r.link)
	reg.Register(ast.KindAutoLink, r.autoLink)
	reg.Register(ast.KindImage, r.image)
}

var webURL = regexp.MustCompile(`(?i)^https?://`)

func openLink(w util.BufWriter, dest []byte) {
	_, _ = w.WriteString(`<a href="`)
	_, _ = w.Write(util.EscapeHTML(util.URLEscape(dest, true)))
	_, _ = w.WriteString(`" rel="nofollow ugc">`)
}

func (r *shortRenderer) link(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*ast.Link)
	if !webURL.Match(n.Destination) {
		return ast.WalkContinue, nil
	}
	if entering {
		openLink(w, n.Destination)
	} else {
		_, _ = w.WriteString("</a>")
	}
	return ast.WalkContinue, nil
}

func (r *shortRenderer) autoLink(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*ast.AutoLink)
	label := n.Label(source)
	dest := n.URL(source)
	if n.AutoLinkType == ast.AutoLinkURL && bytes.HasPrefix(bytes.ToLower(dest), []byte("www.")) {
		dest = append([]byte("http://"), dest...)
	}
	if n.AutoLinkType != ast.AutoLinkURL || !webURL.Match(dest) {
		_, _ = w.Write(util.EscapeHTML(label))
		return ast.WalkContinue, nil
	}
	openLink(w, dest)
	_, _ = w.Write(util.EscapeHTML(label))
	_, _ = w.WriteString("</a>")
	return ast.WalkContinue, nil
}

func (r *shortRenderer) image(w util.BufWriter, source []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	_ = ast.Walk(node, func(c ast.Node, in bool) (ast.WalkStatus, error) {
		if !in {
			return ast.WalkContinue, nil
		}
		switch t := c.(type) {
		case *ast.Text:
			_, _ = w.Write(util.EscapeHTML(t.Segment.Value(source)))
			if t.HardLineBreak() || t.SoftLineBreak() {
				_ = w.WriteByte(' ')
			}
		case *ast.String:
			_, _ = w.Write(util.EscapeHTML(t.Value))
		}
		return ast.WalkContinue, nil
	})
	return ast.WalkSkipChildren, nil
}
