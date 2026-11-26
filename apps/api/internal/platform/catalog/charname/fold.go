package charname

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Fold is the character-name identity key shared by the cross-source fold and
// the importers that attach a new source record to a character already on the
// work: NFKC, lower case, no separators, itaiji folded to the common form.
func Fold(s string) string {
	s = norm.NFKC.String(s)
	s = strings.ToLower(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '　', '・', '･', '·', '•', '=', '＝':
			continue
		}
		r = Itaiji(r)
		b.WriteRune(r)
	}
	return b.String()
}

func Itaiji(r rune) rune {
	if v, ok := itaiji[r]; ok {
		return v
	}
	return r
}

var itaiji = map[rune]rune{
	'佛': '仏', '嶋': '島', '嶌': '島', '彥': '彦', '髙': '高', '﨑': '崎', '嵜': '崎',
	'澤': '沢', '邊': '辺', '邉': '辺', '齊': '斉', '齋': '斎', '龍': '竜', '國': '国',
	'櫻': '桜', '圓': '円', '廣': '広', '惠': '恵', '瀨': '瀬', '賴': '頼', '與': '与',
	'萬': '万', '亞': '亜', '榮': '栄', '實': '実', '壽': '寿', '濱': '浜', '彌': '弥',
	'靜': '静', '眞': '真', '樂': '楽', '鐵': '鉄', '藝': '芸', '會': '会', '學': '学',
	'內': '内', '黑': '黒', '德': '徳', '淺': '浅', '繪': '絵', '禮': '礼', '鹽': '塩',
}
