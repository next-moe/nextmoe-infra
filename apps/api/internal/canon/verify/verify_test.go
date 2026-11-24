package verify

import (
	"encoding/json"
	"testing"

	"api/internal/canon/registry"
)

func prop(shape registry.Shape, vocab string) registry.Property {
	return registry.Property{Shape: shape, Vocab: vocab}
}

func TestValueShapes(t *testing.T) {
	cases := []struct {
		shape registry.Shape
		vocab string
		raw   string
		want  any
		code  string
	}{
		{registry.ShapeISBN13, "", `"978-4-04-916467-1"`, "9784049164671", ""},
		{registry.ShapeISBN13, "", `"9784049164672"`, nil, "VALUE_NOT_ENTAILED"},
		{registry.ShapePartialDate, "", `"2025-08"`, "2025-08", ""},
		{registry.ShapePartialDate, "", `"2025/08/10"`, nil, "VALUE_NOT_ENTAILED"},
		{registry.ShapeVocab, "format", `"paperback"`, "paperback", ""},
		{registry.ShapeVocab, "format", `"scroll"`, nil, "VOCAB_TERM_UNKNOWN"},
		{registry.ShapeLang, "", `"ja"`, "ja", ""},
		{registry.ShapeLang, "", `"zh-Hant-TW"`, "zh-Hant-TW", ""},
		{registry.ShapeMoney, "", `"924"`, "924", ""},
		{registry.ShapeMoney, "", `"-1"`, nil, "VALUE_NOT_ENTAILED"},
		{registry.ShapeRef, "", `"ref:w1"`, "ref:w1", ""},
		{registry.ShapeRef, "", `42`, int64(42), ""},
		{registry.ShapeURL, "", `"ftp://x"`, nil, "VALUE_NOT_ENTAILED"},
	}
	for _, c := range cases {
		got, err := Value(prop(c.shape, c.vocab), json.RawMessage(c.raw))
		if c.code != "" {
			if err == nil || err.Code != c.code {
				t.Errorf("%s %s: want %s, got %v %v", c.shape, c.raw, c.code, got, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("%s %s: want %v, got %v %v", c.shape, c.raw, c.want, got, err)
		}
	}
}

func TestRecordFieldsMatchWithoutPageMarks(t *testing.T) {
	if !Matches(prop(registry.ShapeOrdinal, ""), 28.0, "28") || Matches(prop(registry.ShapeOrdinal, ""), 28.0, "27") {
		t.Error("ordinal against a bare field")
	}
	if !Matches(prop(registry.ShapePartialDate, ""), "2025-08", "2025.8") {
		t.Error("NDL month date")
	}
	if !Matches(prop(registry.ShapeISBN13, ""), "9784049164671", "978-4-04-916467-1") {
		t.Error("hyphenated ISBN field")
	}
	if !Matches(prop(registry.ShapeVocab, "format"), "paperback", "電撃文庫 ; 4347") {
		t.Error("format from a label series statement")
	}
}

func TestEntailment(t *testing.T) {
	page := Excerpt{URL: "https://www.kadokawa.co.jp/product/322508000515/", Lines: []string{
		"著者：七菜 なな　イラスト：Parum",
		"定価: 924円（本体840円+税）",
		"発売日：2027年01月08日",
		"判型：文庫判　ページ数：320",
		"ISBN：978-4-04-916833-4",
		"電撃文庫",
	}}
	yes := []struct {
		p registry.Property
		v any
	}{
		{prop(registry.ShapePersonName, ""), "七菜なな"},
		{prop(registry.ShapePartialDate, ""), "2027-01-08"},
		{prop(registry.ShapePartialDate, ""), "2027-01"},
		{prop(registry.ShapeISBN13, ""), "9784049168334"},
		{prop(registry.ShapeMoney, ""), "924"},
		{prop(registry.ShapeInteger, ""), 320},
		{prop(registry.ShapeVocab, "format"), "paperback"},
		{prop(registry.ShapeVocab, "book_role"), "illustrator"},
		{prop(registry.ShapeCurrency, ""), "JPY"},
		{prop(registry.ShapeVocab, "price_basis"), "tax_excluded"},
		{prop(registry.ShapeVocab, "title_kind"), "official"},
		{prop(registry.ShapeURL, ""), "https://www.kadokawa.co.jp/product/322508000515/"},
	}
	for _, c := range yes {
		if !Entails(c.p, c.v, page) {
			t.Errorf("%s %v should be entailed", c.p.Shape, c.v)
		}
	}
	no := []struct {
		p registry.Property
		v any
	}{
		{prop(registry.ShapePartialDate, ""), "2027-01-09"},
		{prop(registry.ShapeMoney, ""), "320"},
		{prop(registry.ShapeInteger, ""), 924},
		{prop(registry.ShapeVocab, "format"), "digital"},
		{prop(registry.ShapePersonName, ""), "川原礫"},
		{prop(registry.ShapePartialDate, ""), "unknown"},
	}
	for _, c := range no {
		if Entails(c.p, c.v, page) {
			t.Errorf("%s %v must not be entailed", c.p.Shape, c.v)
		}
	}
}

func TestARoleWordStandsAlone(t *testing.T) {
	role := registry.Property{Shape: registry.ShapeVocab, Vocab: "book_role"}
	for _, c := range []struct {
		line, term string
		want       bool
	}{
		{"著者 タンバ", "author", true},
		{"イラスト／りっくす", "illustrator", true},
		{"作家：十文字青", "author", true},
		{"著・文・その他", "author", true},
		{`"auth_role_hyouji": "著"`, "author", true},
		{"原作：川原礫", "original_work", true},
		{"原作：川原礫", "author", false},
		{"企画・原案：X", "illustrator", false},
		{"作品紹介", "author", false},
		{"挿画集", "illustrator", false},
		{"キャラクター原案：X", "character_design", true},
	} {
		if got := Entails(role, c.term, Excerpt{Lines: []string{c.line}}); got != c.want {
			t.Errorf("%q as %s: %v", c.line, c.term, got)
		}
	}
	format := registry.Property{Shape: registry.ShapeVocab, Vocab: "format"}
	if !Entails(format, "paperback", Excerpt{Lines: []string{"電撃文庫 ; 4347"}}) {
		t.Error("a 文庫 label is a paperback's page word")
	}
}

func TestARefNeedsTheTargetsOwnName(t *testing.T) {
	page := func(l string) Excerpt { return Excerpt{Lines: []string{l}} }
	for _, c := range []struct {
		names  []string
		line   string
		record bool
		want   bool
	}{
		{[]string{"電撃文庫"}, "レーベル 電撃文庫", false, true},
		{[]string{"電撃文庫"}, "電撃文庫MAGAZINE", false, false},
		{[]string{"MF文庫J"}, "ＭＦ文庫Ｊ", true, true},
		{[]string{"ファンタジア文庫", "富士見ファンタジア文庫"}, "富士見ファンタジア文庫", true, true},
		{[]string{"ガガガブックス"}, "ガガガブックスf", true, false},
		{[]string{"川原 礫"}, "著者 川原礫", false, true},
		{[]string{"川原"}, "著者 川原礫", false, false},
		{[]string{""}, "著者 川原礫", false, false},
	} {
		if got := Names(c.names, page(c.line), c.record); got != c.want {
			t.Errorf("%v on %q (record %v): %v", c.names, c.line, c.record, got)
		}
	}
}
