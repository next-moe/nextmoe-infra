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
)

type foundCards struct {
	cards  []tools.Card
	lookup bool
}

func isLookup(tool string) bool {
	if _, name, ok := strings.Cut(tool, "__"); ok {
		tool = name
	}
	return strings.HasPrefix(tool, "get_")
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

// The catalog keeps one character per work it appears in, so one name in an
// answer matched three 阿万音鈴羽 cards, titled 阿万音铃羽 or 阿万音 鈴羽 but all
// "Amane Suzuha" in Japanese. Cards of one kind sharing any name show once,
// preferring a detail lookup's card, then one with a cover.
func sameNames(c tools.Card) []string {
	var d struct {
		Kind      string `json:"kind"`
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
	out := []string{"id\x00" + c.ID}
	for _, v := range names {
		if n := normalize(v); n != "" {
			out = append(out, d.Kind+"\x00"+n)
		}
	}
	return out
}

func cardRank(c tools.Card, lookup bool) int {
	var d struct {
		Cover json.RawMessage `json:"cover"`
	}
	_ = json.Unmarshal(c.Data, &d)
	rank := 0
	if lookup {
		rank += 2
	}
	if cover := string(d.Cover); cover != "" && cover != "null" && cover != `""` {
		rank++
	}
	return rank
}

// pickCards keeps the cards the answer names, in the order it names them. A
// search hands back every candidate it found, and showing all of them while
// the model was still working read as recommendations it never made.
func pickCards(found []foundCards, answer string) []tools.Card {
	text := normalize(answer)
	type hit struct {
		card tools.Card
		at   int
		rank int
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
				hits = append(hits, hit{c, at, cardRank(c, found[i].lookup)})
			}
		}
	}
	parent := make([]int, len(hits))
	var root func(int) int
	root = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	owner := map[string]int{}
	for i, h := range hits {
		parent[i] = i
		for _, n := range sameNames(h.card) {
			if j, ok := owner[n]; ok {
				parent[root(i)] = root(j)
			} else {
				owner[n] = i
			}
		}
	}
	best, first := map[int]int{}, map[int]int{}
	for i, h := range hits {
		r := root(i)
		if b, ok := best[r]; !ok || h.rank > hits[b].rank || (h.rank == hits[b].rank && h.at < hits[b].at) {
			best[r] = i
		}
		if f, ok := first[r]; !ok || h.at < f {
			first[r] = h.at
		}
	}
	kept := hits[:0:0]
	for i, h := range hits {
		if r := root(i); best[r] == i {
			h.at = first[r]
			kept = append(kept, h)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].at < kept[j].at })
	out := make([]tools.Card, 0, maxCards)
	for _, h := range kept {
		if len(out) == maxCards {
			break
		}
		out = append(out, h.card)
	}
	if len(out) > 0 {
		return out
	}
	for _, f := range found {
		if !f.lookup {
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
