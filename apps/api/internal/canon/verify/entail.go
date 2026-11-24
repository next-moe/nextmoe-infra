package verify

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"api/internal/canon/registry"

	"golang.org/x/text/unicode/norm"
)

// Excerpt is what evidence resolves to: the cited lines, and the page they
// came from (a snapshot URL, or a record's source).
type Excerpt struct {
	Lines []string
	URL   string
}

var (
	dateRe     = regexp.MustCompile(`([0-9]{4})\s*[年/.\-]\s*([0-9]{1,2})(?:\s*[月/.\-]\s*([0-9]{1,2})\s*日?)?`)
	priceMarks = []string{"円", "定価", "税込", "税抜", "本体", "価格", "¥", "￥", "元", "nt$", "售價", "定價", "价格"}
	unitMarks  = []string{"ページ", "頁", "页", "p.", "pp"}
	volMarks   = []string{"巻", "卷", "集", "第", "vol"}
	currencies = map[string][]string{
		"JPY": {"円", "¥", "￥"},
		"TWD": {"nt$", "新台幣", "元"},
		"CNY": {"元", "人民币", "¥", "￥"},
		"USD": {"$", "usd"},
	}
)

// Entails reports whether ex supports value for p. A shape with no textual
// form on a page (a language tag, a structural reference) is not checked here.
func Entails(p registry.Property, value any, ex Excerpt) bool {
	if len(ex.Lines) == 0 {
		return false
	}
	text := fold(strings.Join(ex.Lines, "\n"))
	switch p.Shape {
	case registry.ShapeText, registry.ShapeTitle, registry.ShapePersonName:
		return strings.Contains(squash(text), squash(fold(fmt.Sprint(value))))
	case registry.ShapePartialDate:
		v := fmt.Sprint(value)
		if v == "unknown" {
			return false
		}
		for _, d := range datesIn(text) {
			if d == v || strings.HasPrefix(d, v+"-") {
				return true
			}
		}
		return false
	case registry.ShapeISBN13, registry.ShapeJAN:
		for _, l := range ex.Lines {
			if strings.Contains(digits(l), fmt.Sprint(value)) {
				return true
			}
		}
		return false
	case registry.ShapeMoney:
		return numberNear(ex.Lines, strings.TrimSuffix(strings.TrimSuffix(fmt.Sprint(value), ".00"), ".0"), priceMarks)
	case registry.ShapeInteger:
		return numberNear(ex.Lines, fmt.Sprint(value), unitMarks)
	case registry.ShapeOrdinal:
		return numberNear(ex.Lines, strconv.FormatFloat(value.(float64), 'f', -1, 64), volMarks)
	case registry.ShapeVocab:
		v, ok := registry.VocabularyByName(p.Vocab)
		if !ok {
			return false
		}
		t := term(v, fmt.Sprint(value))
		if t == nil {
			return false
		}
		if len(t.EntailedBy) == 0 {
			// A term with no page words is a classification (an official
			// title, a standard edition); the source's grade vouches for it.
			return true
		}
		for _, k := range t.EntailedBy {
			if v.WholeWords && standsAlone(text, fold(k)) || !v.WholeWords && strings.Contains(text, fold(k)) {
				return true
			}
		}
		return false
	case registry.ShapeURL:
		return fmt.Sprint(value) == ex.URL || strings.Contains(text, fold(fmt.Sprint(value)))
	case registry.ShapeCurrency:
		for _, m := range currencies[fmt.Sprint(value)] {
			if strings.Contains(text, m) {
				return true
			}
		}
		return false
	case registry.ShapeBool:
		if value == true {
			return strings.Contains(text, "税込")
		}
		return strings.Contains(text, "税抜") || strings.Contains(text, "本体")
	case registry.ShapeLang, registry.ShapeRef:
		return true
	}
	return false
}

// Names reports whether ex carries one of a referenced entity's names. A
// record field is the name's own cell and must be it: as a substring,
// 「ガガガブックス」 was also read off 「ガガガブックスf」.
func Names(names []string, ex Excerpt, recordField bool) bool {
	text := fold(strings.Join(ex.Lines, "\n"))
	for _, n := range names {
		n = fold(n)
		if n == "" {
			continue
		}
		if recordField {
			if squash(text) == squash(n) {
				return true
			}
			continue
		}
		if standsAlone(text, n) {
			return true
		}
	}
	return false
}

// standsAlone finds word in text with no letter or digit against either end.
// Whitespace inside the word may differ, as 「川原 礫」 and 「川原礫」 do.
func standsAlone(text, word string) bool {
	var b strings.Builder
	for _, r := range squash(word) {
		if b.Len() > 0 {
			b.WriteString(`\s*`)
		}
		b.WriteString(regexp.QuoteMeta(string(r)))
	}
	re, err := regexp.Compile(b.String())
	if err != nil || b.Len() == 0 {
		return false
	}
	for from := 0; from < len(text); {
		m := re.FindStringIndex(text[from:])
		if m == nil {
			return false
		}
		start, end := from+m[0], from+m[1]
		before, _ := utf8.DecodeLastRuneInString(text[:start])
		after, _ := utf8.DecodeRuneInString(text[end:])
		if !wordRune(before) && !wordRune(after) {
			return true
		}
		_, n := utf8.DecodeRuneInString(text[start:])
		from = start + n
	}
	return false
}

func wordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.Is(unicode.Mn, r)
}

func fold(s string) string { return strings.ToLower(norm.NFKC.String(s)) }

func squash(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func datesIn(text string) []string {
	var out []string
	for _, m := range dateRe.FindAllStringSubmatch(text, -1) {
		mo, _ := strconv.Atoi(m[2])
		if mo < 1 || mo > 12 {
			continue
		}
		d := fmt.Sprintf("%s-%02d", m[1], mo)
		if m[3] != "" {
			day, _ := strconv.Atoi(m[3])
			d = fmt.Sprintf("%s-%02d", d, day)
		}
		out = append(out, d)
	}
	return out
}

var numberRe = regexp.MustCompile(`[0-9][0-9,]*(\.[0-9]+)?`)

// numberNear wants the number on a line that also carries one of the marks;
// the same digits elsewhere on the page do not count.
func numberNear(lines []string, want string, marks []string) bool {
	for _, l := range lines {
		f := fold(l)
		hasMark := false
		for _, m := range marks {
			if strings.Contains(f, fold(m)) {
				hasMark = true
				break
			}
		}
		if !hasMark {
			continue
		}
		for _, n := range numberRe.FindAllString(f, -1) {
			if strings.ReplaceAll(n, ",", "") == want {
				return true
			}
		}
	}
	return false
}

// Matches is Entails for a structured record field: the field is the value's
// own cell, so a number need not sit beside a unit mark.
func Matches(p registry.Property, value any, field string) bool {
	switch p.Shape {
	case registry.ShapeInteger, registry.ShapeOrdinal, registry.ShapeMoney:
		n := numberRe.FindString(fold(field))
		if n == "" {
			return false
		}
		got, err1 := strconv.ParseFloat(strings.ReplaceAll(n, ",", ""), 64)
		want, err2 := strconv.ParseFloat(fmt.Sprint(value), 64)
		return err1 == nil && err2 == nil && got == want
	case registry.ShapeISBN13, registry.ShapeJAN:
		return digits(field) == fmt.Sprint(value)
	}
	return Entails(p, value, Excerpt{Lines: []string{field}})
}
