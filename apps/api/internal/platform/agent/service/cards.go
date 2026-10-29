package service

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/agent/tools"
)

const (
	maxCards         = 8
	maxFallbackCards = 4
	lookupSize       = 3
)

type foundCards struct {
	cards []tools.Card
}

func answerText(parts []Part) string {
	var b strings.Builder
	for _, p := range parts {
		if p.Type == "text" {
			b.WriteString(p.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func cardNames(c tools.Card) (loose, quoted []string) {
	var d struct {
		Title     string `json:"title"`
		Localized map[string]struct {
			Value string `json:"value"`
		} `json:"localized"`
	}
	_ = json.Unmarshal(c.Data, &d)
	names := []string{d.Title}
	for _, l := range d.Localized {
		names = append(names, l.Value)
	}
	for _, raw := range names {
		n := normalize(raw)
		switch {
		case n == "":
		case utf8.RuneCountInString(n) < 2 || (len(n) == utf8.RuneCountInString(n) && len(n) < 4):
			quoted = append(quoted, "《"+strings.TrimSpace(raw)+"》")
		default:
			loose = append(loose, n)
		}
	}
	return loose, quoted
}

func mentionAt(answer, text string, c tools.Card) int {
	loose, quoted := cardNames(c)
	at := -1
	for _, n := range loose {
		if i := strings.Index(text, n); i >= 0 && (at < 0 || i < at) {
			at = i
		}
	}
	for _, q := range quoted {
		if i := strings.Index(answer, q); i >= 0 {
			if j := len(normalize(answer[:i])); at < 0 || j < at {
				at = j
			}
		}
	}
	return at
}

// pickCards keeps the cards the answer names, in the order it names them. A
// search hands back every candidate it found, and showing all of them while
// the model was still working read as recommendations it never made.
func pickCards(found []foundCards, answer string) []tools.Card {
	text := normalize(answer)
	type hit struct {
		card tools.Card
		at   int
	}
	var hits []hit
	seen := map[string]bool{}
	for i := len(found) - 1; i >= 0; i-- {
		for _, c := range found[i].cards {
			if seen[c.ID] {
				continue
			}
			if at := mentionAt(answer, text, c); at >= 0 {
				seen[c.ID] = true
				hits = append(hits, hit{c, at})
			}
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].at < hits[j].at })
	out := make([]tools.Card, 0, maxCards)
	for _, h := range hits {
		if len(out) == maxCards {
			break
		}
		out = append(out, h.card)
	}
	if len(out) > 0 {
		return out
	}
	for _, f := range found {
		if len(f.cards) > lookupSize {
			continue
		}
		for _, c := range f.cards {
			if len(out) < maxFallbackCards && !seen[c.ID] {
				seen[c.ID] = true
				out = append(out, c)
			}
		}
	}
	return out
}
