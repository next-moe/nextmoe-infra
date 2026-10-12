package osengine

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/platform/catalog/search/spec"
)

const kwStripPunct = "=＝☆★×✕～〜・♪♭♯†‡＊*|:/+#@!?&＆-"

func CompactKeyword(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if unicode.IsSpace(r) || strings.ContainsRune(kwStripPunct, r) {
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

func isCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF)
}

func isHanRune(r rune) bool {
	return r >= 0x4E00 && r <= 0x9FFF
}

func isCJK(s string) bool {
	for _, r := range s {
		if isCJKRune(r) {
			return true
		}
	}
	return false
}

func isASCII(s string) bool {
	for _, r := range s {
		if r >= 0x80 {
			return false
		}
	}
	return true
}

func latinOnly(text, kw string) bool {
	return kw != "" && isASCII(kw) && !isCJK(text)
}

func hanChars(text string) []string {
	seen := make(map[rune]struct{})
	var out []string
	for _, r := range text {
		if !isHanRune(r) {
			continue
		}
		if _, ok := seen[r]; ok {
			continue
		}
		seen[r] = struct{}{}
		out = append(out, string(r))
	}
	return out
}

func fuzzyDistance(kw string) (int, bool) {
	if !isCJK(kw) || utf8.RuneCountInString(kw) < 3 {
		return 0, false
	}
	return 2, true
}

func hanOverlapClause(kw string) any {
	chars := hanChars(kw)
	n := len(chars)
	if n < 3 {
		return nil
	}
	msm := n
	if n >= 6 {
		msm = n - 1
	}
	should := make([]any, 0, n)
	for _, ch := range chars {
		should = append(should, map[string]any{
			"term": map[string]any{"titles_ngram": ch},
		})
	}
	return map[string]any{
		"bool": map[string]any{
			"should":               should,
			"minimum_should_match": msm,
			"boost":                1.5,
		},
	}
}

func precisionClauses(text string, ngram bool) []any {
	clauses := []any{
		map[string]any{"match_phrase": map[string]any{"titles_ja": map[string]any{"query": text, "slop": 1, "boost": 6}}},
		map[string]any{"match_phrase": map[string]any{"titles_zh": map[string]any{"query": text, "slop": 1, "boost": 6}}},
		map[string]any{"match_phrase": map[string]any{"titles": map[string]any{"query": text, "slop": 1, "boost": 5}}},
		map[string]any{"match": map[string]any{"titles_ja": map[string]any{"query": text, "operator": "and", "boost": 4}}},
		map[string]any{"match": map[string]any{"titles_zh": map[string]any{"query": text, "operator": "and", "boost": 4}}},
		map[string]any{"match": map[string]any{"titles": map[string]any{"query": text, "operator": "and", "boost": 3}}},
	}
	if ngram {
		clauses = append(clauses, map[string]any{
			"match": map[string]any{"titles_ngram": map[string]any{"query": text, "operator": "and", "boost": 3}},
		})
	}
	return clauses
}

func titleShould(text string, searchIntro, entity bool) []any {
	var should []any
	kw := CompactKeyword(text)
	latin := latinOnly(text, kw)
	if kw != "" {
		should = append(should, map[string]any{
			"term": map[string]any{"titles_kw": map[string]any{"value": kw, "boost": 14}},
		})
		if latin {
			should = append(should, map[string]any{
				"prefix": map[string]any{"titles_pinyin": map[string]any{"value": kw, "boost": 12}},
			})
		}
	}
	should = append(should, precisionClauses(text, !latin)...)
	if !entity && !latin {
		if fuzz, ok := fuzzyDistance(kw); ok {
			should = append(should, map[string]any{
				"fuzzy": map[string]any{"titles_kw": map[string]any{
					"value":          kw,
					"fuzziness":      fuzz,
					"max_expansions": 80,
					"boost":          8,
				}},
			})
		}
		if overlap := hanOverlapClause(kw); overlap != nil {
			should = append(should, overlap)
		}
	}
	if searchIntro {
		should = append(should,
			map[string]any{"match": map[string]any{"intro_ja": map[string]any{"query": text, "operator": "and", "boost": 0.6}}},
			map[string]any{"match": map[string]any{"intro_zh": map[string]any{"query": text, "operator": "and", "boost": 0.6}}},
			map[string]any{"match": map[string]any{"intro_other": map[string]any{"query": text, "operator": "and", "boost": 0.4}}},
		)
	}
	return should
}

func searchBody(text string, limit int, searchIntro, entity bool) map[string]any {
	if text == "" {
		return map[string]any{
			"query":            map[string]any{"match_all": map[string]any{}},
			"sort":             []any{map[string]any{"popularity": map[string]any{"order": "desc"}}},
			"size":             limit,
			"track_total_hits": true,
		}
	}
	return map[string]any{
		"query": map[string]any{
			"bool": map[string]any{
				"should":               titleShould(text, searchIntro, entity),
				"minimum_should_match": 1,
			},
		},
		"sort":             []any{"_score", map[string]any{"popularity": map[string]any{"order": "desc"}}},
		"size":             limit,
		"track_total_hits": true,
	}
}

var popularityRanked = map[string]bool{
	IndexLabels:  true,
	IndexEngines: true,
	IndexSeries:  true,
}

const exactNameTier = 1e6

func popularityRankedQuery(text string, filters []any) map[string]any {
	should := titleShould(text, false, true)
	if kw := CompactKeyword(text); kw != "" {
		should = append(should, map[string]any{
			"constant_score": map[string]any{
				"filter": map[string]any{"term": map[string]any{"titles_kw": kw}},
				"boost":  exactNameTier,
			},
		})
	}
	return map[string]any{
		"function_score": map[string]any{
			"query": shouldQuery(should, filters),
			"functions": []any{
				map[string]any{"weight": 1},
				map[string]any{"field_value_factor": map[string]any{"field": "popularity", "missing": 0}},
			},
			"score_mode": "sum",
			"boost_mode": "multiply",
		},
	}
}

func WorksBody(q string, limit int, searchIntro bool) map[string]any {
	return searchBody(spec.SanitizeQuery(q), limit, searchIntro, false)
}

func EntityBody(q string, limit int) map[string]any {
	return searchBody(spec.SanitizeQuery(q), limit, false, true)
}
