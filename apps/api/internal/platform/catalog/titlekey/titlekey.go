package titlekey

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const minAlnum = 3

func Loose(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func Keys(s string) []string {
	a, b := Loose(s), Loose(Strip(s))
	var out []string
	if utf8.RuneCountInString(a) >= minAlnum {
		out = append(out, a)
	}
	if utf8.RuneCountInString(b) >= minAlnum && b != a {
		out = append(out, b)
	}
	return out
}

func alnumRunes(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			n++
		}
	}
	return n
}
