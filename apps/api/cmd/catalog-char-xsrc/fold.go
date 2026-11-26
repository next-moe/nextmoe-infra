package main

import (
	"regexp"
	"strings"

	"api/internal/platform/catalog/charname"

	"golang.org/x/text/unicode/norm"
)

func nameSegments(s string) []string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	f := func(r rune) bool {
		switch r {
		case ' ', '\t', '　', '・', '･', '·', '•', '=', '＝':
			return true
		}
		return false
	}
	var out []string
	for _, seg := range strings.FieldsFunc(s, f) {
		var b strings.Builder
		for _, r := range seg {
			r = charname.Itaiji(r)
			b.WriteRune(r)
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	return out
}

func splitScripts(s string) (cjk, ascii string) {
	var c, a strings.Builder
	for _, r := range s {
		if r <= 127 {
			a.WriteRune(r)
		} else {
			c.WriteRune(r)
		}
	}
	return c.String(), a.String()
}

func lcsRunes(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	if len(ra) == 0 || len(rb) == 0 {
		return 0
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	best := 0
	for i := 1; i <= len(ra); i++ {
		for j := 1; j <= len(rb); j++ {
			if ra[i-1] == rb[j-1] {
				cur[j] = prev[j-1] + 1
				if cur[j] > best {
					best = cur[j]
				}
			} else {
				cur[j] = 0
			}
		}
		prev, cur = cur, prev
	}
	return best
}

func namesSimilar(aNames, bNames []string) bool {
	for _, an := range aNames {
		aSegs := nameSegments(an)
		aFold := charname.Fold(an)
		for _, bn := range bNames {
			for _, as := range aSegs {
				if len([]rune(as)) < 2 {
					continue
				}
				for _, bs := range nameSegments(bn) {
					if as == bs {
						return true
					}
				}
			}
			bFold := charname.Fold(bn)
			if aFold == "" || bFold == "" {
				continue
			}
			aCJ, aA := splitScripts(aFold)
			bCJ, bA := splitScripts(bFold)
			need := 3
			if len([]rune(aCJ)) <= 4 || len([]rune(bCJ)) <= 4 {
				need = 2
			}
			if aCJ != "" && bCJ != "" && lcsRunes(aCJ, bCJ) >= need {
				return true
			}
			if len(aA) >= 4 && len(bA) >= 4 && lcsRunes(aA, bA) >= 4 {
				return true
			}
		}
	}
	return false
}

var qualifierRe = regexp.MustCompile(`[(（【\[［][^)）】\]］]+[)）】\]］]`)

// qualifiedPair reports whether either display name carries a bracketed
// qualifier the other name lacks — 「リップ（少女編）」, 「グリム(ヴィルヘルム)」.
// ErogameScape files a form, period or personality of a character as its own
// entry this way, and on 2026-09-17 deepseek merged such an entry into the
// whole character at 0.98 even with a prompt rule naming its sibling, because
// the two intros describe the same person. Such a pair never merges
// automatically.
func qualifiedPair(a, b string) bool {
	fa, fb := charname.Fold(a), charname.Fold(b)
	for _, side := range [][2]string{{a, fb}, {b, fa}} {
		for _, q := range qualifierRe.FindAllString(side[0], -1) {
			rs := []rune(q)
			inner := charname.Fold(string(rs[1 : len(rs)-1]))
			if inner != "" && !strings.Contains(side[1], inner) {
				return true
			}
		}
	}
	return false
}
