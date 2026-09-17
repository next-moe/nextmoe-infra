package orglabels

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var corporateDesignator = regexp.MustCompile(
	`株式会社|有限会社|合同会社|合資会社|合名会社|\((?:株|有|合)\)|` +
		`\bco\b\.?,?\s*\bltd\b\.?|\binc\b\.?|\bllc\b|\bltd\b\.?|\bcorporation\b|\bcorp\b\.?`)

// looseLabelKey takes a name already folded by NFKC and lower-case, the form
// display_name_norm and the loaders carry.
func looseLabelKey(norm string) string {
	var b strings.Builder
	for _, r := range corporateDesignator.ReplaceAllString(norm, " ") {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	if utf8.RuneCountInString(b.String()) < 2 {
		return ""
	}
	return b.String()
}

func looseIndex(norms map[string][]int64) map[string][]int64 {
	out := make(map[string][]int64, len(norms))
	for n, ids := range norms {
		k := looseLabelKey(n)
		if k == "" {
			continue
		}
		for _, id := range ids {
			if !slices.Contains(out[k], id) {
				out[k] = append(out[k], id)
			}
		}
	}
	return out
}

func looseKeys(o *orgRec) []string {
	var out []string
	for _, n := range o.nameNorms {
		if k := looseLabelKey(n); k != "" && !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out
}

func looseHits(o *orgRec, index map[string][]int64) []int64 {
	var out []int64
	for _, k := range looseKeys(o) {
		for _, id := range index[k] {
			if !slices.Contains(out, id) {
				out = append(out, id)
			}
		}
	}
	slices.Sort(out)
	return out
}

type mintPlan struct {
	exts map[string]bool
	keys map[string]bool
}

func newMintPlan() *mintPlan {
	return &mintPlan{exts: map[string]bool{}, keys: map[string]bool{}}
}

func mintExt(source int16, extID string) string {
	return srcKey(source) + "/" + extID
}

func (m *mintPlan) taken(source int16, o *orgRec) bool {
	if m.exts[mintExt(source, o.extID)] {
		return true
	}
	return slices.ContainsFunc(looseKeys(o), func(k string) bool { return m.keys[k] })
}

func (m *mintPlan) add(source int16, o *orgRec) {
	m.exts[mintExt(source, o.extID)] = true
	for _, k := range looseKeys(o) {
		m.keys[k] = true
	}
}
