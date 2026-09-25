package service

import (
	"regexp"
	"strings"
)

var (
	traitURLTag   = regexp.MustCompile(`(?is)\[url=[^\]]*\](.*?)\[/url\]`)
	traitBBCode   = regexp.MustCompile(`\[/?[a-zA-Z]+(?:=[^\]]*)?\]`)
	traitNewlines = regexp.MustCompile(`\n{3,}`)
)

func SplitTraitAliases(raw string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, part := range strings.Split(raw, "\n") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func PlainTraitDescription(s string) string {
	s = traitURLTag.ReplaceAllString(s, "$1")
	s = traitBBCode.ReplaceAllString(s, "")
	s = traitNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}
