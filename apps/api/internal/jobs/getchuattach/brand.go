package getchuattach

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/titlekey"
)

func rungEGBrandDate(it item, snap snapshot, st *Stats) (plannedAction, bool) {
	// Same EG brand + related title, hold-out 2026-09-18: same day right 4,856 /
	// wrong 19, within 31 days 713 / 4 (a DL edition dated two weeks after the
	// package), within a year 357 / 17, beyond 515 / 43 — the last two stay
	// unattached. Series prefixes (今日のおかず …) and censor runes (○/〇) are
	// the mass of the hits the title rungs miss.
	day, err := parseGetchuDate(it.ReleaseDate)
	if err != nil {
		return plannedAction{}, false
	}
	sameDay, near := map[int64]struct{}{}, map[int64]struct{}{}
	for _, g := range sameBrandRelatedGames(it, snap) {
		sell, err := time.Parse("2006-01-02", g.Sellday)
		if err != nil {
			continue
		}
		diff := day.Sub(sell)
		if diff < 0 {
			diff = -diff
		}
		dst := near
		switch {
		case diff == 0:
			dst = sameDay
		case diff > nearWindow:
			continue
		}
		for _, w := range uniqueIDs(snap.egWorks[g.ID]) {
			dst[w] = struct{}{}
		}
	}
	rule, set := ruleEGBrand, sameDay
	if len(sameDay) == 0 {
		rule, set = ruleEGBrandNear, near
	}
	hits := make([]int64, 0, len(set))
	for w := range set {
		hits = append(hits, w)
	}
	hits = withoutRejected(hits, it.GetchuID, snap, st)
	if len(hits) != 1 {
		return plannedAction{}, false
	}
	if rule == ruleEGBrand {
		st.EGBrand++
	} else {
		st.EGBrandNear++
	}
	st.Attached++
	return attachOn(it, hits[0], rule, snap), true
}

func sameBrandRelatedGames(it item, snap snapshot) []egGame {
	seen := map[int64]struct{}{}
	var out []egGame
	for k := range getchuBrandKeys(it.Brand) {
		for _, bid := range snap.egBrandByKey[k] {
			if _, dup := seen[bid]; dup {
				continue
			}
			seen[bid] = struct{}{}
			for _, g := range snap.egByBrand[bid] {
				if relatedTitle(it.Title, g.Gamename) {
					out = append(out, g)
				}
			}
		}
	}
	return out
}

// sharesAdultEGBrand holds when the Getchu brand is an EG brand with at least
// one 18+ title. Getchu's 18+ notice also sits on CERO Z console and PC games
// (Call of Duty, F.E.A.R. 2, the P/ECE handheld) whose EG brands list no 18+
// title; 14 of 16,212 anchored adult items fail this, measured 2026-09-18.
func sharesAdultEGBrand(it item, snap snapshot) bool {
	for k := range getchuBrandKeys(it.Brand) {
		for _, bid := range snap.egBrandByKey[k] {
			for _, g := range snap.egByBrand[bid] {
				if g.Erogame {
					return true
				}
			}
		}
	}
	return false
}

// anyBrandEGEdition finds an EG game of any brand that the item is another
// edition of. EG lists じぃすぽっと's 今日のおかず shorts under
// モニスタラッシュ, dated days to months before Getchu's DL edition: 53 of
// the 128 mints planned on 2026-09-18 were such editions.
func anyBrandEGEdition(it item, snap snapshot) bool {
	lt := titlekey.Loose(it.Title)
	lb := titlekey.Loose(BaseTitle(it.Title))
	for _, g := range snap.egGames {
		k := titlekey.Loose(g.Gamename)
		if runeCount(k) >= minEditionRunes && strings.Contains(lt, k) {
			return true
		}
		if runeCount(lb) >= minEditionRunes && strings.Contains(k, lb) {
			return true
		}
		if censorMatch(it.Title, g.Gamename) {
			return true
		}
	}
	return false
}

func uniqueEGBrand(it item, snap snapshot) (int64, bool) {
	seen := map[int64]struct{}{}
	var hits []int64
	for k := range getchuBrandKeys(it.Brand) {
		for _, id := range snap.egBrandByKey[k] {
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			hits = append(hits, id)
		}
	}
	if len(hits) != 1 {
		return 0, false
	}
	return hits[0], true
}

func egBrandKeys(name, furigana string) map[string]struct{} {
	out := map[string]struct{}{}
	addLoose(out, name)
	addLoose(out, stripTrailingParen(name))
	addLoose(out, furigana)
	return out
}

func getchuBrandKeys(brand string) map[string]struct{} {
	out := map[string]struct{}{}
	s := strings.ReplaceAll(brand, "／", "/")
	for _, part := range strings.Split(s, "/") {
		part = strings.TrimSpace(part)
		if part == "" || utf8.RuneCountInString(part) < 2 {
			continue
		}
		addLoose(out, stripTrailingParen(part))
	}
	return out
}

func addLoose(out map[string]struct{}, s string) {
	k := titlekey.Loose(s)
	if k != "" {
		out[k] = struct{}{}
	}
}

func stripTrailingParen(s string) string {
	s = strings.TrimSpace(s)
	runes := []rune(s)
	if len(runes) < 3 {
		return s
	}
	close := runes[len(runes)-1]
	var open rune
	switch close {
	case ')':
		open = '('
	case '）':
		open = '（'
	default:
		return s
	}
	for i := len(runes) - 2; i >= 0; i-- {
		if runes[i] == open {
			return strings.TrimSpace(string(runes[:i]))
		}
	}
	return s
}

func brandKeysOverlap(a, b map[string]struct{}) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	for k := range a {
		if _, ok := b[k]; ok {
			return true
		}
	}
	return false
}

func relatedTitle(itemTitle, egName string) bool {
	a, b := titlekey.Loose(itemTitle), titlekey.Loose(egName)
	if runeCount(a) >= 2 && runeCount(b) >= 2 && (strings.Contains(a, b) || strings.Contains(b, a)) {
		return true
	}
	return censorMatch(itemTitle, egName)
}

// censorMatch compares the item title with an EG title rune by rune
// over the shorter of the two, ignoring spacing, case and punctuation, a
// censor rune in the item matching any one rune: 〇眠〇漢Episode1 is EG's
// 催眠痴漢Episode1, and NEW BREEDER～…自分好みに○○！～ is EG's
// New Breeder 〜…自分好みに調教！〜.
func censorMatch(itemTitle, egName string) bool {
	item := []rune(censorKey(itemTitle))
	eg := []rune(titlekey.Loose(egName))
	n := len(item)
	if len(eg) < n {
		n = len(eg)
	}
	if n < minCensorRunes {
		return false
	}
	censored := false
	for i := 0; i < n; i++ {
		if item[i] == censorRune {
			censored = true
			continue
		}
		if item[i] != eg[i] {
			return false
		}
	}
	return censored
}

const censorRune = '○'

func censorKey(s string) string {
	var b strings.Builder
	for _, r := range nfkc(s) {
		switch {
		case isCensor(r):
			b.WriteRune(censorRune)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}
