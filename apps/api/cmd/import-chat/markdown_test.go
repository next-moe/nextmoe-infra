package main

import (
	"reflect"
	"testing"

	"api/internal/platform/chat/content"
)

func TestFromMarkdown(t *testing.T) {
	url := "https://moyu.moe/p/1"
	lang := "go"
	for name, c := range map[string]struct {
		in     string
		text   string
		ents   []content.Entity
		images []legacyImage
	}{
		"plain": {in: "你好 world", text: "你好 world"},
		"image only": {in: "![sticker](/image/abc)", text: "",
			images: []legacyImage{{Alt: "sticker", URL: "/image/abc"}}},
		"text around images": {in: "看 ![](/image/a) 和 ![image.png](/image/b) 完", text: "看  和  完",
			images: []legacyImage{{URL: "/image/a"}, {Alt: "image.png", URL: "/image/b"}}},
		"link": {in: "去 [补丁](https://moyu.moe/p/1) 看", text: "去 补丁 看",
			ents: []content.Entity{{Type: content.TypeTextLink, Offset: 2, Length: 2, URL: &url}}},
		"emphasis": {in: "**粗** __斜__ ~~删~~ ||剧透|| `code`", text: "粗 斜 删 剧透 code",
			ents: []content.Entity{
				{Type: content.TypeBold, Offset: 0, Length: 1},
				{Type: content.TypeItalic, Offset: 2, Length: 1},
				{Type: content.TypeStrikethrough, Offset: 4, Length: 1},
				{Type: content.TypeSpoiler, Offset: 6, Length: 2},
				{Type: content.TypeCode, Offset: 9, Length: 4},
			}},
		"fence": {in: "前\n```go\nfmt.Println(\"**x**\")\n```\n后", text: "前\nfmt.Println(\"**x**\")\n后",
			ents: []content.Entity{{Type: content.TypePre, Offset: 2, Length: 20, Language: &lang}}},
		"emoji offsets": {in: "😀 **好**", text: "😀 好",
			ents: []content.Entity{{Type: content.TypeBold, Offset: 3, Length: 1}}},
		"unclosed stays literal": {in: "a ** b", text: "a ** b"},
	} {
		text, ents, images := fromMarkdown(c.in)
		if text != c.text || !reflect.DeepEqual(ents, c.ents) || !reflect.DeepEqual(images, c.images) {
			t.Errorf("%s:\n  text %q want %q\n  ents %+v want %+v\n  images %+v want %+v", name, text, c.text, ents, c.ents, images, c.images)
			continue
		}
		if _, _, err := content.Normalize(text, ents); err != nil && text != "" {
			t.Errorf("%s: the converted message must pass chat's own validation: %v", name, err)
		}
	}
}
