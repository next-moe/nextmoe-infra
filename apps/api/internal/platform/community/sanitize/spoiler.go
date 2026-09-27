package sanitize

import (
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// The sites' Markdown spoiler syntax, ported from the forum's
// internal/infrastructure/markdown/spoiler.go (moyu carries the same file).
// Community renders only excerpts with it; a divergence from the sites'
// parser is what leaks a spoiler into the feed, so change them together.

const spoilerClass = "kun-spoiler"

type spoilerInline struct{ ast.BaseInline }

var kindSpoilerInline = ast.NewNodeKind("KunSpoilerInline")

func (n *spoilerInline) Kind() ast.NodeKind { return kindSpoilerInline }

func (n *spoilerInline) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type spoilerBlock struct {
	ast.BaseBlock
	fence int
}

var kindSpoilerBlock = ast.NewNodeKind("KunSpoilerBlock")

func (n *spoilerBlock) Kind() ast.NodeKind { return kindSpoilerBlock }

func (n *spoilerBlock) Dump(source []byte, level int) { ast.DumpHelper(n, source, level, nil, nil) }

type spoilerDelimiterProcessor struct{}

func (p *spoilerDelimiterProcessor) IsDelimiter(b byte) bool { return b == '|' }

func (p *spoilerDelimiterProcessor) CanOpenCloser(opener, closer *parser.Delimiter) bool {
	return opener.Char == closer.Char
}

func (p *spoilerDelimiterProcessor) OnMatch(consumes int) ast.Node { return &spoilerInline{} }

var defaultSpoilerDelimiterProcessor = &spoilerDelimiterProcessor{}

type spoilerInlineParser struct{}

func (s *spoilerInlineParser) Trigger() []byte { return []byte{'|'} }

func (s *spoilerInlineParser) Parse(parent ast.Node, block text.Reader, pc parser.Context) ast.Node {
	line, segment := block.PeekLine()
	run := 0
	for run < len(line) && line[run] == '|' {
		run++
	}
	if run != 2 {
		return nil
	}
	node := parser.NewDelimiter(true, true, run, '|', defaultSpoilerDelimiterProcessor)
	node.Segment = segment.WithStop(segment.Start + run)
	block.Advance(run)
	pc.PushDelimiter(node)
	return node
}

type spoilerBlockParser struct{}

func (b *spoilerBlockParser) Trigger() []byte { return []byte{':'} }

func (b *spoilerBlockParser) Open(parent ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, _ := reader.PeekLine()
	fence, name := matchSpoilerFence(line, reader.LineOffset())
	if fence == 0 || name != "spoiler" {
		return nil, parser.NoChildren
	}
	reader.AdvanceToEOL()
	return &spoilerBlock{fence: fence}, parser.HasChildren
}

func (b *spoilerBlockParser) Continue(node ast.Node, reader text.Reader, pc parser.Context) parser.State {
	line, _ := reader.PeekLine()
	fence, name := matchSpoilerFence(line, reader.LineOffset())
	if name == "" && fence >= node.(*spoilerBlock).fence {
		reader.AdvanceToEOL()
		return parser.Close
	}
	return parser.Continue | parser.HasChildren
}

func (b *spoilerBlockParser) Close(node ast.Node, reader text.Reader, pc parser.Context) {}

func (b *spoilerBlockParser) CanInterruptParagraph() bool { return true }

func (b *spoilerBlockParser) CanAcceptIndentedLine() bool { return false }

func matchSpoilerFence(line []byte, lineOffset int) (int, string) {
	w, pos := util.IndentWidth(line, lineOffset)
	if w > 3 || pos >= len(line) {
		return 0, ""
	}
	end := pos
	for end < len(line) && line[end] == ':' {
		end++
	}
	if end-pos < 3 {
		return 0, ""
	}
	return end - pos, strings.TrimRight(string(line[end:]), " \t\r\n")
}

type spoilerRenderer struct{}

func (r *spoilerRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindSpoilerInline, r.render("span"))
	reg.Register(kindSpoilerBlock, r.render("div"))
}

func (r *spoilerRenderer) render(tag string) renderer.NodeRendererFunc {
	return func(w util.BufWriter, source []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if entering {
			_, _ = w.WriteString("<" + tag + ` class="` + spoilerClass + `">`)
		} else {
			_, _ = w.WriteString("</" + tag + ">")
		}
		return ast.WalkContinue, nil
	}
}

type spoilerExtension struct{}

func (e *spoilerExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(
		parser.WithBlockParsers(util.Prioritized(&spoilerBlockParser{}, 100)),
		parser.WithInlineParsers(util.Prioritized(&spoilerInlineParser{}, 500)),
	)
	m.Renderer().AddOptions(renderer.WithNodeRenderers(util.Prioritized(&spoilerRenderer{}, 500)))
}
