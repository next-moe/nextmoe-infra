// Package content validates chat message text and its formatting entities.
// Offsets and lengths count UTF-16 code units, as in Telegram: both clients
// (JavaScript and Dart) index strings in UTF-16, so they need no conversion.
package content

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

const (
	MaxTextUnits    = 4096
	MaxEntities     = 100
	MaxURLLength    = 2048
	MaxLanguageLen  = 32
	PreviewUnits    = 120
	maxContextField = 200
)

const (
	TypeBold          = "bold"
	TypeItalic        = "italic"
	TypeUnderline     = "underline"
	TypeStrikethrough = "strikethrough"
	TypeSpoiler       = "spoiler"
	TypeCode          = "code"
	TypePre           = "pre"
	TypeBlockquote    = "blockquote"
	TypeTextLink      = "text_link"
	TypeMention       = "mention"
	TypeURL           = "url"
)

var manualTypes = map[string]bool{
	TypeBold: true, TypeItalic: true, TypeUnderline: true, TypeStrikethrough: true,
	TypeSpoiler: true, TypeCode: true, TypePre: true, TypeBlockquote: true,
	TypeTextLink: true, TypeMention: true,
}

type Entity struct {
	Type     string  `json:"type" enum:"bold,italic,underline,strikethrough,spoiler,code,pre,blockquote,text_link,mention,url"`
	Offset   int     `json:"offset" minimum:"0" doc:"start, in UTF-16 code units"`
	Length   int     `json:"length" minimum:"1" doc:"length, in UTF-16 code units"`
	UserID   *int64  `json:"user_id,omitempty" doc:"mention only: the mentioned user"`
	URL      *string `json:"url,omitempty" doc:"text_link only"`
	Language *string `json:"language,omitempty" doc:"pre only: the code block's language"`
}

func (e Entity) end() int { return e.Offset + e.Length }

type Error struct {
	Field  string
	Reason string
}

func (e *Error) Error() string { return e.Field + ": " + e.Reason }

func invalid(field, format string, args ...any) error {
	return &Error{Field: field, Reason: fmt.Sprintf(format, args...)}
}

func IsError(err error) (*Error, bool) {
	var ce *Error
	ok := errors.As(err, &ce)
	return ce, ok
}

func UTF16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func Normalize(text string, entities []Entity) (string, []Entity, error) {
	if !utf8.ValidString(text) {
		return "", nil, invalid("text", "not valid UTF-8")
	}
	if strings.ContainsRune(text, 0) {
		return "", nil, invalid("text", "contains a NUL character")
	}
	units := utf16.Encode([]rune(text))
	origLen := len(units)
	lead := 0
	for lead < len(units) && isSpaceUnit(units, lead) {
		lead++
	}
	trail := len(units)
	for trail > lead && isSpaceUnit(units, trail-1) {
		trail--
	}
	units = units[lead:trail]
	if len(units) > MaxTextUnits {
		return "", nil, invalid("text", "longer than %d UTF-16 code units", MaxTextUnits)
	}
	text = string(utf16.Decode(units))

	kept := make([]Entity, 0, len(entities))
	for i, e := range entities {
		field := fmt.Sprintf("entities[%d]", i)
		if e.Type == TypeURL {
			continue
		}
		if !manualTypes[e.Type] {
			return "", nil, invalid(field+".type", "unknown entity type %q", e.Type)
		}
		if e.Offset < 0 || e.Length <= 0 {
			return "", nil, invalid(field, "offset must be >= 0 and length > 0")
		}
		if e.end() > origLen {
			return "", nil, invalid(field, "extends past the end of the text")
		}
		start, end := e.Offset-lead, e.end()-lead
		if start < 0 {
			start = 0
		}
		if end > len(units) {
			end = len(units)
		}
		if end <= start {
			continue
		}
		if splitsSurrogate(units, start) || splitsSurrogate(units, end) {
			return "", nil, invalid(field, "a boundary splits a surrogate pair")
		}
		clean := Entity{Type: e.Type, Offset: start, Length: end - start}
		switch e.Type {
		case TypeTextLink:
			if e.URL == nil {
				return "", nil, invalid(field+".url", "text_link needs a url")
			}
			u, err := checkURL(*e.URL)
			if err != nil {
				return "", nil, invalid(field+".url", "%v", err)
			}
			clean.URL = &u
		case TypeMention:
			if e.UserID == nil || *e.UserID <= 0 {
				return "", nil, invalid(field+".user_id", "mention needs a positive user_id")
			}
			id := *e.UserID
			clean.UserID = &id
		case TypePre:
			if e.Language != nil && *e.Language != "" {
				if !languagePattern.MatchString(*e.Language) {
					return "", nil, invalid(field+".language", "must match %s", languagePattern)
				}
				lang := *e.Language
				clean.Language = &lang
			}
		}
		kept = append(kept, clean)
	}
	sortEntities(kept)
	if err := checkNesting(kept); err != nil {
		return "", nil, err
	}
	kept = append(kept, detectURLs(text, units, kept)...)
	sortEntities(kept)
	if len(kept) > MaxEntities {
		return "", nil, invalid("entities", "more than %d entities", MaxEntities)
	}
	if len(kept) == 0 {
		kept = nil
	}
	return text, kept, nil
}

