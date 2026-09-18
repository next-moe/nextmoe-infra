package titlekey

import (
	"slices"
	"testing"
)

func TestStrip(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"【マルチエンディングSLG】寝取られお嬢様を監禁再調教!?", "寝取られお嬢様を監禁再調教!?"},
		{"野外学習 Windows10対応廉価版", "野外学習"},
		{"ファイナルデイズレガシー【分岐なしADV】(Win/Mac/Android)", "ファイナルデイズレガシー"},
		{"Resonance (English version)", "Resonance"},
		{"【推しの子】", "【推しの子】"},
		{"【スマホ版】スライムバスター・リミテッド", "スライムバスター・リミテッド"},
		{"幻月のパンドオラ Best Price版", "幻月のパンドオラ"},
		{"姫恋~縛羽の欠片~ v2.0", "姫恋~縛羽の欠片"},
		{"悪夢 ～青い果実の散花～ HDワイドスクリーン版", "悪夢 ~青い果実の散花"},
		{"Ver.2", "Ver.2"},
		{"[Android Ver.] Forte Girl (English version)", "Forte Girl"},
		{"夜想曲 前編", "夜想曲"},
		{"ヨナヨナ", "ヨナヨナ"},
		{"Starry☆Sky完全版", "Starry☆Sky"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := Strip(tc.in); got != tc.want {
				t.Fatalf("Strip(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLooseFoldsWidthDashesAndCase(t *testing.T) {
	a := Loose("淫虐の学園 ～猥堕に蔓延る復讐の罠～")
	b := Loose("淫虐の学園 〜猥堕に蔓延る復讐の罠〜")
	if a != b {
		t.Fatalf("Loose folded the wave-dash pair to %q and %q", a, b)
	}
	c := Loose("ＡＢＣ")
	d := Loose("abc")
	if c != d {
		t.Fatalf("Loose folded width/case to %q and %q", c, d)
	}
	if c != "abc" {
		t.Fatalf("Loose(ＡＢＣ) = %q, want abc", c)
	}
}

func TestKeysDropShortKeys(t *testing.T) {
	if got := Keys("A B"); len(got) != 0 {
		t.Fatalf("Keys(%q) = %q, want none", "A B", got)
	}
	got := Keys("ヨナヨナ")
	if len(got) != 1 {
		t.Fatalf("Keys(%q) = %q, want one key", "ヨナヨナ", got)
	}
}

func TestKeysLooseFirstAndDistinct(t *testing.T) {
	in := "【スマホ版】スライムバスター・リミテッド"
	got := Keys(in)
	want := []string{
		"スマホ版スライムバスターリミテッド",
		"スライムバスターリミテッド",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Keys(%q) = %q, want %q", in, got, want)
	}
}

func TestStripSkipsBracketBelowMinAlnum(t *testing.T) {
	in := "【推しの子】"
	if got := Strip(in); got != in {
		t.Fatalf("Strip(%q) = %q, want the brackets kept", in, got)
	}
}

func TestStripSkipsBanBelowMinAlnum(t *testing.T) {
	in := "AB 対応版"
	if got := Strip(in); got != in {
		t.Fatalf("Strip(%q) = %q, want the 版 suffix kept", in, got)
	}
}

func TestStripSkipsEditionBelowMinAlnum(t *testing.T) {
	in := "AB 前編"
	if got := Strip(in); got != in {
		t.Fatalf("Strip(%q) = %q, want the edition phrase kept", in, got)
	}
}

func TestStripSkipsTrimBelowMinAlnum(t *testing.T) {
	in := "~AB~"
	if got := Strip(in); got != in {
		t.Fatalf("Strip(%q) = %q, want the trimmed edges kept", in, got)
	}
}
