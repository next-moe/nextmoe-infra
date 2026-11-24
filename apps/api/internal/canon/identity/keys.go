// Package identity answers "is this already in canon, or in the legacy
// catalog" (plan 06): the keys an entity is found by, kept in canon.identity_key,
// and the candidate search over them. A candidate is a question for a reviewer,
// never a conclusion.
package identity

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	KindTitle     = "title"
	KindName      = "name"
	KindAuthority = "authority"
)

var (
	trailingVolume = regexp.MustCompile(`^(.+?)第?[0-9]+[巻卷集]?$`)
	trailingRoman  = regexp.MustCompile(`^(.*[^a-z])[ivx]+$`)
	trailingPart   = regexp.MustCompile(`^(.+)[上中下]$`)
)

// TitleKeys is every key a title is found by: itself, itself without a
// trailing volume, and the part before NDL's " : " (other title information).
func TitleKeys(text string) []string {
	var out []string
	add := func(s string) {
		k := TitleKey(s)
		for _, c := range []string{k, trimmed(trailingVolume, k), trimmed(trailingRoman, k), trimmed(trailingPart, k)} {
			if c != "" && !slices.Contains(out, c) {
				out = append(out, c)
			}
		}
	}
	add(text)
	if head, _, ok := strings.Cut(norm.NFKC.String(text), " : "); ok {
		add(head)
	}
	return out
}

// TitleKey keeps letters and digits only, so spacing, punctuation, width and
// case never tell two spellings of one title apart.
func TitleKey(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, norm.NFKC.String(text))
}

// NameKey drops only case, width and whitespace: 「・」 can be part of a name.
func NameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(norm.NFKC.String(name)), ""))
}

func trimmed(re *regexp.Regexp, k string) string {
	m := re.FindStringSubmatch(k)
	if m == nil || len([]rune(m[1])) < 2 {
		return ""
	}
	return m[1]
}