func HasLink(entities []Entity) bool {
	for _, e := range entities {
		if e.Type == TypeURL || e.Type == TypeTextLink {
			return true
		}
	}
	return false
}

func MentionedUserIDs(entities []Entity) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, e := range entities {
		if e.Type == TypeMention && e.UserID != nil && !seen[*e.UserID] {
			seen[*e.UserID] = true
			out = append(out, *e.UserID)
		}
	}
	return out
}

func Slice(text string, entities []Entity, from, to int) (string, []Entity) {
	units := utf16.Encode([]rune(text))
	if from < 0 {
		from = 0
	}
	if to > len(units) {
		to = len(units)
	}
	if from >= to {
		return "", nil
	}
	if splitsSurrogate(units, to) {
		to--
	}
	if splitsSurrogate(units, from) {
		from++
	}
	if from >= to {
		return "", nil
	}
	var out []Entity
	for _, e := range entities {
		if e.Offset >= from && e.end() <= to {
			c := e
			c.Offset -= from
			out = append(out, c)
		}
	}
	return string(utf16.Decode(units[from:to])), out
}

func Preview(text string, entities []Entity) (string, []Entity) {
	if UTF16Len(text) <= PreviewUnits {
		return text, entities
	}
	return Slice(text, entities, 0, PreviewUnits)
}

func QuoteAt(text string, entities []Entity, quote string, offset int) ([]Entity, error) {
	n := UTF16Len(quote)
	if n == 0 || offset < 0 {
		return nil, invalid("reply_quote", "empty quote or negative offset")
	}
	got, ents := Slice(text, entities, offset, offset+n)
	if got != quote {
		return nil, invalid("reply_quote.text", "is not the replied message's text at offset %d", offset)
	}
	return ents, nil
}

var languagePattern = regexp.MustCompile(`^[A-Za-z0-9+#._-]{1,32}$`)

func checkURL(raw string) (string, error) {
	if len(raw) > MaxURLLength {
		return "", fmt.Errorf("longer than %d bytes", MaxURLLength)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("must be an absolute http or https URL")
	}
	return u.String(), nil
}

func isSpaceUnit(units []uint16, i int) bool {
	u := units[i]
	return u < 0xD800 && unicode.IsSpace(rune(u))
}

func splitsSurrogate(units []uint16, i int) bool {
	return i > 0 && i < len(units) && units[i-1] >= 0xD800 && units[i-1] < 0xDC00
}

func sortEntities(es []Entity) {
	sort.SliceStable(es, func(i, j int) bool {
		if es[i].Offset != es[j].Offset {
			return es[i].Offset < es[j].Offset
		}
		return es[i].Length > es[j].Length
	})
}

func checkNesting(es []Entity) error {
	var stack []Entity
	for i, e := range es {
		for len(stack) > 0 && stack[len(stack)-1].end() <= e.Offset {
			stack = stack[:len(stack)-1]
		}
		if len(stack) > 0 {
			top := stack[len(stack)-1]
			if e.end() > top.end() {
				return invalid(fmt.Sprintf("entities[%d]", i), "partly overlaps a %s entity", top.Type)
			}
			if top.Type == TypeCode || top.Type == TypePre {
				return invalid(fmt.Sprintf("entities[%d]", i), "nothing may be nested inside %s", top.Type)
			}
			for _, s := range stack {
				if s.Type == e.Type && s.Offset == e.Offset && s.Length == e.Length {
					return invalid(fmt.Sprintf("entities[%d]", i), "duplicates another %s entity", e.Type)
				}
			}
		}
		stack = append(stack, e)
	}
	return nil
}

var urlPattern = regexp.MustCompile(`(?i)\b(?:https?://|www\.)[^\s<>"'` + "`" + `\x{3000}-\x{303F}\x{FF01}-\x{FF0F}\x{FF1A}-\x{FF20}\x{FF3B}-\x{FF40}\x{FF5B}-\x{FF65}]+`)

const trailingPunct = ".,;:!?)]}'\""

func detectURLs(text string, units []uint16, existing []Entity) []Entity {
	var out []Entity
	for _, loc := range urlPattern.FindAllStringIndex(text, -1) {
		raw := strings.TrimRight(text[loc[0]:loc[1]], trailingPunct)
		if raw == "" {
			continue
		}
		start := UTF16Len(text[:loc[0]])
		e := Entity{Type: TypeURL, Offset: start, Length: UTF16Len(raw)}
		if e.end() > len(units) || !fitsAmong(e, existing) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func fitsAmong(u Entity, existing []Entity) bool {
	for _, e := range existing {
		if u.end() <= e.Offset || e.end() <= u.Offset {
			continue
		}
		inside := u.Offset >= e.Offset && u.end() <= e.end()
		contains := e.Offset >= u.Offset && e.end() <= u.end()
		if !inside && !contains {
			return false
		}
		if inside && (e.Type == TypeCode || e.Type == TypePre || e.Type == TypeTextLink) {
			return false
		}
		if contains {
			return false
		}
	}
	return true
}
