package textsafety

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	terminators = "。！？!?…\n"
	closers     = "」』）)】]〉》\"'”’"
)

// splitSentences cuts s into segments whose concatenation is exactly s; each
// segment carries its own terminator, closing brackets and trailing space.
func splitSentences(s string) []string {
	var out []string
	rs := []rune(s)
	start := 0
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		cut := strings.ContainsRune(terminators, r)
		if r == '.' && (i+1 == len(rs) || unicode.IsSpace(rs[i+1])) {
			cut = true
		}
		if !cut {
			continue
		}
		j := i + 1
		for j < len(rs) && (strings.ContainsRune(terminators, rs[j]) || strings.ContainsRune(closers, rs[j]) || unicode.IsSpace(rs[j])) {
			j++
		}
		out = append(out, string(rs[start:j]))
		start, i = j, j-1
	}
	if start < len(rs) {
		out = append(out, string(rs[start:]))
	}
	return out
}

func numberedText(segs []string) string {
	var b strings.Builder
	for i, seg := range segs {
		if t := strings.TrimSpace(seg); t != "" {
			fmt.Fprintf(&b, "[%d] %s\n", i+1, t)
		}
	}
	return b.String()
}

var blankRun = regexp.MustCompile(`\n{3,}`)

// redact drops the 1-based segment numbers in drop and reports how many runes
// of non-space text survived.
func redact(segs []string, drop []int) (string, int) {
	skip := make(map[int]bool, len(drop))
	for _, n := range drop {
		skip[n-1] = true
	}
	var b strings.Builder
	for i, seg := range segs {
		if !skip[i] {
			b.WriteString(seg)
		}
	}
	text := strings.TrimSpace(blankRun.ReplaceAllString(b.String(), "\n\n"))
	return text, visibleRunes(text)
}

func visibleRunes(s string) int {
	n := 0
	for _, r := range s {
		if !unicode.IsSpace(r) {
			n++
		}
	}
	return n
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }
