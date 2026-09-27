package content

import (
	"reflect"
	"strings"
	"testing"
)

func ptr[T any](v T) *T { return &v }

func TestUTF16Len(t *testing.T) {
	for s, want := range map[string]int{"": 0, "abc": 3, "中文": 2, "😀": 2, "a😀b": 4} {
		if got := UTF16Len(s); got != want {
			t.Errorf("UTF16Len(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestNormalizeTrimsAndShiftsEntities(t *testing.T) {
	text, ents, err := Normalize("  hi 😀 there \n", []Entity{
		{Type: TypeBold, Offset: 2, Length: 2},
		{Type: TypeItalic, Offset: 0, Length: 4},
		{Type: TypeSpoiler, Offset: 13, Length: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hi 😀 there" {
		t.Fatalf("text = %q", text)
	}
	want := []Entity{
		{Type: TypeBold, Offset: 0, Length: 2},
		{Type: TypeItalic, Offset: 0, Length: 2},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities = %+v, want %+v", ents, want)
	}
}

func TestNormalizeRejects(t *testing.T) {
	cases := map[string]struct {
		text string
		ents []Entity
	}{
		"invalid utf8":       {"a\xffb", nil},
		"nul":                {"a\x00b", nil},
		"too long":           {strings.Repeat("😀", MaxTextUnits/2+1), nil},
		"unknown type":       {"abc", []Entity{{Type: "blink", Offset: 0, Length: 1}}},
		"past the end":       {"abc", []Entity{{Type: TypeBold, Offset: 1, Length: 5}}},
		"zero length":        {"abc", []Entity{{Type: TypeBold, Offset: 1, Length: 0}}},
		"splits surrogate":   {"a😀b", []Entity{{Type: TypeBold, Offset: 0, Length: 2}}},
		"partial overlap":    {"abcdef", []Entity{{Type: TypeBold, Offset: 0, Length: 3}, {Type: TypeItalic, Offset: 2, Length: 3}}},
		"inside code":        {"abcdef", []Entity{{Type: TypeCode, Offset: 0, Length: 6}, {Type: TypeBold, Offset: 1, Length: 2}}},
		"duplicate":          {"abcdef", []Entity{{Type: TypeBold, Offset: 0, Length: 3}, {Type: TypeBold, Offset: 0, Length: 3}}},
		"link without url":   {"abc", []Entity{{Type: TypeTextLink, Offset: 0, Length: 3}}},
		"javascript link":    {"abc", []Entity{{Type: TypeTextLink, Offset: 0, Length: 3, URL: ptr("javascript:alert(1)")}}},
		"mention without id": {"@a", []Entity{{Type: TypeMention, Offset: 0, Length: 2}}},
		"bad language":       {"x", []Entity{{Type: TypePre, Offset: 0, Length: 1, Language: ptr("go lang")}}},
	}
	for name, c := range cases {
		if _, _, err := Normalize(c.text, c.ents); err == nil {
			t.Errorf("%s: want an error", name)
		} else if _, ok := IsError(err); !ok {
			t.Errorf("%s: want a content error, got %T", name, err)
		}
	}
}

func TestNormalizeTooManyEntities(t *testing.T) {
	var ents []Entity
	for i := 0; i < MaxEntities+1; i++ {
		ents = append(ents, Entity{Type: TypeBold, Offset: i, Length: 1})
	}
	if _, _, err := Normalize(strings.Repeat("a", MaxEntities+1), ents); err == nil {
		t.Fatal("want an error past the entity cap")
	}
}

func TestNormalizeNestingAllowed(t *testing.T) {
	_, ents, err := Normalize("abcdef", []Entity{
		{Type: TypeBlockquote, Offset: 0, Length: 6},
		{Type: TypeBold, Offset: 0, Length: 6},
		{Type: TypeItalic, Offset: 1, Length: 2},
		{Type: TypeCode, Offset: 4, Length: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 4 {
		t.Fatalf("entities = %+v", ents)
	}
}

func TestNormalizeDetectsURLs(t *testing.T) {
	text, ents, err := Normalize("看 https://moyu.moe/patch/1, and www.kungal.com。done", nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ents {
		if e.Type != TypeURL {
			t.Fatalf("unexpected entity %+v", e)
		}
		s, _ := Slice(text, nil, e.Offset, e.end())
		got = append(got, s)
	}
	want := []string{"https://moyu.moe/patch/1", "www.kungal.com"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("urls = %q, want %q", got, want)
	}
	if !HasLink(ents) {
		t.Fatal("HasLink must see a detected url")
	}
}

func TestNormalizeDropsClientURLsAndSkipsCode(t *testing.T) {
	_, ents, err := Normalize("`https://a.example` https://b.example", []Entity{
		{Type: TypeURL, Offset: 0, Length: 3},
		{Type: TypeCode, Offset: 0, Length: 19},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []Entity{
		{Type: TypeCode, Offset: 0, Length: 19},
		{Type: TypeURL, Offset: 20, Length: 17},
	}
	if !reflect.DeepEqual(ents, want) {
		t.Fatalf("entities = %+v, want %+v", ents, want)
	}
}

func TestNormalizeURLInsideBold(t *testing.T) {
	_, ents, err := Normalize("x https://a.example y", []Entity{{Type: TypeBold, Offset: 0, Length: 21}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 || ents[1].Type != TypeURL || ents[1].Offset != 2 || ents[1].Length != 17 {
		t.Fatalf("entities = %+v", ents)
	}
}

func TestHasLinkAndMentions(t *testing.T) {
	ents := []Entity{
		{Type: TypeMention, Offset: 0, Length: 1, UserID: ptr(int64(7))},
		{Type: TypeMention, Offset: 2, Length: 1, UserID: ptr(int64(7))},
		{Type: TypeMention, Offset: 4, Length: 1, UserID: ptr(int64(9))},
	}
	if HasLink(ents) {
		t.Fatal("mentions are not links")
	}
	if got := MentionedUserIDs(ents); !reflect.DeepEqual(got, []int64{7, 9}) {
		t.Fatalf("mentions = %v", got)
	}
	if !HasLink([]Entity{{Type: TypeTextLink}}) {
		t.Fatal("a text_link is a link")
	}
}

func TestSliceAndPreview(t *testing.T) {
	text := "ab😀cd"
	got, ents := Slice(text, []Entity{{Type: TypeBold, Offset: 2, Length: 2}, {Type: TypeItalic, Offset: 0, Length: 5}}, 1, 5)
	if got != "b😀c" {
		t.Fatalf("slice = %q", got)
	}
	if len(ents) != 1 || ents[0].Offset != 1 {
		t.Fatalf("entities = %+v", ents)
	}
	if got, _ := Slice(text, nil, 0, 3); got != "ab" {
		t.Fatalf("a cut through a surrogate pair must fall back, got %q", got)
	}

	long := strings.Repeat("字", PreviewUnits+10)
	p, _ := Preview(long, nil)
	if UTF16Len(p) != PreviewUnits {
		t.Fatalf("preview length = %d", UTF16Len(p))
	}
}

func TestQuoteAt(t *testing.T) {
	text := "hello 😀 world"
	ents := []Entity{{Type: TypeBold, Offset: 9, Length: 5}}
	got, err := QuoteAt(text, ents, "world", 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Offset != 0 || got[0].Length != 5 {
		t.Fatalf("quote entities = %+v", got)
	}
	if _, err := QuoteAt(text, ents, "world", 8); err == nil {
		t.Fatal("a quote at the wrong offset must fail")
	}
	if _, err := QuoteAt(text, ents, "", 0); err == nil {
		t.Fatal("an empty quote must fail")
	}
}
