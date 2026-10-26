package osengine

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"
)

const kwStripPunct = "=☆★×✕～〜・♪♭♯†‡*|:/+#@!?&-'’‘.,、。~()[]「」『』【】〈〉《》\"“”　 "

var kwStripRunes = func() []rune {
	seen := map[rune]bool{}
	var out []rune
	add := func(r rune) {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	for _, r := range kwStripPunct {
		add(r)
		if r > ' ' && r <= '~' {
			add(r + 0xFEE0)
		}
	}
	return out
}()

func kwStripped(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune(string(kwStripRunes), r)
}

func kwStripPattern() string {
	var b strings.Builder
	b.WriteByte('[')
	for _, r := range kwStripRunes {
		if strings.ContainsRune(`\[]^-&`, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	b.WriteString(`\s]`)
	return b.String()
}

func CompactKeyword(text string) string {
	var b strings.Builder
	for _, r := range norm.NFC.String(text) {
		if !kwStripped(r) {
			b.WriteRune(r)
		}
	}
	return foldLatinMarks(width.Fold.String(strings.ToLower(b.String())))
}

func foldLatinMarks(s string) string {
	if isASCII(s) {
		return s
	}
	var b strings.Builder
	var base rune
	for _, r := range norm.NFD.String(s) {
		if unicode.Is(unicode.Mn, r) && base != 0 && base < 0x250 {
			continue
		}
		base = r
		b.WriteRune(r)
	}
	return norm.NFC.String(b.String())
}
