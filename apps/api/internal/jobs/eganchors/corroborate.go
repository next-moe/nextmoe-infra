package eganchors

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const titlePrefixMinRunes = 4

// titleKey must match titleKey in apps/api/internal/platform/catalog/llmsuggest/queue_refs_corroborate.go;
// the two will be merged into one package later and must not import across.
func titleKey(s string) string {
	var b strings.Builder
	for _, r := range norm.NFKC.String(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func corroborate(g egGame, workID int64, snap snapshot) string {
	if titlesCorroborate(g.Gamename, snap.workTitles[workID]) {
		return "title"
	}
	if datesCorroborate(g.Sellday, snap.workDates[workID]) {
		return "date"
	}
	return ""
}

func titlesCorroborate(gamename string, titles []string) bool {
	egKey := titleKey(gamename)
	if egKey == "" {
		return false
	}
	for _, t := range titles {
		wk := titleKey(t)
		if wk == "" {
			continue
		}
		if titleKeysCorroborate(egKey, wk) {
			return true
		}
	}
	return false
}

func titleKeysCorroborate(a, b string) bool {
	if a == b {
		return true
	}
	if len([]rune(a)) < titlePrefixMinRunes || len([]rune(b)) < titlePrefixMinRunes {
		return false
	}
	return strings.HasPrefix(a, b) || strings.HasPrefix(b, a)
}

func datesCorroborate(sellday string, days []string) bool {
	if sellday == "" {
		return false
	}
	for _, d := range days {
		if d == sellday {
			return true
		}
	}
	return false
}
