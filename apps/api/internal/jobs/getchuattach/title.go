package getchuattach

import (
	"strings"
	"unicode/utf8"

	"api/internal/platform/catalog/titlekey"

	"golang.org/x/text/unicode/norm"
)

func rungTitleDate(it item, snap snapshot, st *Stats) (plannedAction, bool) {
	hits := withoutRejected(titleHits(it, snap), it.GetchuID, snap, st)
	if len(hits) != 1 {
		return plannedAction{}, false
	}
	w := hits[0]
	if corroborate(it, w, snap) == "" {
		return plannedAction{}, false
	}
	st.TitleDate++
	st.Attached++
	return attachOn(it, w, ruleTitleDate, snap), true
}

func rungTitleCut(it item, snap snapshot, st *Stats) (plannedAction, bool) {
	// Hold-out 2026-09-18: one whitespace cut was right 52 / wrong 0; two or more
	// cuts 8 / 1. 133 unanchored items attach this way. Longest prefix first so
	// トリプルペアリング ミニファンディスク 初回版 meets the fan disc dated the
	// same day, never トリプルペアリング -Triple Pairing- dated months earlier.
	day := catalogDay(it.ReleaseDate)
	if day == "" {
		return plannedAction{}, false
	}
	for _, pfx := range cutPrefixes(it.Title) {
		hits := withoutRejected(prefixHits(pfx, snap), it.GetchuID, snap, st)
		if len(hits) != 1 {
			continue
		}
		w := hits[0]
		if !workHasDay(snap, w, day) {
			continue
		}
		st.TitleCut++
		st.Attached++
		return attachOn(it, w, ruleTitleCut, snap), true
	}
	return plannedAction{}, false
}

func prefixHits(title string, snap snapshot) []int64 {
	seen := map[int64]struct{}{}
	var hits []int64
	for _, k := range titlekey.Keys(title) {
		for _, w := range snap.titleIndex[k] {
			if _, dup := seen[w]; dup {
				continue
			}
			seen[w] = struct{}{}
			hits = append(hits, w)
		}
	}
	return hits
}

func workHasDay(snap snapshot, workID int64, day string) bool {
	for _, d := range snap.workDates[workID] {
		if d == day {
			return true
		}
	}
	return false
}

func splitWS(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\u3000'
	})
}

func cutPrefixes(title string) []string {
	toks := splitWS(title)
	if len(toks) < 2 {
		return nil
	}
	out := make([]string, 0, len(toks)-1)
	for n := len(toks) - 1; n >= 1; n-- {
		out = append(out, strings.Join(toks[:n], " "))
	}
	return out
}

func titlePrefixes(title string) []string {
	out := []string{title}
	out = append(out, cutPrefixes(title)...)
	return out
}

func BaseTitle(title string) string {
	s := dropTrailingBracket(strings.TrimSpace(title))
	toks := splitWS(s)
	for len(toks) >= 2 && isEditionToken(toks[len(toks)-1]) {
		toks = toks[:len(toks)-1]
	}
	s = strings.TrimSpace(strings.Join(toks, " "))
	return strings.TrimSpace(dropTrailingBracket(s))
}

func isEditionToken(tok string) bool {
	n := nfkcFold(tok)
	for _, s := range []string{"版", "限定", "初回", "通常", "豪華", "特典", "特装", "予約", "同梱", "付き", "パッケージ"} {
		if strings.Contains(n, strings.ToLower(s)) {
			return true
		}
	}
	if strings.Contains(n, "edition") {
		return true
	}
	if n == "dx" {
		return true
	}
	runes := []rune(tok)
	return len(runes) > 0 && runes[len(runes)-1] == '付'
}

func dropTrailingBracket(s string) string {
	pairs := [][2]rune{
		{'【', '】'}, {'［', '］'}, {'[', ']'},
		{'＜', '＞'}, {'<', '>'}, {'（', '）'}, {'(', ')'},
	}
	runes := []rune(strings.TrimSpace(s))
	if len(runes) < 2 {
		return s
	}
	close := runes[len(runes)-1]
	var open rune
	ok := false
	for _, p := range pairs {
		if p[1] == close {
			open, ok = p[0], true
			break
		}
	}
	if !ok {
		return s
	}
	for i := len(runes) - 2; i >= 0; i-- {
		if runes[i] == open {
			inner := string(runes[i+1 : len(runes)-1])
			n := nfkcFold(inner)
			if strings.Contains(n, "版") || strings.Contains(n, "限定") || strings.Contains(n, "特典") ||
				strings.Contains(n, "げっちゅ屋") || strings.Contains(n, "getchu") {
				return strings.TrimSpace(string(runes[:i]))
			}
			return s
		}
	}
	return s
}

func firstWSToken(s string) string {
	toks := splitWS(s)
	if len(toks) == 0 {
		return strings.TrimSpace(s)
	}
	return toks[0]
}

func runeCount(s string) int { return utf8.RuneCountInString(s) }

func nfkc(s string) string { return norm.NFKC.String(s) }

func isCensor(r rune) bool {
	switch r {
	case '○', '◯', '〇', '●':
		return true
	default:
		return false
	}
}

func hasCensor(s string) bool {
	for _, r := range s {
		if isCensor(r) {
			return true
		}
	}
	return false
}
