package main

import (
	"regexp"
	"strings"

	"api/internal/platform/chat/content"
)

type legacyImage struct {
	Alt string
	URL string
}

var (
	fencePattern  = regexp.MustCompile("(?s)```([A-Za-z0-9+#._-]{0,32})\\n?(.*?)```")
	inlinePattern = regexp.MustCompile(`!\[([^\]\n]*)\]\(([^)\s]+)\)|\[([^\]\n]+)\]\((https?://[^)\s]+)\)|` +
		"`([^`\\n]+)`" + `|\*\*([^*\n]+?)\*\*|__([^_\n]+?)__|~~([^~\n]+?)~~|\|\|([^|\n]+?)\|\|`)
)

// fromMarkdown turns the markdown the old kungal and moyu chats stored into
// chat's text + entities. Embedded images leave the text and come back as a
// list; the caller sends them as photo messages.
func fromMarkdown(md string) (string, []content.Entity, []legacyImage) {
	var (
		b      strings.Builder
		ents   []content.Entity
		images []legacyImage
		units  int
	)
	write := func(s string) {
		b.WriteString(s)
		units += content.UTF16Len(s)
	}
	span := func(kind, s string, extra func(*content.Entity)) {
		if s == "" {
			return
		}
		e := content.Entity{Type: kind, Offset: units, Length: content.UTF16Len(s)}
		if extra != nil {
			extra(&e)
		}
		write(s)
		ents = append(ents, e)
	}
	inline := func(s string) {
		last := 0
		for _, m := range inlinePattern.FindAllStringSubmatchIndex(s, -1) {
			write(s[last:m[0]])
			last = m[1]
			group := func(i int) string {
				if m[2*i] < 0 {
					return ""
				}
				return s[m[2*i]:m[2*i+1]]
			}
			switch {
			case m[4] >= 0:
				images = append(images, legacyImage{Alt: group(1), URL: group(2)})
			case m[6] >= 0:
				url := group(4)
				span(content.TypeTextLink, group(3), func(e *content.Entity) { e.URL = &url })
			case m[10] >= 0:
				span(content.TypeCode, group(5), nil)
			case m[12] >= 0:
				span(content.TypeBold, group(6), nil)
			case m[14] >= 0:
				span(content.TypeItalic, group(7), nil)
			case m[16] >= 0:
				span(content.TypeStrikethrough, group(8), nil)
			case m[18] >= 0:
				span(content.TypeSpoiler, group(9), nil)
			}
		}
		write(s[last:])
	}
	last := 0
	for _, m := range fencePattern.FindAllStringSubmatchIndex(md, -1) {
		inline(md[last:m[0]])
		last = m[1]
		code := strings.TrimSuffix(md[m[4]:m[5]], "\n")
		if lang := md[m[2]:m[3]]; lang != "" {
			span(content.TypePre, code, func(e *content.Entity) { e.Language = &lang })
		} else {
			span(content.TypePre, code, nil)
		}
	}
	inline(md[last:])
	return b.String(), ents, images
}
